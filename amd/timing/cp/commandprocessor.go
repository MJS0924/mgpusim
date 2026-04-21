package cp

import (
	"fmt"
	"log"
	"reflect"

	"github.com/sarchlab/akita/v4/mem/cache"
	"github.com/sarchlab/akita/v4/mem/idealmemcontroller"
	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/mem/vm/tlb"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/tracing"
	"github.com/sarchlab/mgpusim/v4/amd/protocol"
	"github.com/sarchlab/mgpusim/v4/amd/sampling"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cp/internal/dispatching"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cp/internal/resource"
	"github.com/sarchlab/mgpusim/v4/amd/timing/pagemigrationcontroller"
	"github.com/sarchlab/mgpusim/v4/amd/timing/rdma"
)

// CommandProcessor is an Akita component that is responsible for receiving
// requests from the driver and dispatch the requests to other parts of the
// GPU.
type CommandProcessor struct {
	*sim.TickingComponent
	sim.HookableBase
	deviceID uint32 // GPU 장치 ID

	Dispatchers        []dispatching.Dispatcher
	DMAEngine          sim.Port
	Driver             sim.Port
	TLBs               []sim.Port
	CUs                []sim.RemotePort
	AddressTranslators []sim.Port
	RDMA               sim.Port
	PMC                sim.Port
	L1VCaches          []sim.Port
	L1SCaches          []sim.Port
	L1ICaches          []sim.Port
	CohDirectory       sim.Port
	L2Caches           []sim.Port
	DRAMControllers    []*idealmemcontroller.Comp
	GMMU               sim.Port // GMMU와 연결된 포트
	L1VROBs            []sim.Port
	L1SROBs            []sim.Port
	L1IROBs            []sim.Port

	ToDriver             sim.Port
	ToDMA                sim.Port
	ToCUs                sim.Port
	ToTLBs               sim.Port
	ToAddressTranslators sim.Port
	ToCaches             sim.Port
	ToCohDir             sim.Port
	ToRDMA               sim.Port
	ToPMC                sim.Port
	ToGMMU               sim.Port // GMMU로 명령을 보내는 포트
	ToROBs               sim.Port

	currShootdownRequest *protocol.ShootDownCommand
	currRestartRequest   *protocol.GPURestartReq
	currFlushRequest     *protocol.FlushReq

	numTLBs                      uint64
	numCUAck                     uint64
	numAddrTranslationFlushAck   uint64
	numAddrTranslationRestartAck uint64
	numTLBAck                    uint64
	numCacheACK                  uint64
	numCohDirACK                 uint64
	numROBACK                    uint64 // ROB ack 개수

	shootDownInProcess bool
	cacheCtrlInProcess bool

	bottomKernelLaunchReqIDToTopReqMap map[string]*protocol.LaunchKernelReq
	bottomMemCopyH2DReqIDToTopReqMap   map[string]*protocol.MemCopyH2DReq
	bottomMemCopyD2HReqIDToTopReqMap   map[string]*protocol.MemCopyD2HReq

	pageMigrationPolicy uint64

	returnValue []bool
	printReturn bool
	recordTime  sim.VTimeInSec
}

// CUInterfaceForCP defines the interface that a CP requires from CU.
type CUInterfaceForCP interface {
	resource.DispatchableCU

	// ControlPort returns a port on the CU that the CP can send controlling
	// messages to.
	ControlPort() sim.RemotePort
}

// RegisterCU allows the Command Processor to control the CU.
func (p *CommandProcessor) RegisterCU(cu CUInterfaceForCP) {
	p.CUs = append(p.CUs, cu.ControlPort())
	for _, d := range p.Dispatchers {
		d.RegisterCU(cu)
	}
}

// Tick ticks
func (p *CommandProcessor) Tick() bool {
	// now := p.Engine.CurrentTime()
	// p.printReturn = false
	// if now >= p.recordTime+0.000001 {
	// 	p.recordTime = now
	// 	p.printReturn = true
	// }

	madeProgress := false

	madeProgress = p.tickDispatchers() || madeProgress
	p.returnValue[1] = p.processReqFromDriver()
	madeProgress = p.returnValue[1] || madeProgress
	madeProgress = p.processRspFromInternal() || madeProgress

	if p.printReturn {
		fmt.Printf("[DEBUG CP %d]\treturn: %v\n", p.deviceID, p.returnValue)
	}
	return madeProgress
}

func (p *CommandProcessor) tickDispatchers() (madeProgress bool) {
	for _, d := range p.Dispatchers {
		madeProgress = d.Tick() || madeProgress
	}

	p.returnValue[0] = madeProgress
	return madeProgress
}

func (p *CommandProcessor) processReqFromDriver() bool {
	msg := p.ToDriver.PeekIncoming()
	if msg == nil {
		return false
	}

	// fmt.Printf("[CP %d]\tReceived message of type %s from Driver\n", p.deviceID, reflect.TypeOf(msg))
	switch req := msg.(type) {
	case *protocol.LaunchKernelReq:
		return p.processLaunchKernelReq(req)
	case *protocol.FlushReq:
		if req.Cache {
			return p.processCacheFlushReq(req)
		}
		return p.processCohDirFlushReq(req)
	case *protocol.MemCopyD2HReq, *protocol.MemCopyH2DReq:
		return p.processMemCopyReq(req)
	case *protocol.RDMADrainCmdFromDriver:
		return p.processRDMADrainCmd(req)
	case *protocol.RDMARestartCmdFromDriver:
		return p.processRDMARestartCommand(req)
	case *protocol.ShootDownCommand:
		return p.processShootdownCommand(req)
	case *protocol.GPURestartReq:
		return p.processGPURestartReq(req)
	case *protocol.PageMigrationReqToCP:
		return p.processPageMigrationReq(req)
	case *vm.PTEInvalidationReq:
		return p.processPTEInvalidationReq(req)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromInternal() bool {
	madeProgress := false

	madeProgress = p.processRspFromDMAs() || madeProgress
	madeProgress = p.processRspFromRDMAs() || madeProgress
	madeProgress = p.processRspFromCUs() || madeProgress
	madeProgress = p.processRspFromROBs() || madeProgress
	madeProgress = p.processRspFromATs() || madeProgress
	madeProgress = p.processRspFromCohDirs() || madeProgress
	madeProgress = p.processRspFromCaches() || madeProgress
	madeProgress = p.processRspFromTLBs() || madeProgress
	madeProgress = p.processRspFromPMC() || madeProgress
	madeProgress = p.processRspFromGMMU() || madeProgress

	p.returnValue[2] = madeProgress
	return madeProgress
}

func (p *CommandProcessor) processRspFromDMAs() bool {
	msg := p.ToDMA.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *sim.GeneralRsp:
		return p.processMemCopyRsp(req)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromRDMAs() bool {
	msg := p.ToRDMA.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *rdma.DrainRsp:
		return p.processRDMADrainRsp(req)
	case *rdma.RestartRsp:
		return p.processRDMARestartRsp(req)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromCUs() bool {
	msg := p.ToCUs.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *protocol.CUPipelineFlushRsp:
		return p.processCUPipelineFlushRsp(req)
	case *protocol.CUPipelineRestartRsp:
		return p.processCUPipelineRestartRsp(req)
	}

	return false
}

func (p *CommandProcessor) processRspFromROBs() bool {
	item := p.ToROBs.PeekIncoming()
	if item == nil {
		return false
	}

	msg := item.(*mem.ControlMsg)
	// switch req := msg.(type) {
	// case *protocol.CUPipelineFlushRsp:
	// 	return p.processCUPipelineFlushRsp(req)
	// case *protocol.CUPipelineRestartRsp:
	// 	return p.processCUPipelineRestartRsp(req)
	// }
	if msg.NotifyDone && msg.DiscardTransations {
		return p.processROBFlushRsp(msg)
	} else if msg.NotifyDone && msg.Restart {
		return p.processROBRestartRsp(msg)
	}

	return false
}

func (p *CommandProcessor) processRspFromCohDirs() bool {
	msg := p.ToCohDir.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *cache.FlushRsp:
		return p.processCohDirFlushRsp(req)
	case *cache.RestartRsp:
		return p.processCohDirRestartRsp(req)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromCaches() bool {
	msg := p.ToCaches.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *cache.FlushRsp:
		req = msg.(*cache.FlushRsp)
		if req.FromL1I {
			return p.processL1ICacheFlushRsp(req)
		} else {
			return p.processCacheFlushRsp(req)
		}
	case *cache.RestartRsp:
		req = msg.(*cache.RestartRsp)
		if req.FromL1I {
			return p.processL1ICacheRestartRsp(req)
		} else {
			return p.processCacheRestartRsp(req)
		}
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromATs() bool {
	item := p.ToAddressTranslators.PeekIncoming()
	if item == nil {
		return false
	}

	msg := item.(*mem.ControlMsg)

	if p.numAddrTranslationFlushAck > 0 {
		return p.processAddressTranslatorFlushRsp(msg)
	} else if p.numAddrTranslationRestartAck > 0 {
		return p.processAddressTranslatorRestartRsp(msg)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromTLBs() bool {
	msg := p.ToTLBs.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *tlb.FlushRsp:
		return p.processTLBFlushRsp(req)
	case *tlb.RestartRsp:
		return p.processTLBRestartRsp(req)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromPMC() bool {
	msg := p.ToPMC.PeekIncoming()
	if msg == nil {
		return false
	}

	switch req := msg.(type) {
	case *pagemigrationcontroller.PageMigrationRspFromPMC:
		return p.processPageMigrationRsp(req)
	}

	panic("never")
}

func (p *CommandProcessor) processRspFromGMMU() bool {
	msg := p.ToGMMU.PeekIncoming()
	if msg == nil {
		return false
	}

	switch rsp := msg.(type) {
	case *vm.PTEInvalidationRsp:
		return p.processPTEInvalidationRsp(rsp)
	case *vm.FlushRsp:
		return p.processGMMUFlushRsp(rsp)
	case *vm.RestartRsp:
		return p.processGMMURestartRsp(rsp)
	default:
		log.Panicf("[CP]\tCP canot handle request of type %s", reflect.TypeOf(rsp))
	}

	return false
}

func (p *CommandProcessor) processLaunchKernelReq(
	req *protocol.LaunchKernelReq,
) bool {
	d := p.findAvailableDispatcher()

	if d == nil {
		return false
	}

	if *sampling.SampledRunnerFlag {
		sampling.SampledEngineInstance.Reset()
	}
	d.StartDispatching(req)
	p.ToDriver.RetrieveIncoming()

	tracing.TraceReqReceive(req, p)
	// tracing.TraceReqInitiate(&reqToBottom, now, p,
	// 	tracing.MsgIDAtReceiver(req, p))

	return true
}

func (p *CommandProcessor) findAvailableDispatcher() dispatching.Dispatcher {
	for _, d := range p.Dispatchers {
		if !d.IsDispatching() {
			return d
		}
	}

	return nil
}
func (p *CommandProcessor) processRDMADrainCmd(
	cmd *protocol.RDMADrainCmdFromDriver,
) bool {
	fmt.Printf("[CP %d]\tReceive RDMA Drain Command\n", p.deviceID)
	req := rdma.DrainReqBuilder{}.
		WithSrc(p.ToRDMA.AsRemote()).
		WithDst(p.RDMA.AsRemote()).
		Build()

	err := p.ToRDMA.Send(req)
	if err != nil {
		panic(err)
	}

	p.ToDriver.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processRDMADrainRsp(
	rsp *rdma.DrainRsp,
) bool {
	if p.ToDriver == nil {
		panic("ToDriver port is nil")
	}
	if p.Driver == nil {
		panic("Driver port is nil")
	}

	req := protocol.NewRDMADrainRspToDriver(p.ToDriver, p.Driver)

	fmt.Printf("[CP %d]\tSend RDMA Drain Response\n", p.deviceID)
	err := p.ToDriver.Send(req)
	if err != nil {
		panic(err)
	}

	p.ToRDMA.RetrieveIncoming()

	return true
}

// flush 순서
// CU, ROB, AT, Cache, TLB, GMMU
// restart 순서
// GMMU, cache, TLB, AT, ROB, CU

// L1I cache는 L1V, L1S cache와 반대로
// AT의 top과 L1 cache의 bottom이 연결되어 있음, 따라서 버그 발생 시 아래와 같이 순서 변경 필요
// flush:		CU, ROB, L1I, AT, L2/L1V/L1S, TLB, GMMU
// restart: 	GMMU, L2/L1V/L1S, TLB, AT, L1I, ROB, CU

// Coherence Directory 추가에 따라 다음과 같이 flush/restart 수행
// flush:		CU, ROB, L1I, AT, L1V/L1S/L2, CohDir, TLB, GMMU
// restart: 	GMMU, CohDir, L1V/L1S/L2, TLB, AT, L1I, ROB, CU

func (p *CommandProcessor) processShootdownCommand(
	cmd *protocol.ShootDownCommand,
) bool {
	if p.shootDownInProcess == true {
		return false
	}

	fmt.Printf("[CP%d]\tReceive ShootDown CMD\n\t\tSending CU flush\n", p.deviceID)

	p.currShootdownRequest = cmd
	p.shootDownInProcess = true
	for _, d := range p.Dispatchers {
		d.Pause()
	}

	for i := 0; i < len(p.CUs); i++ {
		p.numCUAck++
		req := protocol.CUPipelineFlushReqBuilder{}.
			WithSrc(p.ToCUs.AsRemote()).
			WithDst(p.CUs[i]).
			Build()
		p.ToCUs.Send(req)
	}

	kind := "Page Migration"
	if p.currShootdownRequest.ForDuplication {
		kind = "Page Duplication"
	} else if p.currShootdownRequest.ForInvalidation {
		kind = "Page Invalidation"
	}

	tracing.StartTask(
		p.currShootdownRequest.Meta().ID,
		"",
		p,
		kind,
		fmt.Sprintf("ShootDown GPU%d", p.deviceID),
		nil,
	)

	p.ToDriver.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processCUPipelineFlushRsp(
	rsp *protocol.CUPipelineFlushRsp,
) bool {
	p.numCUAck--

	if p.numCUAck == 0 {
		fmt.Printf("[CP %d]\tComplete CU flush\n\t\tStart ROB Flush\n", p.deviceID)

		for i := 0; i < len(p.L1VROBs); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToROBs.AsRemote()).
				WithDst(p.L1VROBs[i].AsRemote()).
				ToDiscardTransactions().
				Build()
			err := p.ToROBs.Send(req)
			if err != nil {
				fmt.Printf("[CP %d]\tCritical Error: Failed to send ROB flush request\n", p.deviceID)
			}
			p.numROBACK++
		}

		for i := 0; i < len(p.L1SROBs); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToROBs.AsRemote()).
				WithDst(p.L1SROBs[i].AsRemote()).
				ToDiscardTransactions().
				Build()
			err := p.ToROBs.Send(req)
			if err != nil {
				fmt.Printf("[CP %d]\tCritical Error: Failed to send ROB flush request\n", p.deviceID)
			}
			p.numROBACK++
		}

		for i := 0; i < len(p.L1IROBs); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToROBs.AsRemote()).
				WithDst(p.L1IROBs[i].AsRemote()).
				ToDiscardTransactions().
				Build()
			err := p.ToROBs.Send(req)
			if err != nil {
				fmt.Printf("[CP %d]\tCritical Error: Failed to send ROB flush request\n", p.deviceID)
			}
			p.numROBACK++
		}
	}

	p.ToCUs.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processROBFlushRsp(
	rsp *mem.ControlMsg,
) bool {
	p.numROBACK--

	if p.numROBACK == 0 {
		if p.cacheCtrlInProcess {
			return false
		}
		fmt.Printf("[CP %d]\tComplete ROB flush\n\t\tStart L1I Cache Flush\n", p.deviceID)

		for _, port := range p.L1ICaches {
			p.flushAndResetL1Cache(port)
		}
		p.cacheCtrlInProcess = true
	}

	p.ToROBs.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processL1ICacheFlushRsp(
	rsp *cache.FlushRsp,
) bool {
	p.numCacheACK--
	p.ToCaches.RetrieveIncoming()

	if p.numCacheACK == 0 {
		if p.shootDownInProcess {
			fmt.Printf("[CP %d]\tComplete L1I Cache Flush\n\t\tStart Address Translator Flush\n", p.deviceID)
			p.cacheCtrlInProcess = false

			for i := 0; i < len(p.AddressTranslators); i++ {
				req := mem.ControlMsgBuilder{}.
					WithSrc(p.ToAddressTranslators.AsRemote()).
					WithDst(p.AddressTranslators[i].AsRemote()).
					ToDiscardTransactions().
					Build()
				p.ToAddressTranslators.Send(req)
				p.numAddrTranslationFlushAck++
			}
			return true
		}

		fmt.Printf("[CP %d]\tComplete Cache Flush\n\t\tSend Cache Flush Response to Driver\n", p.deviceID)
		return p.processRegularCacheFlush(rsp)
	}

	return true
}

func (p *CommandProcessor) processAddressTranslatorFlushRsp(
	msg *mem.ControlMsg,
) bool {
	p.numAddrTranslationFlushAck--

	if p.numAddrTranslationFlushAck == 0 {
		if p.cacheCtrlInProcess {
			return false
		}
		fmt.Printf("[CP %d]\tComplete Address Translator Flush\n\t\tStart L1S/L1V/L2 Cache Flush\n", p.deviceID)

		for _, port := range p.L1SCaches {
			p.flushAndResetL1Cache(port)
		}

		for _, port := range p.L1VCaches {
			p.flushAndResetL1Cache(port)
		}

		for _, port := range p.L2Caches {
			p.flushAndResetL2Cache(port)
		}
		p.cacheCtrlInProcess = true
	}

	p.ToAddressTranslators.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) flushAndResetL1Cache(
	port sim.Port,
) {
	req := cache.FlushReqBuilder{}.
		WithSrc(p.ToCaches.AsRemote()).
		WithDst(port.AsRemote()).
		PauseAfterFlushing().
		DiscardInflight().
		InvalidateAllCacheLines().
		Build()

	if p.currShootdownRequest.ToAccessingGPU {
		req.InvalidateAllCachelines = true
	}

	p.ToCaches.Send(req)
	p.numCacheACK++
}

func (p *CommandProcessor) flushAndResetL2Cache(port sim.Port) {
	req := cache.FlushReqBuilder{}.
		WithSrc(p.ToCaches.AsRemote()).
		WithDst(port.AsRemote()).
		PauseAfterFlushing().
		DiscardInflight().
		// InvalidateAllCacheLines().
		Build()

	p.ToCaches.Send(req)
	p.numCacheACK++
}

func (p *CommandProcessor) processCacheFlushRsp(
	rsp *cache.FlushRsp,
) bool {
	p.numCacheACK--
	p.ToCaches.RetrieveIncoming()

	if p.numCacheACK == 0 {
		p.cacheCtrlInProcess = false
		if p.shootDownInProcess {
			fmt.Printf("[CP %d]\tComplete L1S/L1V/L2 Cache Flush\n\t\tStart TLB shootdown\n", p.deviceID)
			// TODO: 모든 GPU의 cache flush가 완료된 이후에 coh dir flush 수행할 것
			// return p.processCohDirFlush()
			return p.processCacheFlushCausedByTLBShootdown(rsp)
		}

		fmt.Printf("[CP %d]\tComplete Cache Flush\n\t\tSend Cache Flush Response to Driver\n", p.deviceID)
		return p.processRegularCacheFlush(rsp)
	}

	return true
}

func (p *CommandProcessor) processRegularCacheFlush(
	flushRsp *cache.FlushRsp,
) bool {
	rsp := sim.GeneralRspBuilder{}.
		WithSrc(p.ToDriver.AsRemote()).
		WithDst(p.currFlushRequest.Src).
		WithOriginalReq(p.currFlushRequest).
		Build()

	p.ToDriver.Send(rsp)

	tracing.TraceReqComplete(p.currFlushRequest, p)
	p.currFlushRequest = nil

	return true
}

func (p *CommandProcessor) processCacheFlushCausedByTLBShootdown(
	flushRsp *cache.FlushRsp,
) bool {
	p.currFlushRequest = nil

	for i := 0; i < len(p.TLBs); i++ {
		shootDownCmd := p.currShootdownRequest
		req := tlb.FlushReqBuilder{}.
			WithSrc(p.ToTLBs.AsRemote()).
			WithDst(p.TLBs[i].AsRemote()).
			WithPID(shootDownCmd.PID).
			WithVAddrs(shootDownCmd.VAddr).
			Build()

		if p.currShootdownRequest.ToAccessingGPU {
			req.InvalidateAllLines = true
		}

		p.ToTLBs.Send(req)
		p.numTLBAck++
	}

	return true
}

func (p *CommandProcessor) processTLBFlushRsp(
	rsp *tlb.FlushRsp,
) bool {
	p.numTLBAck--

	if p.numTLBAck == 0 {
		fmt.Printf("[CP %d]\tComplete TLB Flush\n\t\tStart GMMU Flush\n", p.deviceID)
		p.currFlushRequest = nil
		shootDownCmd := p.currShootdownRequest

		req := tlb.FlushReqBuilder{}.
			WithSrc(p.ToGMMU.AsRemote()).
			WithDst(p.GMMU.AsRemote()).
			WithPID(shootDownCmd.PID).
			WithVAddrs(shootDownCmd.VAddr).
			Build()

		// req := vm.FlushReqBuilder{}.
		// 	WithSrc(p.ToGMMU.AsRemote()).
		// 	WithDst(p.GMMU.AsRemote()).
		// 	Build()
		p.ToGMMU.Send(req)
	}

	p.ToTLBs.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processGMMUFlushRsp(
	rsp *vm.FlushRsp,
) bool {
	fmt.Printf("[CP %d]\tComplete GMMU Flush\n\t\tStart Coh Dir Flush\n", p.deviceID)

	p.processCohDirFlush()
	p.ToGMMU.RetrieveIncoming()
	return true
}

func (p *CommandProcessor) processCohDirFlush() bool {
	port := p.CohDirectory
	req := cache.FlushReqBuilder{}.
		WithSrc(p.ToCohDir.AsRemote()).
		WithDst(port.AsRemote()).
		// WithAddr().
		Build()

	if p.shootDownInProcess {
		req.DiscardInflight = true
		req.PauseAfterFlushing = true
	} else {
		req.DiscardInflight = true
		req.PauseAfterFlushing = false
	}

	p.ToCohDir.Send(req)
	p.numCohDirACK++
	p.cacheCtrlInProcess = true

	return true
}

func (p *CommandProcessor) processCohDirFlushRsp(
	rsp *cache.FlushRsp,
) bool {
	p.numCohDirACK--
	p.ToCohDir.RetrieveIncoming()

	if p.numCohDirACK == 0 {
		p.cacheCtrlInProcess = false

		if p.shootDownInProcess {
			fmt.Printf("[CP %d]\tComplete Coh Dir Flush\n\t\tShootdown Finished, Send response to driver\n", p.deviceID)

			req := protocol.NewShootdownCompleteRsp(p.ToDriver, p.Driver)
			err := p.ToDriver.Send(req)
			if err != nil {
				fmt.Printf("[CP %d]\t[Warning]\tFailed to send shootdown complete response to Driver\n", p.deviceID)
				return false
			}

			p.shootDownInProcess = false
			return true
		}

		return p.processRegularCohDirFlush(rsp)
	}

	return true
}

func (p *CommandProcessor) processRegularCohDirFlush(
	flushRsp *cache.FlushRsp,
) bool {
	rsp := sim.GeneralRspBuilder{}.
		WithSrc(p.ToDriver.AsRemote()).
		WithDst(p.currFlushRequest.Src).
		WithOriginalReq(p.currFlushRequest).
		Build()

	p.ToDriver.Send(rsp)

	tracing.TraceReqComplete(p.currFlushRequest, p)
	p.currFlushRequest = nil

	return true
}

func (p *CommandProcessor) processRDMARestartCommand(
	cmd *protocol.RDMARestartCmdFromDriver,
) bool {
	// fmt.Printf("[CP %d]\tReceived RDMA Restart Command\n", p.deviceID)
	req := rdma.RestartReqBuilder{}.
		WithSrc(p.ToRDMA.AsRemote()).
		WithDst(p.RDMA.AsRemote()).
		Build()

	p.ToRDMA.Send(req)

	p.ToDriver.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processRDMARestartRsp(rsp *rdma.RestartRsp) bool {
	req := protocol.NewRDMARestartRspToDriver(p.ToDriver, p.Driver)
	p.ToDriver.Send(req)
	p.ToRDMA.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processGPURestartReq(
	cmd *protocol.GPURestartReq,
) bool {
	if p.cacheCtrlInProcess {
		return false
	}

	fmt.Printf("[CP %d]\tReceive GPU Restart Req\n\t\tStart CohDir Restart\n", p.deviceID)

	p.currRestartRequest = cmd
	port := p.CohDirectory
	req := cache.RestartReqBuilder{}.
		WithSrc(p.ToCohDir.AsRemote()).
		WithDst(port.AsRemote()).
		Build()

	p.ToCohDir.Send(req)
	p.numCohDirACK++
	p.cacheCtrlInProcess = true

	p.ToDriver.RetrieveIncoming()

	return true

	// fmt.Printf("[CP %d]\tReceive GPU Restart Req\n\t\tStart GMMU Restart\n", p.deviceID)
	// p.currRestartRequest = cmd

	// rsp := vm.RestartReqBuilder{}.
	// 	WithSrc(p.ToGMMU.AsRemote()).
	// 	WithDst(p.GMMU.AsRemote()).
	// 	Build()
	// p.ToGMMU.Send(rsp)

	// p.ToDriver.RetrieveIncoming()

	// return true
}

func (p *CommandProcessor) processCohDirRestartRsp(
	rsp *cache.RestartRsp,
) bool {
	p.numCohDirACK--

	if p.numCohDirACK == 0 {
		fmt.Printf("[CP %d]\tComplete CohDir Restart\n\t\tStart GMMU Restart\n", p.deviceID)

		p.cacheCtrlInProcess = false

		rsp := vm.RestartReqBuilder{}.
			WithSrc(p.ToGMMU.AsRemote()).
			WithDst(p.GMMU.AsRemote()).
			Build()
		p.ToGMMU.Send(rsp)
	}

	p.ToCohDir.RetrieveIncoming()
	return true

}

func (p *CommandProcessor) processGMMURestartRsp(
	rsp *vm.RestartRsp,
) bool {
	if p.cacheCtrlInProcess {
		return false
	}

	fmt.Printf("[CP %d]\tComplete GMMU Restart\n\t\tStart L1S/L1V/L2 Cache Restart\n", p.deviceID)
	for _, port := range p.L2Caches {
		p.restartCache(port)
	}
	for _, port := range p.L1SCaches {
		p.restartCache(port)
	}
	for _, port := range p.L1VCaches {
		p.restartCache(port)
	}

	p.cacheCtrlInProcess = true

	// fmt.Printf("[CP %d]\tComplete GMMU Restart\n\t\tStart CohDir Restart\n", p.deviceID)
	// port := p.CohDirectory
	// req := cache.RestartReqBuilder{}.
	// 	WithSrc(p.ToCohDir.AsRemote()).
	// 	WithDst(port.AsRemote()).
	// 	Build()

	// p.ToCohDir.Send(req)
	// p.numCohDirACK++
	// p.cacheCtrlInProcess = true

	p.ToGMMU.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) restartCache(port sim.Port) {
	req := cache.RestartReqBuilder{}.
		WithSrc(p.ToCaches.AsRemote()).
		WithDst(port.AsRemote()).
		Build()

	err := p.ToCaches.Send(req)
	if err != nil {
		panic(err)
	}

	p.numCacheACK++
}

func (p *CommandProcessor) processCacheRestartRsp(
	rsp *cache.RestartRsp,
) bool {
	p.numCacheACK--

	if p.numCacheACK == 0 {
		fmt.Printf("[CP %d]\tComplete L1S/L1V/L2 Cache Restart\n\t\tStart TLB Restart\n", p.deviceID)
		p.cacheCtrlInProcess = false

		for i := 0; i < len(p.TLBs); i++ {
			p.numTLBAck++

			req := tlb.RestartReqBuilder{}.
				WithSrc(p.ToTLBs.AsRemote()).
				WithDst(p.TLBs[i].AsRemote()).
				Build()
			p.ToTLBs.Send(req)
		}
	}

	p.ToCaches.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processTLBRestartRsp(
	rsp *tlb.RestartRsp,
) bool {
	p.numTLBAck--

	if p.numTLBAck == 0 {
		fmt.Printf("[CP %d]\tComplete TLB Restart\n\t\tStart Address Translator Restart\n", p.deviceID)
		for i := 0; i < len(p.AddressTranslators); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToAddressTranslators.AsRemote()).
				WithDst(p.AddressTranslators[i].AsRemote()).
				ToRestart().
				Build()
			p.ToAddressTranslators.Send(req)

			p.numAddrTranslationRestartAck++
		}
	}

	p.ToTLBs.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processAddressTranslatorRestartRsp(
	rsp *mem.ControlMsg,
) bool {
	p.numAddrTranslationRestartAck--

	if p.numAddrTranslationRestartAck == 0 {
		if p.cacheCtrlInProcess {
			return false
		}

		fmt.Printf("[CP %d]\tComplete Address Translator Restart\n\t\tStart L1I Cache Restart\n", p.deviceID)
		for _, port := range p.L1ICaches {
			p.restartCache(port)
		}
		p.cacheCtrlInProcess = true
	}

	p.ToAddressTranslators.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processL1ICacheRestartRsp(
	rsp *cache.RestartRsp,
) bool {
	p.numCacheACK--
	if p.numCacheACK == 0 {
		p.cacheCtrlInProcess = false

		fmt.Printf("[CP %d]\tComplete L1I Cache Restart\n\t\tStart ROB Restart\n", p.deviceID)
		for i := 0; i < len(p.L1VROBs); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToROBs.AsRemote()).
				WithDst(p.L1VROBs[i].AsRemote()).
				ToRestart().
				Build()
			p.ToROBs.Send(req)
			p.numROBACK++
		}

		for i := 0; i < len(p.L1SROBs); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToROBs.AsRemote()).
				WithDst(p.L1SROBs[i].AsRemote()).
				ToRestart().
				Build()
			p.ToROBs.Send(req)
			p.numROBACK++
		}

		for i := 0; i < len(p.L1IROBs); i++ {
			req := mem.ControlMsgBuilder{}.
				WithSrc(p.ToROBs.AsRemote()).
				WithDst(p.L1IROBs[i].AsRemote()).
				ToRestart().
				Build()
			p.ToROBs.Send(req)
			p.numROBACK++
		}
	}

	p.ToCaches.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processROBRestartRsp(
	rsp *mem.ControlMsg,
) bool {
	p.numROBACK--

	if p.numROBACK == 0 {
		fmt.Printf("[CP %d]\tComplete ROB Restart\n\t\tStart CU Restart\n", p.deviceID)

		for i := 0; i < len(p.CUs); i++ {
			req := protocol.CUPipelineRestartReqBuilder{}.
				WithSrc(p.ToCUs.AsRemote()).
				WithDst(p.CUs[i]).
				Build()
			p.ToCUs.Send(req)

			p.numCUAck++
		}
	}

	p.ToROBs.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processCUPipelineRestartRsp(
	rsp *protocol.CUPipelineRestartRsp,
) bool {
	p.numCUAck--

	if p.numCUAck == 0 {
		fmt.Printf("[CP %d]\tComplete CU Pipeline Restart\n\t\tGPU Restart Complete\n", p.deviceID)

		rsp := protocol.NewGPURestartRsp(p.ToDriver, p.Driver, uint32(p.deviceID))
		p.ToDriver.Send(rsp)

		for _, d := range p.Dispatchers {
			d.Resume()
		}
	}

	p.ToCUs.RetrieveIncoming()

	tracing.EndTask(p.currShootdownRequest.Meta().ID, p)

	return true
}

func (p *CommandProcessor) processPageMigrationReq(
	cmd *protocol.PageMigrationReqToCP,
) bool {
	req := pagemigrationcontroller.PageMigrationReqToPMCBuilder{}.
		WithSrc(p.ToPMC.AsRemote()).
		WithDst(p.PMC.AsRemote()).
		WithVAddr(cmd.VirtualAddress).
		WithPID(cmd.PID).
		WithPageSize(cmd.PageSize).
		WithPMCPortOfRemoteGPU(cmd.DestinationPMCPort.AsRemote()).
		WithReadFrom(cmd.ToReadFromPhysicalAddress).
		WithWriteTo(cmd.ToWriteToPhysicalAddress).
		Build()

	err := p.ToPMC.Send(req)
	if err != nil {
		panic(err)
	}

	p.ToDriver.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processPageMigrationRsp(
	rsp *pagemigrationcontroller.PageMigrationRspFromPMC,
) bool {
	req := protocol.NewPageMigrationRspToDriver(p.ToDriver, p.Driver)

	err := p.ToDriver.Send(req)
	if err != nil {
		panic(err)
	}

	p.ToPMC.RetrieveIncoming()

	return true
}

// page migration request를 입력받으면 PMC로 전달
func (p *CommandProcessor) processPTEInvalidationReq(
	cmd *vm.PTEInvalidationReq,
) bool {
	req := vm.PTEInvalidationReqBuilder{}.
		WithSrc(p.ToGMMU.AsRemote()).
		WithDst(p.GMMU.AsRemote()).
		WithVAddr(cmd.VAddr).
		WithPID(cmd.PID).
		WithPageSize(cmd.PageSize).
		Build()

	fmt.Printf("[CP %d]\tReceived PTE Invalidation Request VA %x, forward to GMMU\n", p.deviceID, cmd.VAddr)
	err := p.ToGMMU.Send(req)
	if err != nil {
		panic("[CP]\tfailed to send PTEInvalidationReq to GMMU")
	}

	p.ToDriver.RetrieveIncoming()

	return true
}

func (p *CommandProcessor) processPTEInvalidationRsp(
	rsp *vm.PTEInvalidationRsp,
) bool {
	req := vm.PTEInvalidationRspBuilder{}.
		WithSrc(p.ToDriver.AsRemote()).
		WithDst(p.Driver.AsRemote()).
		Build()

	// fmt.Printf("\t\tSending PTE Invalidation Rsp To Driver\n")
	err := p.ToDriver.Send(req)
	if err != nil {
		panic(err)
	}

	p.ToGMMU.RetrieveIncoming()
	return true
}

func (p *CommandProcessor) processCacheFlushReq(
	req *protocol.FlushReq,
) bool {
	fmt.Printf("[CP %d]\tReceive Cache Flush Request from Driver\n", p.deviceID)

	if p.numCacheACK > 0 {
		return false
	}

	if p.cacheCtrlInProcess {
		return false
	}

	fmt.Printf("\t\tSend Flush Requests to Caches\n")
	for _, port := range p.L1ICaches {
		p.flushCache(port)
	}
	fmt.Printf("\t\tSend Flush Requests to L1I Caches: numACK: %d\n", p.numCacheACK)

	for _, port := range p.L1SCaches {
		p.flushCache(port)
	}
	fmt.Printf("\t\tSend Flush Requests to L1S Caches: numACK: %d\n", p.numCacheACK)

	for _, port := range p.L1VCaches {
		p.flushCache(port)
	}
	fmt.Printf("\t\tSend Flush Requests to L1V Caches: numACK: %d\n", p.numCacheACK)

	for _, port := range p.L2Caches {
		p.flushCache(port)
	}
	fmt.Printf("\t\tSend Flush Requests to L2 Caches: numACK: %d\n", p.numCacheACK)

	p.currFlushRequest = req
	if p.numCacheACK == 0 {
		rsp := sim.GeneralRspBuilder{}.
			WithSrc(p.ToDriver.AsRemote()).
			WithDst(p.Driver.AsRemote()).
			WithOriginalReq(req).
			Build()
		p.ToDriver.Send(rsp)
	}

	p.ToDriver.RetrieveIncoming()

	tracing.TraceReqReceive(req, p)

	return true
}

func (p *CommandProcessor) flushCache(port sim.Port) {
	flushReq := cache.FlushReqBuilder{}.
		WithSrc(p.ToCaches.AsRemote()).
		WithDst(port.AsRemote()).
		DiscardInflight().
		InvalidateAllCacheLines().
		Build()

	err := p.ToCaches.Send(flushReq)
	if err != nil {
		panic(err)
	}

	p.numCacheACK++
}

func (p *CommandProcessor) processCohDirFlushReq(
	req *protocol.FlushReq,
) bool {
	fmt.Printf("[CP %d]\tReceive Coh Dir Flush Request from Drive\n", p.deviceID)

	if p.numCohDirACK > 0 {
		return false
	}

	if p.cacheCtrlInProcess {
		return false
	}

	fmt.Printf("\t\t\tSend Flush Requests to Coh Dir\n")
	p.processCohDirFlush()

	p.currFlushRequest = req
	if p.numCohDirACK == 0 {
		rsp := sim.GeneralRspBuilder{}.
			WithSrc(p.ToDriver.AsRemote()).
			WithDst(p.Driver.AsRemote()).
			WithOriginalReq(req).
			Build()
		p.ToDriver.Send(rsp)
	}

	p.ToDriver.RetrieveIncoming()

	tracing.TraceReqReceive(req, p)

	return true
}

func (p *CommandProcessor) cloneMemCopyH2DReq(
	req *protocol.MemCopyH2DReq,
) *protocol.MemCopyH2DReq {
	cloned := *req
	cloned.ID = sim.GetIDGenerator().Generate()
	p.bottomMemCopyH2DReqIDToTopReqMap[cloned.ID] = req
	return &cloned
}

func (p *CommandProcessor) cloneMemCopyD2HReq(
	req *protocol.MemCopyD2HReq,
) *protocol.MemCopyD2HReq {
	cloned := *req
	cloned.ID = sim.GetIDGenerator().Generate()
	p.bottomMemCopyD2HReqIDToTopReqMap[cloned.ID] = req
	return &cloned
}

func (p *CommandProcessor) processMemCopyReq(
	req sim.Msg,
) bool {
	if p.numCacheACK > 0 {
		return false
	}

	var cloned sim.Msg
	switch req := req.(type) {
	case *protocol.MemCopyH2DReq:
		fmt.Printf("[CP %d]\tReceived MemCopyH2DReq\n", p.deviceID)
		cloned = p.cloneMemCopyH2DReq(req)
	case *protocol.MemCopyD2HReq:
		fmt.Printf("[CP %d]\tReceived MemCopyD2HReq\n", p.deviceID)
		cloned = p.cloneMemCopyD2HReq(req)
	default:
		panic("unknown type")
	}

	cloned.Meta().Dst = p.DMAEngine.AsRemote()
	cloned.Meta().Src = p.ToDMA.AsRemote()

	p.ToDMA.Send(cloned)
	p.ToDriver.RetrieveIncoming()

	tracing.TraceReqReceive(req, p)
	tracing.TraceReqInitiate(cloned, p, tracing.MsgIDAtReceiver(req, p))
	fmt.Printf("\t\tSend request to DMA\n")

	return true
}

func (p *CommandProcessor) findAndRemoveOriginalMemCopyRequest(
	rsp sim.Rsp,
) sim.Msg {
	rspTo := rsp.GetRspTo()

	originalH2DReq, ok := p.bottomMemCopyH2DReqIDToTopReqMap[rspTo]
	if ok {
		delete(p.bottomMemCopyH2DReqIDToTopReqMap, rspTo)
		return originalH2DReq
	}

	originalD2HReq, ok := p.bottomMemCopyD2HReqIDToTopReqMap[rspTo]
	if ok {
		delete(p.bottomMemCopyD2HReqIDToTopReqMap, rspTo)
		return originalD2HReq
	}

	panic("never")
}

func (p *CommandProcessor) processMemCopyRsp(
	req sim.Rsp,
) bool {
	originalReq := p.findAndRemoveOriginalMemCopyRequest(req)

	rsp := sim.GeneralRspBuilder{}.
		WithDst(originalReq.Meta().Src).
		WithSrc(p.ToDriver.AsRemote()).
		WithOriginalReq(originalReq).
		Build()

	p.ToDriver.Send(rsp)
	p.ToDMA.RetrieveIncoming()

	tracing.TraceReqComplete(originalReq, p)
	tracing.TraceReqFinalize(req, p)

	return true
}
