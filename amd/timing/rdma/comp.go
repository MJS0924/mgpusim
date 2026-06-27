// Package rdma provides the implementation of an RDMA engine.
package rdma

import (
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/mem/mempath"
	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/tracing"
)

type transaction struct {
	fromInside  sim.Msg
	fromOutside sim.Msg
	toInside    sim.Msg
	toOutside   sim.Msg
	ack         uint64
}

// An Comp is a component that helps one GPU to access the memory on
// another GPU
type Comp struct {
	*sim.TickingComponent

	deviceID           uint64
	RDMARequestInside  sim.Port
	RDMARequestOutside sim.Port // [R1 LEGACY] retained as nil after R1 split; kept so any external references compile until callers migrate.
	RDMADataInside     sim.Port
	RDMADataOutside    sim.Port // [R1 LEGACY] retained as nil after R1 split; replaced by the 4 typed wire-side ports below.
	RDMAInvInside      sim.Port
	// [ITER7 STRUCTURAL FIX] Dedicated egress for INV RSP toward local
	// REC. Previously INV REQ and INV RSP both used RDMAInvInside,
	// which sequenced RSP delivery behind any backlog of REQs queued
	// at RDMAInvInside.outgoingBuf (observed 1569 at conv2d iter6
	// hang). With response progress blocked by request backlog, the
	// peer-REC.inflightInvToBottom=256 cap never drained → cycle. The
	// new port wires to REC.RDMAInvRspPort (already created at REC
	// builder but never plugged in), giving INV RSP an isolated path
	// that cannot be head-of-line blocked by INV REQ.
	RDMAInvRspInside sim.Port

	// [R1 STRUCTURAL FIX] Four typed wire-side ports replacing the
	// previous bidirectional RDMARequestOutside / RDMADataOutside pair.
	// In iter18 a single RDMADataOutside.outgoingBuf observed 1583/4096
	// mixing three outbound types (sendRspFromL2 AccessRsp + sendInvReq
	// InvReq + sendInvRsp InvRsp). Mixing these three at one port lets a
	// stalled type head-of-line-block the other two and starve the peer's
	// inflight-cap drains. Each direction × type now has its own port:
	//   RDMADataReqOutside: AccessReq egress / peer's AccessReq ingress
	//   RDMADataRspOutside: AccessRsp egress / peer's AccessRsp ingress
	//   RDMAInvReqOutside:  InvReq    egress / peer's InvReq    ingress
	//   RDMAInvRspOutside:  InvRsp    egress / peer's InvRsp    ingress
	RDMADataReqOutside sim.Port
	RDMADataRspOutside sim.Port
	RDMAInvReqOutside  sim.Port
	RDMAInvRspOutside  sim.Port

	// [R1 INSIDE SPLIT] Paired Inside ports for typed traffic from local REC.
	// REC pushes outgoing AccessReq → RDMADataReqInside, outgoing AccessRsp →
	// RDMADataRspInside. Legacy RDMARequestInside / RDMADataInside aliases
	// remain populated to the same underlying ports for backward compat with
	// any non-migrated caller; r9nano builder uses the new names directly.
	RDMADataReqInside sim.Port
	RDMADataRspInside sim.Port

	reqFromL1Buf   []sim.Msg
	procInvReqBuf  []sim.Msg
	incomingReqBuf []sim.Msg
	procInvRspBuf  []sim.Msg

	CtrlPort sim.Port

	isDraining              bool
	pauseIncomingReqsFromL1 bool
	currentDrainReq         *DrainReq

	localModules           mem.AddressToPortMapper
	localInvModules        mem.AddressToPortMapper
	RemoteRDMAAddressTable mem.AddressToPortMapper
	localModuleBottoms     mem.AddressToPortMapper

	transactionsFromOutside []transaction
	transactionsFromInside  []transaction
	invalidationFromInside  []*mem.InvReq
	invalidationFromOutside []*mem.InvReq

	incomingReqPerCycle int
	incomingRspPerCycle int
	outgoingReqPerCycle int
	outgoingRspPerCycle int

	AccessCounter *map[vm.PID]map[uint64]uint8
	dirtyMask     *[]map[vm.PID]map[uint64][]uint8
	readMask      *[]map[vm.PID]map[uint64][]uint8

	log2PageSize      uint64
	log2CacheLineSize uint64

	tickReturn   bool
	printReturn  bool
	recordTime   sim.VTimeInSec
	returnFalse0 string
	returnFalse1 string
	returnFalse2 string
	returnFalse3 string

	// [RDMA INSTRUMENTATION] Silent-drop counters for diagnostic
	// purposes. Incremented in processRspFromL2 /
	// processRspFromRDMARequestOutside when the incoming response's
	// RspTo fails to match any transaction in the local tracking
	// arrays. Non-zero values point at lost responses — likely the
	// origin of the stencil2d/conv2d REC tail-stall (engine halts
	// with 75 WGs still in_progress because their memory acks never
	// returned). Exposed via the monitor API.
	lostRspFromL2Count            uint64 // processRspFromL2: outgoing rsp had no matching transactionsFromOutside
	lostRspFromOutsideCount       uint64 // processRspFromRDMARequestOutside: incoming rsp had no matching transactionsFromInside
	lostRspFromL2SampleID         string // last dropped RspTo ID at processRspFromL2 (newest wins)
	lostRspFromOutsideSampleID    string // last dropped RspTo ID at processRspFromRDMARequestOutside
	// [ITER20 DIAG D] every AccessRsp that arrives at RDMADataRspOutside
	// (peer's ack ingress from the network), counted before matching. If
	// this stays low while the peer egressed many eviction-acks, the acks
	// vanish in the dir->RDMA->network leg, not at the matcher.
	ackRecvAtRspOutsideCount      uint64

	traceProcess bool
	debugProcess bool
	debugAddress uint64
}

// SetLocalModuleFinder sets the table to lookup for local data.
func (c *Comp) SetLocalModuleFinder(lmf mem.AddressToPortMapper) {
	c.localModules = lmf
}

func (c *Comp) SetLocalInvModuleFinder(lmf mem.AddressToPortMapper) {
	c.localInvModules = lmf
}

// SetLocalModuleFinder sets the table to lookup for local data.
func (c *Comp) SetLocalModuleBottomFinder(lmf mem.AddressToPortMapper) {
	c.localModuleBottoms = lmf
}

// Tick checks if make progress
// func (c *Comp) Tick() bool {
// 	madeProgress := false

// 	madeProgress = c.processFromCtrlPort() || madeProgress
// 	if c.isDraining {
// 		madeProgress = c.drainRDMA() || madeProgress
// 	}

// 	for i := 0; i < c.outgoingReqPerCycle; i++ {
// 		madeProgress = c.processFromL1() || madeProgress // 1. Req. from RDMARequestInside -> RDMARequestOutside
// 	}

// 	for i := 0; i < c.outgoingRspPerCycle; i++ {
// 		madeProgress = c.processFromL2() || madeProgress // 3. Rsp. from RDMADataInside -> Rsp. to corresponding req.Src
// 	}

// 	for i := 0; i < c.incomingReqPerCycle; i++ {
// 		madeProgress = c.processIncomingReq() || madeProgress // 2. Req. from RDMADataOutside -> Req. to RDMADataInside
// 	}

// 	for i := 0; i < c.incomingRspPerCycle; i++ {
// 		madeProgress = c.processIncomingRsp() || madeProgress // 4. Rsp. from RDMARequestOutside -> Rsp. to corresponding req.Src
// 	}

// 	c.tickReturn = madeProgress
// 	return madeProgress
// }

// Tick checks if make progress
func (c *Comp) Tick() bool {
	// now := c.Engine.CurrentTime()
	// c.printReturn = false
	// if now >= c.recordTime+0.000001 && c.deviceID == 5 {
	// 	c.recordTime = now
	// 	c.printReturn = false
	// }
	c.traceProcess = false
	c.debugProcess = false
	c.debugAddress = 12884956160

	madeProgress := false
	temp := false

	temp = c.processFromCtrlPort()
	madeProgress = temp || madeProgress
	if c.printReturn {
		fmt.Printf("[DEBUG RDMA 5]\treturn 1: %v\n", temp)
	}

	if c.isDraining {
		temp = c.drainRDMA()
		madeProgress = temp || madeProgress
		if c.printReturn {
			fmt.Printf("[DEBUG RDMA 5]\treturn 2: %v\n", temp)
		}
	}

	// [R1] RSP handlers run before REQ handlers. RSPs unblock peer inflight
	// caps; REQs add new pressure. Within RSP vs REQ the two typed paths
	// (DATA and INV) are fully independent — they peek their own ports and
	// share no gating buffer.

	// --- Outbound RSPs (local L2 -> peer) ---
	for i := 0; i < c.outgoingRspPerCycle; i++ {
		temp = c.processFromL2() // local AccessRsp -> RDMADataRspOutside
		madeProgress = temp || madeProgress
	}

	// [ITER19 DEADLOCK FIX] Drain the peer-serve AccessRsp that the local
	// directory returns to RDMADataReqInside. processReqFromRDMADataOutside
	// forwards a peer's AccessReq to the local dir with Src=RDMADataReqInside;
	// every directory (REC/SD/HMG) copies rsp.Dst = req.Src verbatim, so the
	// dir's AccessRsp is delivered into RDMADataReqInside.IncomingBuf. Nothing
	// read it before -> responses piled up forever -> engine ran dry -> dead-
	// lock (conv2d window-1 L=2304 == remote_read_miss). The request egress
	// uses RDMADataReqInside.OutgoingBuf and this reader drains its IncomingBuf:
	// physically distinct buffers, so a queued REQUEST can never head-of-line
	// block a RESPONSE. RDMADataReqInside.IncomingBuf is type-pure (only the
	// dir's AccessRsp is ever addressed there; ToRDMADataReq is a dead field).
	for i := 0; i < c.outgoingRspPerCycle; i++ {
		temp = c.processReqInsideRsp()
		madeProgress = temp || madeProgress
	}

	// --- Inbound RSPs (peer -> local consumer) ---
	for i := 0; i < c.incomingRspPerCycle; i++ {
		temp = c.processIncomingDataRsp() // peer AccessRsp at RDMADataRspOutside
		madeProgress = temp || madeProgress
	}
	for i := 0; i < c.incomingRspPerCycle; i++ {
		temp = c.processIncomingInvRsp() // peer InvRsp at RDMAInvRspOutside
		madeProgress = temp || madeProgress
	}

	// [ITER7] Dedicated INV RSP path from local REC toward peer.
	for i := 0; i < c.outgoingRspPerCycle; i++ {
		temp = c.processFromInvRspInside()
		madeProgress = temp || madeProgress
	}

	// --- Outbound REQs (local L1 -> peer) ---
	for i := 0; i < c.outgoingReqPerCycle; i++ {
		temp = c.processFromL1() // local AccessReq -> RDMADataReqOutside
		madeProgress = temp || madeProgress
	}

	// --- Inbound REQs (peer -> local consumer) ---
	for i := 0; i < c.incomingReqPerCycle; i++ {
		temp = c.processIncomingDataReq() // peer AccessReq at RDMADataReqOutside
		madeProgress = temp || madeProgress
	}
	for i := 0; i < c.incomingReqPerCycle; i++ {
		temp = c.processIncomingInvReq() // peer InvReq at RDMAInvReqOutside
		madeProgress = temp || madeProgress
	}

	// [ITER7] InvInside intake from local directory (outbound InvReq).
	for i := 0; i < c.outgoingRspPerCycle; i++ {
		temp = c.processFromInvInside()
		madeProgress = temp || madeProgress
	}

	c.tickReturn = madeProgress

	return madeProgress
}

func (c *Comp) processFromCtrlPort() bool {
	req := c.CtrlPort.PeekIncoming()
	if req == nil {
		// if c.deviceID == 5 {
		// 	fmt.Printf("[RDMA 5]\tReturn false: No valid request from CtrlPort\n")
		// }
		return false
	}

	switch req := req.(type) {
	case *DrainReq:
		fmt.Printf("[RDMA %d]\tStart RDMA Drain\n", c.deviceID)
		c.currentDrainReq = req
		c.isDraining = true
		c.pauseIncomingReqsFromL1 = true

		c.CtrlPort.RetrieveIncoming()

		// RDMA drain -> 현재 진행 중인 요청을 모두 없애버리기
		// L2 cache에서 요청을 받지 않음 -> L2 cache queue가 full 됨 -> RDMA 멈춤 -> drain 요청 처리 불가
		// 따라서 drain 요청 시, 진행되지 않은 요청은 없애야 할 듯?
		// c.transactionsFromInside = nil
		// c.transactionsFromOutside = nil

		return true
	case *RestartReq:
		return c.processRDMARestartReq(req)
	default:
		log.Panicf("cannot process request of type %s", reflect.TypeOf(req))
		return false
	}
}

func (c *Comp) processRDMARestartReq(req *RestartReq) bool {
	restartCompleteRsp := RestartRspBuilder{}.
		WithSrc(c.CtrlPort.AsRemote()).
		// WithDst(c.currentDrainReq.Src).
		WithDst(req.Meta().Src).
		Build()
	err := c.CtrlPort.Send(restartCompleteRsp)
	fmt.Printf("[RDMA %d]\tTry to Send Restart Rsp to driver\n", c.deviceID)

	if err != nil {
		// if c.deviceID == 5 {
		// 	fmt.Printf("[RDMA 5]\tReturn false: Fail to send restart rsp to driver\n")
		// }
		return false
	}

	c.currentDrainReq = nil
	c.pauseIncomingReqsFromL1 = false
	c.CtrlPort.RetrieveIncoming()

	fmt.Printf("\t\tSuccess to Send Restart Rsp to driver\n")
	// *(c.AccessCounter) = make(map[vm.PID]map[uint64]uint8)

	return true
}

func (c *Comp) drainRDMA() bool {
	drainCompleteRsp := DrainRspBuilder{}.
		WithSrc(c.CtrlPort.AsRemote()).
		WithDst(c.currentDrainReq.Src).
		Build()

	err := c.CtrlPort.Send(drainCompleteRsp)
	if err != nil {
		// if c.deviceID == 5 {
		// 	fmt.Printf("[RDMA 5]\tReturn false: Fail to send drain rsp to driver\n")
		// }
		return false
	}

	c.transactionsFromInside = nil
	c.transactionsFromOutside = nil
	c.invalidationFromInside = nil
	c.invalidationFromOutside = nil

	for c.RDMADataInside.RetrieveIncoming() != nil {
	}
	for c.RDMARequestInside.RetrieveIncoming() != nil {
	}
	for c.RDMAInvInside.RetrieveIncoming() != nil {
	}
	for c.RDMAInvRspInside.RetrieveIncoming() != nil {
	}
	// [ITER19 DEADLOCK FIX] Also drain the typed data inside ports so a
	// mid-flight DrainReq cannot strand peer-serve traffic and stall
	// fullyDrained(): RDMADataReqInside holds the peer-serve REQ egress backlog
	// and the peer-serve RSP ingress (now consumed by processReqInsideRsp);
	// RDMADataRspInside is reserved/inert but drained for symmetry.
	for c.RDMADataReqInside.RetrieveIncoming() != nil {
	}
	for c.RDMADataRspInside.RetrieveIncoming() != nil {
	}
	// [R1] drain the 4 typed wire-side ports.
	for c.RDMADataReqOutside.RetrieveIncoming() != nil {
	}
	for c.RDMADataRspOutside.RetrieveIncoming() != nil {
	}
	for c.RDMAInvReqOutside.RetrieveIncoming() != nil {
	}
	for c.RDMAInvRspOutside.RetrieveIncoming() != nil {
	}

	c.reqFromL1Buf = nil
	c.procInvReqBuf = nil
	c.incomingReqBuf = nil
	c.procInvRspBuf = nil

	c.isDraining = false

	return true
}

func (c *Comp) fullyDrained() bool {
	return len(c.transactionsFromOutside) == 0 &&
		len(c.transactionsFromInside) == 0
}

func (c *Comp) processFromL1() bool {
	if c.pauseIncomingReqsFromL1 {
		c.returnFalse0 = "pauseIncomingReqsFromL1"
		return false
	}

	madeProgress := false
	for {
		if len(c.reqFromL1Buf) > 0 {
			item := c.reqFromL1Buf[0]
			// [R1] Egress on the typed DATA-REQ port. Src was already set
			// to RDMADataReqOutside.AsRemote() at processReqFromL1; using
			// any other port would panic "sending port is not msg src".
			err := c.RDMADataReqOutside.Send(item)

			if err == nil {
				c.reqFromL1Buf = c.reqFromL1Buf[1:]
				madeProgress = true

				continue
			}
		}

		req := c.RDMARequestInside.PeekIncoming()
		if req == nil {
			if !madeProgress {
				c.returnFalse0 = "There is no req from RDMAReqInside"
			}

			return madeProgress
		}

		switch req := req.(type) {
		case mem.AccessReq:
			ret := c.processReqFromL1(req)
			if !ret {
				if !madeProgress {
					c.returnFalse0 = "[processReqFromL1]"
				}

				return madeProgress
			} else if c.debugProcess && req.GetAddress() == c.debugAddress {
				fmt.Printf("[%s] [bottomSender]\tSend remote read req - 0: addr %x\n", c.Name(), req.GetAddress())
			}

			c.recordMsgSend(req)
			madeProgress = true
		default:
			log.Panicf("cannot process request of type %s", reflect.TypeOf(req))
			return false
		}
	}
}

func (c *Comp) processReqFromL1(
	req mem.AccessReq,
) bool {
	dst := c.RemoteRDMAAddressTable.Find(req.GetAddress())
	cloned := c.cloneReq(req)
	// [R1] Egress on the typed DATA-REQ port. RemoteRDMAAddressTable was
	// updated to map to peer's RDMADataReqOutside in the platform builder.
	cloned.Meta().Src = c.RDMADataReqOutside.AsRemote()
	cloned.Meta().Dst = dst
	cloned.SetSrcRDMA(cloned.Meta().Src)

	err := (*sim.SendError)(nil)
	if !c.RDMADataReqOutside.CanSend() {
		c.reqFromL1Buf = append(c.reqFromL1Buf, cloned)
	} else {
		err = c.RDMADataReqOutside.Send(cloned)
	}

	if err != nil {
		return false
	}

	c.RDMARequestInside.RetrieveIncoming()

	trans := transaction{
		fromInside: req,
		toOutside:  cloned,
	}
	c.transactionsFromInside = append(c.transactionsFromInside, trans)

	return true
}

func (c *Comp) processFromL2() bool {
	for {
		req := c.RDMADataInside.PeekIncoming()
		if req == nil {
			c.returnFalse1 = "There is no req from RDMADataInside"
			return false
		}

		switch req := req.(type) {
		case mem.AccessRsp:
			c.returnFalse1 = ""
			ret := c.processRspFromL2(req)
			if ret {
				c.recordMsgSend(req)
			} else if c.debugProcess && req.GetOrigin().GetAddress() == c.debugAddress {
				fmt.Printf("[%s] [bottomSender]\tSend remote read rsp - 2: addr %x\n", c.Name(), req.GetOrigin().GetAddress())
			}

			return ret
		default:
			panic(fmt.Sprintf("unknown req type %T, Src %s", req, req.Meta().Src))
		}
	}
}

// [ITER19 DEADLOCK FIX] processReqInsideRsp drains the peer-serve AccessRsp
// that the local directory returns to RDMADataReqInside.IncomingBuf. The
// forwarded peer REQ (processReqFromRDMADataOutside) keeps Src=RDMADataReqInside,
// and every directory (REC/SD/HMG) sets rsp.Dst = req.Src verbatim, so the
// dir's AccessRsp arrives here. Type-pure: only AccessRsp is ever addressed to
// RDMADataReqInside (ToRDMADataReq is a dead field; nothing sends an AccessReq
// here). The request egress lives on RDMADataReqInside.OutgoingBuf, this reader
// drains IncomingBuf -> distinct buffers, no req-blocks-rsp HoL.
func (c *Comp) processReqInsideRsp() bool {
	req := c.RDMADataReqInside.PeekIncoming()
	if req == nil {
		return false
	}

	switch req := req.(type) {
	case mem.AccessRsp:
		ret := c.processRspFromReqInside(req)
		if ret {
			c.recordMsgSend(req)
		}
		return ret
	default:
		panic(fmt.Sprintf("unknown rsp type %T at RDMADataReqInside, Src %s",
			req, req.Meta().Src))
	}
}

// processRspFromReqInside is processRspFromL2 with the ingress RetrieveIncoming
// pointed at RDMADataReqInside instead of RDMADataInside. All other semantics
// (transaction match against transactionsFromOutside, clone, ReqOutside ->
// RspOutside Dst rewrite, ack/cleanup) are identical, so the peer-serve
// completion path is unchanged.
func (c *Comp) processRspFromReqInside(rsp mem.AccessRsp) bool {
	if !c.RDMADataRspOutside.CanSend() {
		return false
	}

	transactionIndex := c.findTransactionByRspToID(
		rsp.GetRspTo(), c.transactionsFromOutside)
	if transactionIndex == -1 {
		c.RDMADataReqInside.RetrieveIncoming()
		c.lostRspFromL2Count++
		c.lostRspFromL2SampleID = rsp.GetRspTo()
		return true
	}
	trans := &(c.transactionsFromOutside[transactionIndex])

	rspToOutside := c.cloneRsp(rsp, trans.fromOutside.Meta().ID,
		trans.fromOutside.(mem.AccessReq).GetAddress())
	rspToOutside.Meta().Src = c.RDMADataRspOutside.AsRemote()
	rspDst := string(trans.fromOutside.Meta().Src)
	rspToOutside.Meta().Dst = sim.RemotePort(
		strings.Replace(rspDst, ".RDMADataReqOutside", ".RDMADataRspOutside", 1))

	err := c.RDMADataRspOutside.Send(rspToOutside)
	if err != nil {
		return false
	}
	c.RDMADataReqInside.RetrieveIncoming()

	trans.ack++
	if trans.ack >= rsp.GetWaitFor() {
		c.traceOutsideInEnd(*trans)
		c.transactionsFromOutside =
			append(c.transactionsFromOutside[:transactionIndex],
				c.transactionsFromOutside[transactionIndex+1:]...)
	}

	return true
}

func (c *Comp) processRspFromL2(
	rsp mem.AccessRsp,
) bool {
	c.returnFalse1 = "[processRspFromL2]"
	if !c.RDMADataRspOutside.CanSend() {
		c.returnFalse1 = "[processRspFromL2] Cannot send to RDMADataRspOutside"
		return false
	}

	transactionIndex := c.findTransactionByRspToID(
		rsp.GetRspTo(), c.transactionsFromOutside)
	if transactionIndex == -1 {
		c.RDMADataInside.RetrieveIncoming()
		c.returnFalse1 = "[processRspFromL2] Cannot find transaction"
		// [RDMA INSTRUMENTATION] Silent drop: response from local L2
		// could not be matched against any pending external request.
		c.lostRspFromL2Count++
		c.lostRspFromL2SampleID = rsp.GetRspTo()
		return true
	}
	trans := &(c.transactionsFromOutside[transactionIndex])

	// rspToOutside := c.cloneRsp(rsp, trans.fromOutside.Meta().ID)
	rspToOutside := c.cloneRsp(rsp, trans.fromOutside.Meta().ID, trans.fromOutside.(mem.AccessReq).GetAddress())
	rspToOutside.Meta().Src = c.RDMADataRspOutside.AsRemote()
	// [R1] Original incoming REQ arrived from peer's RDMADataReqOutside.
	// Route the response into peer's typed RSP-ingress port so it cannot
	// be head-of-line blocked behind queued REQs at peer's REQ port.
	rspDst := string(trans.fromOutside.Meta().Src)
	rspToOutside.Meta().Dst = sim.RemotePort(
		strings.Replace(rspDst, ".RDMADataReqOutside", ".RDMADataRspOutside", 1))

	err := c.RDMADataRspOutside.Send(rspToOutside)
	if err != nil {
		c.returnFalse1 = "[processRspFromL2] Failed to send to RDMADataRspOutside"
		return false
	}
	c.RDMADataInside.RetrieveIncoming()

	trans.ack++
	if trans.ack >= rsp.GetWaitFor() {
		// if rsp.GetWaitFor() != 1 {
		// 	fmt.Printf("[RDMA %d]\tSend last rsp for %x from %s to %s\n\t\t\tack: %d %d\n", c.deviceID,
		// 		trans.fromOutside.(mem.AccessReq).GetAddress(), rsp.Meta().Src, rspToOutside.Meta().Dst, trans.ack, rsp.GetWaitFor())
		// }

		c.traceOutsideInEnd(*trans)

		c.transactionsFromOutside =
			append(c.transactionsFromOutside[:transactionIndex],
				c.transactionsFromOutside[transactionIndex+1:]...)
	}

	// if strings.Contains(c.Name(), "GPU[3]") {
	// 	fmt.Printf("[%s]\t4. Response(%s) %x from %s to %s\n", c.Name(), rsp.GetRspTo(), trans.fromOutside.(mem.AccessReq).GetAddress(), rspToOutside.Meta().Src, rspToOutside.Meta().Dst)
	// }
	return true

}

// [R1] processIncomingDataRsp peeks the dedicated RDMADataRspOutside port.
// Replaces half of the old processIncomingRsp which multiplexed AccessRsp
// and InvReq on a single RDMARequestOutside port. With the split,
// downstream INV REQ backlog can no longer head-of-line block AccessRsp
// delivery to the originating L1.
func (c *Comp) processIncomingDataRsp() bool {
	req := c.RDMADataRspOutside.PeekIncoming()
	if req == nil {
		c.returnFalse3 = "There is no rsp from RDMADataRspOutside"
		return false
	}

	switch req := req.(type) {
	case mem.AccessRsp:
		c.ackRecvAtRspOutsideCount++ // [ITER20 DIAG D]
		ret := c.processRspFromRDMARequestOutside(req)
		if ret {
			c.recordMsgSend(req)
			if c.debugProcess && req.GetOrigin().GetAddress() == c.debugAddress {
				fmt.Printf("[%s] [bottomSender]\tReceive remote read rsp - 3: addr %x\n", c.Name(), req.GetOrigin().GetAddress())
			}
			return true
		}
		c.returnFalse3 = "[processRspFromRDMARequestOutside]"
		return false
	default:
		log.Panicf("unexpected type at RDMADataRspOutside: %s", reflect.TypeOf(req))
		return false
	}
}

// [R1] processIncomingInvReq peeks the dedicated RDMAInvReqOutside port.
// The InvReq buffering buffer (procInvReqBuf) is preserved because the
// downstream RDMAInvInside.CanSend() can still false momentarily (local
// directory backpressure) and we must not drop the peer's InvReq.
func (c *Comp) processIncomingInvReq() bool {
	madeProgress := false
	popInvReqBuf := false

	if len(c.procInvReqBuf) > 0 {
		item := c.procInvReqBuf[0]
		// [ITER5 BUG FIX] procInvReqBuf holds InvReq messages whose Src
		// was set to RDMAInvInside.AsRemote() at processInvReq. Use the
		// matching port to avoid "sending port is not msg src" panic.
		err := c.RDMAInvInside.Send(item)
		if err == nil {
			c.procInvReqBuf = c.procInvReqBuf[1:]
			madeProgress = true
			popInvReqBuf = true
		}
	}

	req := c.RDMAInvReqOutside.PeekIncoming()
	if req == nil {
		if !madeProgress {
			c.returnFalse3 = "There is no req from RDMAInvReqOutside"
		}
		return madeProgress
	}

	switch req := req.(type) {
	case *mem.InvReq:
		if !popInvReqBuf {
			ret := c.processInvReq(req)
			if ret {
				c.recordMsgSend(req)
				madeProgress = true
			}

			if !madeProgress {
				c.returnFalse3 = "[processInvReq]"
			}
		}
	default:
		log.Panicf("unexpected type at RDMAInvReqOutside: %s", reflect.TypeOf(req))
		return false
	}

	return madeProgress
}

func (c *Comp) processRspFromRDMARequestOutside(
	rsp mem.AccessRsp,
) bool {
	if !c.RDMARequestInside.CanSend() {
		// if c.deviceID == 5 {
		// 	fmt.Printf("[RDMA 5]\tReturn false: processRspFromRDMARequestOutside: Cannot send to RDMARequestInside 1\n")
		// }
		return false
	}

	transactionIndex := c.findTransactionByRspToID(
		rsp.GetRspTo(), c.transactionsFromInside)
	var trans transaction
	var rspToInside mem.AccessRsp

	if transactionIndex == -1 {
		// [RDMA INSTRUMENTATION] Incoming response from peer GPU
		// failed to match any pending request this RDMA had forwarded
		// outward — address-only routing then likely sends it to the
		// wrong destination (or a port that isn't expecting it),
		// stranding the originating L1/L2.
		c.lostRspFromOutsideCount++
		c.lostRspFromOutsideSampleID = rsp.GetRspTo()
		rspToInside = c.cloneRsp(rsp, "", rsp.GetOrigin().GetAddress())
		rspToInside.Meta().Src = c.RDMARequestInside.AsRemote()
		rspToInside.Meta().Dst = c.localModuleBottoms.Find(rsp.GetOrigin().GetAddress())
	} else {
		trans = c.transactionsFromInside[transactionIndex]
		rspToInside = c.cloneRsp(rsp, trans.fromInside.Meta().ID, trans.fromInside.(mem.AccessReq).GetAddress())
		rspToInside.Meta().Src = c.RDMARequestInside.AsRemote()
		rspToInside.Meta().Dst = trans.fromInside.Meta().Src

		// fmt.Printf("[RDMA %d]\tSend data %x to %s\n",
		// 	c.deviceID, rsp.GetOrigin().GetAddress(), rspToInside.Meta().Dst)
	}

	err := c.RDMARequestInside.Send(rspToInside)
	if err != nil {
		// if c.deviceID == 5 {
		// 	fmt.Printf("[RDMA 5]\tReturn false: processRspFromRDMARequestOutside: Cannot send to RDMARequestInside 2\n")
		// }
		return false
	}

	// [R1 BUGFIX] Drain from the typed peer-side RSP port — legacy
	// RDMARequestOutside is nil after R1 split (peer data RSP arrives at
	// RDMADataRspOutside).
	c.RDMADataRspOutside.RetrieveIncoming()

	if transactionIndex != -1 {
		c.transactionsFromInside =
			append(c.transactionsFromInside[:transactionIndex],
				c.transactionsFromInside[transactionIndex+1:]...)

		// c.recordAccessCount(trans)

		c.traceInsideOutEnd(trans)
	}

	// fmt.Printf("[%s]\t4. Response %x from %s to %s\n", c.Name(), trans.fromInside.(mem.AccessReq).GetAddress(), rsp.Meta().Src, rspToInside.Meta().Dst)
	return true
}

func (c *Comp) processInvReq(
	req *mem.InvReq,
) bool {
	reqToBottom := mem.InvReqBuilder{}.
		WithSrc(c.RDMAInvInside.AsRemote()).
		WithDst(c.localInvModules.Find(req.Address)).
		WithPID(req.PID).
		WithAddress(req.Address).
		WithReqFrom(req.Meta().ID).
		WithDstRDMA(req.DstRDMA).
		WithRegionID(req.RegionID).
		WithIsWriteInv(req.IsWriteInv).
		Build()

	err := (*sim.SendError)(nil)
	if !c.RDMAInvInside.CanSend() {
		c.procInvReqBuf = append(c.procInvReqBuf, reqToBottom)
	} else {
		err = c.RDMAInvInside.Send(reqToBottom)
	}
	if err == nil {
		// [R1 BUGFIX] InvReq arrives via the typed RDMAInvReqOutside —
		// legacy RDMARequestOutside is nil after R1 split.
		c.RDMAInvReqOutside.RetrieveIncoming()
		c.invalidationFromOutside = append(c.invalidationFromOutside, req)

		// fmt.Printf("[%s]\tC. (%s -> %s) Send InvReq for Addr %x from %s to %s\n",
		// 	c.Name(), req.Meta().ID, reqToBottom.Meta().ID, req.Address, req.Meta().Src, reqToBottom.Meta().Dst)
		return true
	}

	// if c.deviceID == 5 {
	// 	fmt.Printf("[RDMA 5]\tReturn false: processInvReq: Cannot send to RDMARequestOutside\n")
	// }
	return false
}

func (c *Comp) recordAccessCount(
	trans transaction,
) bool {

	req := trans.fromInside.(mem.AccessReq)
	vAddr := req.GetVAddr()
	byteSize := req.GetByteSize()
	pid := req.GetPID()

	startPage := vAddr >> c.log2PageSize
	endPage := (vAddr + byteSize - 1) >> c.log2PageSize

	ac := *(c.AccessCounter)
	innerMap, found := ac[pid]

	if !found {
		innerMap = make(map[uint64]uint8)
		ac[pid] = innerMap
	}

	for addr := startPage; addr <= endPage; addr++ {
		if innerMap[addr] < 255 {
			innerMap[addr]++
		}
	}

	return true
}

// [R1] processIncomingDataReq peeks RDMADataReqOutside (peer's AccessReq
// ingress). Replaces half of the old processIncomingReq which
// multiplexed AccessReq and InvRsp on a single RDMADataOutside port.
func (c *Comp) processIncomingDataReq() bool {
	madeProgress := false
	popIncomingReqBuf := false

	if len(c.incomingReqBuf) > 0 {
		item := c.incomingReqBuf[0]
		// item.Src == RDMADataReqInside (set in processReqFromRDMADataOutside)
		err := c.RDMADataReqInside.Send(item)
		if err == nil {
			c.incomingReqBuf = c.incomingReqBuf[1:]
			madeProgress = true
			popIncomingReqBuf = true
		}
	}

	req := c.RDMADataReqOutside.PeekIncoming()
	if req == nil {
		if !madeProgress {
			c.returnFalse2 = "There is no req from RDMADataReqOutside"
		}
		return madeProgress
	}

	switch req := req.(type) {
	case mem.AccessReq:
		if !popIncomingReqBuf {
			ret := c.processReqFromRDMADataOutside(req)
			if ret {
				c.recordMsgSend(req)
				madeProgress = true
			} else if c.debugProcess && req.GetAddress() == c.debugAddress {
				fmt.Printf("[%s] [bottomSender]\tReceive remote read req - 1: addr %x\n", c.Name(), req.GetAddress())
			}

			if !madeProgress {
				c.returnFalse2 = "[processReqFromRDMADataOutside]"
			}
		}
	default:
		log.Panicf("unexpected type at RDMADataReqOutside: %s", reflect.TypeOf(req))
		return false
	}

	return madeProgress
}

// [R1] processIncomingInvRsp peeks RDMAInvRspOutside (peer's InvRsp
// ingress). Replaces the InvRsp branch of the old processIncomingReq.
func (c *Comp) processIncomingInvRsp() bool {
	madeProgress := false
	popProcInvRspBuf := false

	if len(c.procInvRspBuf) > 0 {
		item := c.procInvRspBuf[0]
		// [ITER7] Use RDMAInvRspInside (not RDMADataInside) — procInvRspBuf
		// items were queued by processInvRsp with Src=RDMAInvRspInside.
		err := c.RDMAInvRspInside.Send(item)
		if err == nil {
			c.procInvRspBuf = c.procInvRspBuf[1:]
			madeProgress = true
			popProcInvRspBuf = true
		}
	}

	req := c.RDMAInvRspOutside.PeekIncoming()
	if req == nil {
		if !madeProgress {
			c.returnFalse2 = "There is no rsp from RDMAInvRspOutside"
		}
		return madeProgress
	}

	switch req := req.(type) {
	case *mem.InvRsp:
		if !popProcInvRspBuf {
			ret := c.processInvRsp(req)
			if ret {
				c.recordMsgSend(req)
				madeProgress = true
			}

			if !madeProgress {
				c.returnFalse2 = "[processInvRsp]"
			}
		}
	default:
		log.Panicf("unexpected type at RDMAInvRspOutside: %s", reflect.TypeOf(req))
		return false
	}

	return madeProgress
}

func (c *Comp) processReqFromRDMADataOutside(
	req mem.AccessReq,
) bool {
	dst := c.localModules.Find(req.GetAddress())

	cloned := c.cloneReq(req)
	// [R1 BUGFIX] Use the typed Inside REQ port — paired with R2's REC
	// RDMADataReqPort via the shared RDMAToCohDir directconnection.
	cloned.Meta().Src = c.RDMADataReqInside.AsRemote()
	cloned.Meta().Dst = dst

	err := (*sim.SendError)(nil)
	if !c.RDMADataReqInside.CanSend() {
		c.incomingReqBuf = append(c.incomingReqBuf, cloned)
	} else {
		err = c.RDMADataReqInside.Send(cloned)
	}

	if err == nil {
		// [R1 BUGFIX] Drain from the typed peer-side port, not the legacy
		// nil RDMADataOutside.
		c.RDMADataReqOutside.RetrieveIncoming()

		trans := transaction{
			fromOutside: req,
			toInside:    cloned,
		}
		c.transactionsFromOutside =
			append(c.transactionsFromOutside, trans)

		return true
	}

	// if c.deviceID == 5 {
	// 	fmt.Printf("[RDMA 5]\tReturn false: processReqFromRDMADataOutside: Cannot send to RDMADataInside\n")
	// }
	return false
}

func (c *Comp) processInvRsp(
	rsp *mem.InvRsp,
) bool {
	i := c.findInvReqByRspToID(rsp.RespondTo, c.invalidationFromInside)
	if i == -1 {
		fmt.Printf("[RDMA %d]\t3. Cannot find invalidation request for InvRsp with RespondTo %s\n", c.deviceID, rsp.RespondTo)
		// [R1] Inbound peer InvRsp now arrives on RDMAInvRspOutside.
		c.RDMAInvRspOutside.RetrieveIncoming()
		// return false
		return true
	}
	req := c.invalidationFromInside[i]

	// [ITER7] Use dedicated RDMAInvRspInside port for INV RSP so RSP
	// delivery cannot be queued behind INV REQ traffic on
	// RDMAInvInside.outgoingBuf. The original InvReq's Src was set by
	// REC's sendToTop to "GPU[X].<Dir>.RDMAInvPort"; the corresponding
	// RspPort is "GPU[X].<Dir>.RDMAInvRspPort". Rewrite the Dst here
	// so the directconnection RDMAToCohDirForInvRsp finds the port.
	dstStr := string(req.Meta().Src)
	rspDst := sim.RemotePort(strings.Replace(dstStr, ".RDMAInvPort", ".RDMAInvRspPort", 1))

	rspToBottom := mem.InvRspBuilder{}.
		WithSrc(c.RDMAInvRspInside.AsRemote()).
		WithDst(rspDst).
		WithRspTo(req.ReqFrom).
		WithSrcRDMA(rsp.SrcRDMA).
		Build()

	err := (*sim.SendError)(nil)
	if !c.RDMAInvRspInside.CanSend() {
		c.procInvRspBuf = append(c.procInvRspBuf, rspToBottom)
	} else {
		err = c.RDMAInvRspInside.Send(rspToBottom)
	}

	if err == nil {
		// [R1] Inbound peer InvRsp arrived on RDMAInvRspOutside.
		c.RDMAInvRspOutside.RetrieveIncoming()
		c.invalidationFromInside = append(c.invalidationFromInside[:i], c.invalidationFromInside[i+1:]...)
		// fmt.Printf("[RDMA %d]\tFinalize Inv Req - 2: %s -> %s, %s\n", c.deviceID, rsp.RespondTo, req.ReqFrom, rspToBottom.Dst)

		return true
	}

	return false
}

func (c *Comp) processFromInvInside() bool {
	req := c.RDMAInvInside.PeekIncoming()
	if req == nil {
		return false
	}

	switch req := req.(type) {
	case *mem.InvReq:
		if c.sendInvReq(req) {
			c.recordMsgSend(req)
			return true
		}
		return false

	case *mem.InvRsp:
		// [ITER7 contract] InvRsp must arrive on RDMAInvRspInside, not
		// RDMAInvInside. The legacy transitional path is gone — any
		// InvRsp arriving here is a wiring contract violation.
		panic("InvRsp must arrive on RDMAInvRspInside")

	default:
		panic(fmt.Sprintf("unknown req type in RDMAInvInside: %s", reflect.TypeOf(req)))
	}
}

// [ITER7] Dedicated INV RSP intake from REC. Isolated from
// processFromInvInside so a backlog of outgoing INV REQs cannot
// head-of-line block INV RSP egress on the cross-GPU path.
func (c *Comp) processFromInvRspInside() bool {
	rsp := c.RDMAInvRspInside.PeekIncoming()
	if rsp == nil {
		return false
	}

	switch rsp := rsp.(type) {
	case *mem.InvRsp:
		if c.sendInvRsp(rsp) {
			c.recordMsgSend(rsp)
			return true
		}
		return false
	default:
		panic(fmt.Sprintf("unknown msg type in RDMAInvRspInside: %s", reflect.TypeOf(rsp)))
	}
}

func (c *Comp) sendInvReq(
	req *mem.InvReq,
) bool {
	// [R1] InvReq egress on the dedicated typed port. Previously this
	// shared RDMADataOutside with sendRspFromL2 + sendInvRsp; an INV
	// REQ backlog (iter18: 1583/4096) head-of-line blocked AccessRsp
	// and InvRsp egress, starving peer caps.
	if !c.RDMAInvReqOutside.CanSend() {
		return false
	}

	// [ITER19 INV-ROUTE FIX] The directory records a sharer's identity from
	// the original data request's SrcRDMA, which is the peer's
	// RDMADataReqOutside (data-REQ port). Sending an InvReq there makes the
	// peer's processIncomingDataReq panic ("unexpected type ... *mem.InvReq").
	// Route the InvReq to the peer's typed INV-REQ ingress instead, mirroring
	// the ReqOutside->RspOutside rewrite in processRspFromL2/sendInvRsp. For
	// REC (DstRDMA already an inv port) the replace is a no-op.
	invDst := sim.RemotePort(strings.Replace(
		string(req.DstRDMA), ".RDMADataReqOutside", ".RDMAInvReqOutside", 1))

	reqToOutside := mem.InvReqBuilder{}.
		WithSrc(c.RDMAInvReqOutside.AsRemote()).
		WithDst(invDst).
		WithPID(req.PID).
		WithAddress(req.Address).
		WithReqFrom(req.Meta().ID).
		WithDstRDMA(req.DstRDMA).
		WithRegionID(req.RegionID).
		WithIsWriteInv(req.IsWriteInv).
		Build()

	err := c.RDMAInvReqOutside.Send(reqToOutside)
	if err == nil {
		c.RDMAInvInside.RetrieveIncoming()
		c.invalidationFromInside = append(c.invalidationFromInside, req)
		return true
	}

	return false
}

func (c *Comp) sendInvRsp(
	rsp *mem.InvRsp,
) bool {
	// [R1] InvRsp egress on the dedicated typed port. Previously this
	// shared RDMADataOutside with AccessRsp + InvReq; under heavy
	// invalidation traffic this was the head-of-line block.
	if !c.RDMAInvRspOutside.CanSend() {
		return false
	}

	i := c.findInvReqByRspToID(rsp.RespondTo, c.invalidationFromOutside)
	if i == -1 {
		fmt.Printf("[RDMA %d]\t2. Cannot find invalidation request for InvRsp with RespondTo %s\n", c.deviceID, rsp.RespondTo)
		// [ITER19 INV-RSP DRAIN FIX] processFromInvRspInside peeked this InvRsp
		// from RDMAInvRspInside; drain THAT port, not RDMAInvInside (distinct
		// buffer). Draining the wrong port left the head un-popped -> infinite
		// re-emit + HoL once the InvRsp routing fix delivers here.
		c.RDMAInvRspInside.RetrieveIncoming()
		return true
	}
	req := c.invalidationFromOutside[i]

	// [R1] req.Meta().Src here is the peer's RDMAInvReqOutside (set by
	// peer's sendInvReq). Reroute Dst to peer's typed RDMAInvRspOutside
	// so the response cannot be head-of-line blocked behind queued REQs
	// at peer's REQ port.
	rspDst := string(req.Meta().Src)
	rspDst = strings.Replace(rspDst, ".RDMAInvReqOutside", ".RDMAInvRspOutside", 1)

	rspToOutside := mem.InvRspBuilder{}.
		WithSrc(c.RDMAInvRspOutside.AsRemote()).
		WithDst(sim.RemotePort(rspDst)).
		WithRspTo(req.ReqFrom).
		// [R1] SrcRDMA was previously RDMARequestOutside (a logical
		// peer-RDMA identifier used by some directory logic). With the
		// split, RDMADataReqOutside is the equivalent stable identifier
		// for this RDMA's data-request egress.
		WithSrcRDMA(c.RDMADataReqOutside.AsRemote()).
		Build()

	err := c.RDMAInvRspOutside.Send(rspToOutside)
	if err == nil {
		// [ITER19 INV-RSP DRAIN FIX] drain RDMAInvRspInside (the port
		// processFromInvRspInside peeked), not RDMAInvInside.
		c.RDMAInvRspInside.RetrieveIncoming()
		c.invalidationFromOutside = append(c.invalidationFromOutside[:i], c.invalidationFromOutside[i+1:]...)
		return true
	}

	return false
}

func (c *Comp) findTransactionByRspToID(
	rspTo string,
	transactions []transaction,
) int {
	for i, trans := range transactions {
		if trans.toOutside != nil && trans.toOutside.Meta().ID == rspTo {
			return i
		}

		if trans.toInside != nil && trans.toInside.Meta().ID == rspTo {
			return i
		}
	}

	// log.Panicf("transaction %s not found", rspTo)
	// return 0
	return -1
}

func (c *Comp) findInvReqByRspToID(
	rspTo string,
	req []*mem.InvReq,
) int {
	for i, rq := range req {
		if rq.Meta().ID == rspTo {
			return i
		}
	}

	return -1
}

func (c *Comp) cloneReq(origin mem.AccessReq) mem.AccessReq {
	switch origin := origin.(type) {
	case *mem.ReadReq:
		read := mem.ReadReqBuilder{}.
			WithSrc(origin.Src).
			WithDst(origin.Dst).
			WithReqFrom(c.Name()).
			WithPID(origin.GetPID()).
			WithAddress(origin.Address).
			WithVAddr(origin.GetVAddr()).
			WithByteSize(origin.AccessByteSize).
			Build()
		read.SetSrcRDMA(origin.SrcRDMA)
		read.PathProbe = origin.PathProbe
		if mempath.Enabled {
			read.PathProbe.Stamp(c.Name(), mempath.EvRDMAOut, c.Engine.CurrentTime())
		}
		return read
	case *mem.WriteReq:
		write := mem.WriteReqBuilder{}.
			WithSrc(origin.Src).
			WithDst(origin.Dst).
			WithReqFrom(c.Name()).
			WithPID(origin.GetPID()).
			WithAddress(origin.Address).
			WithVAddr(origin.GetVAddr()).
			WithData(origin.Data).
			WithDirtyMask(origin.DirtyMask).
			WithInfo((*(c.dirtyMask))[c.deviceID-1][origin.GetPID()][origin.GetVAddr()>>c.log2PageSize]).
			Build()
		write.SetSrcRDMA(origin.SrcRDMA)
		write.PathProbe = origin.PathProbe
		if mempath.Enabled {
			write.PathProbe.Stamp(c.Name(), mempath.EvRDMAOut, c.Engine.CurrentTime())
		}
		return write
	default:
		log.Panicf("cannot clone request of type %s",
			reflect.TypeOf(origin))
	}
	return nil
}

func (c *Comp) cloneRsp(origin mem.AccessRsp, rspTo string, addr uint64) mem.AccessRsp {
	if addr != origin.GetOrigin().GetAddress() {
		rspTo = ""
	}

	switch origin := origin.(type) {
	case *mem.DataReadyRsp:
		rsp := mem.DataReadyRspBuilder{}.
			WithSrc(origin.Src).
			WithDst(origin.Dst).
			WithRspTo(rspTo).
			WithData(origin.Data).
			WithOrigin(origin.Origin).
			Build()
		rsp.PathProbe = origin.PathProbe
		if mempath.Enabled {
			rsp.PathProbe.Stamp(c.Name(), mempath.EvRDMAIn, c.Engine.CurrentTime())
		}
		return rsp
	case *mem.WriteDoneRsp:
		rsp := mem.WriteDoneRspBuilder{}.
			WithSrc(origin.Src).
			WithDst(origin.Dst).
			WithRspTo(rspTo).
			WithOrigin(origin.Origin).
			Build()
		rsp.PathProbe = origin.PathProbe
		if mempath.Enabled {
			rsp.PathProbe.Stamp(c.Name(), mempath.EvRDMAIn, c.Engine.CurrentTime())
		}
		return rsp
	default:
		log.Panicf("cannot clone request of type %s",
			reflect.TypeOf(origin))
	}
	return nil
}

// SetFreq sets freq
func (c *Comp) SetFreq(freq sim.Freq) {
	c.TickingComponent.Freq = freq
}

func (c *Comp) traceInsideOutStart(req mem.AccessReq, cloned mem.AccessReq) {
	if !c.traceProcess {
		return
	}

	if len(c.Hooks()) == 0 {
		return
	}

	tracing.StartTaskWithSpecificLocation(
		tracing.MsgIDAtReceiver(req, c),
		req.Meta().ID+"_req_out",
		c,
		"req_in",
		reflect.TypeOf(req).String(),
		c.Name()+".InsideOut",
		req,
	)

	tracing.StartTaskWithSpecificLocation(
		cloned.Meta().ID+"_req_out",
		tracing.MsgIDAtReceiver(req, c),
		c,
		"req_out",
		reflect.TypeOf(req).String(),
		c.Name()+".InsideOut",
		cloned,
	)
}

func (c *Comp) traceOutsideInStart(req mem.AccessReq, cloned mem.AccessReq) {
	if !c.traceProcess {
		return
	}

	if len(c.Hooks()) == 0 {
		return
	}

	tracing.StartTaskWithSpecificLocation(
		tracing.MsgIDAtReceiver(req, c),
		req.Meta().ID+"_req_out",
		c,
		"req_in",
		reflect.TypeOf(req).String(),
		c.Name()+".OutsideIn",
		req,
	)

	tracing.StartTaskWithSpecificLocation(
		cloned.Meta().ID+"_req_out",
		tracing.MsgIDAtReceiver(req, c),
		c,
		"req_out",
		reflect.TypeOf(req).String(),
		c.Name()+".OutsideIn",
		cloned,
	)
}

func (c *Comp) traceInsideOutEnd(trans transaction) {
	if !c.traceProcess {
		return
	}

	if len(c.Hooks()) == 0 {
		return
	}

	tracing.TraceReqFinalize(trans.toOutside, c)
	tracing.TraceReqComplete(trans.fromInside, c)
}

func (c *Comp) traceOutsideInEnd(trans transaction) {
	tracing.TraceReqFinalize(trans.toInside, c)
	tracing.TraceReqComplete(trans.fromOutside, c)
}

func (c *Comp) printWriteMask(req mem.AccessReq) {
	if req.GetVAddr() == 0 {
		return
	}

	switch req := req.(type) {
	case *mem.WriteReq:
		fmt.Printf("\n======================================================================================\n")
		pid := req.GetPID()
		vpn := req.GetVAddr() >> c.log2PageSize
		// var reqFrom uint64
		// fmt.Sscanf(req.ReqFrom, "GPU[%d].RDMA", &reqFrom)

		fmt.Printf("[GPU[%d].RDMA]\tRemote Write Req VPN %x from %s\n", c.deviceID, vpn, req.ReqFrom)
		for i, list := range *(c.dirtyMask) {
			fmt.Printf("\t\tDirtyMask GPU %d: %v\n", i+1, list[pid][vpn])
		}
		for i, list := range *(c.readMask) {
			fmt.Printf("\t\tReadMask  GPU %d: %v\n", i+1, list[pid][vpn])
		}

		// for j, mask := range *(c.dirtyMask) {
		// 	if uint64(j) == reqFrom-1 {
		// 		continue
		// 	}
		// 	if list, _ := mask[pid][vpn]; len(list) != 0 {
		// 		sharing := false
		// 		for idx, b := range req.Info.([]uint8) {
		// 			if b == 1 && list[idx] == 1 {
		// 				fmt.Printf("\t\tShared Write Detected with GPU %d and GPU %d\n", j+1, c.deviceID)
		// 				sharing = true
		// 				break
		// 			}
		// 		}
		// 		if !sharing {
		// 			fmt.Printf("\t\tFalse Shared Write Detected\n")
		// 		}
		// 	}
		// }
		// for j, mask := range *(c.readMask) {
		// 	if uint64(j) == reqFrom-1 {
		// 		continue
		// 	}
		// 	if list, _ := mask[pid][vpn]; len(list) != 0 {
		// 		sharing := false
		// 		for idx, b := range req.Info.([]uint8) {
		// 			if b == 1 && list[idx] == 1 {
		// 				fmt.Printf("\t\tShared Read/Write Detected with GPU %d and GPU %d\n", j+1, c.deviceID)
		// 				sharing = true
		// 				break
		// 			}
		// 		}
		// 		if !sharing {
		// 			fmt.Printf("\t\tFalse Shared Read/Write Detected\n")
		// 		}
		// 	}
		// }
		fmt.Printf("======================================================================================\n\n")
	case *mem.ReadReq:
		fmt.Printf("\n======================================================================================\n")
		pid := req.GetPID()
		vpn := req.GetVAddr() >> c.log2PageSize
		// var reqFrom uint64
		// fmt.Sscanf(req.ReqFrom, "GPU[%d].RDMA", &reqFrom)

		fmt.Printf("[GPU[%d].RDMA]\tRemote Read Req VPN %x from %s\n", c.deviceID, vpn, req.ReqFrom)
		for i, list := range *(c.dirtyMask) {
			fmt.Printf("\t\tDirtyMask GPU %d: %v\n", i+1, list[pid][vpn])
		}
		for i, list := range *(c.readMask) {
			fmt.Printf("\t\tReadMask  GPU %d: %v\n", i+1, list[pid][vpn])
		}

		// for j, mask := range *(c.dirtyMask) {
		// 	if uint64(j) == reqFrom-1 {
		// 		continue
		// 	}
		// 	if list, _ := mask[pid][vpn]; len(list) != 0 {
		// 		sharing := false
		// 		for idx, b := range req.Info.([]uint8) {
		// 			if b == 1 && list[idx] == 1 {
		// 				fmt.Printf("\t\tShared Write Detected with GPU %d and GPU %d\n", j+1, c.deviceID)
		// 				sharing = true
		// 				break
		// 			}
		// 		}
		// 		if !sharing {
		// 			fmt.Printf("\t\tFalse Shared Write Detected\n")
		// 		}
		// 	}
		// }
		// for j, mask := range *(c.readMask) {
		// 	if uint64(j) == reqFrom-1 {
		// 		continue
		// 	}
		// 	if list, _ := mask[pid][vpn]; len(list) != 0 {
		// 		sharing := false
		// 		for idx, b := range req.Info.([]uint8) {
		// 			if b == 1 && list[idx] == 1 {
		// 				fmt.Printf("\t\tShared Read/Write Detected with GPU %d and GPU %d\n", j+1, c.deviceID)
		// 				sharing = true
		// 				break
		// 			}
		// 		}
		// 		if !sharing {
		// 			fmt.Printf("\t\tFalse Shared Read/Write Detected\n")
		// 		}
		// 	}
		// }
		fmt.Printf("======================================================================================\n\n")
	default:
	}
}

func (c *Comp) recordMsgSend(req sim.Msg) {
	req = req.Clone()

	what := ""
	switch req := req.(type) {
	case *mem.ReadReq:
		what = "Read Req" + fmt.Sprintf(" %d", req.TrafficBytes)
	case *mem.WriteReq:
		what = "Write Req" + fmt.Sprintf(" %d", req.TrafficBytes)
	case *mem.InvReq:
		what = "Inv Req" + fmt.Sprintf(" %d", req.TrafficBytes)
	case *mem.DataReadyRsp:
		what = "Read Rsp" + fmt.Sprintf(" %d", req.TrafficBytes)
	case *mem.WriteDoneRsp:
		what = "Write Rsp" + fmt.Sprintf(" %d", req.TrafficBytes)
	case *mem.InvRsp:
		what = "Inv Rsp" + fmt.Sprintf(" %d", req.TrafficBytes)
	default:
	}

	tracing.TraceReqReceive(req, c)
	tracing.AddTaskStep(req.Meta().ID, c, what)
	tracing.TraceReqComplete(req, c)
}
