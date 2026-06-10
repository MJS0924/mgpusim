package rdma

import (
	"fmt"
	"os"
	"strings"

	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/sim"
)

const deadlockDumpPath = "/root/deadlock_dump.txt"

func appendDeadlockDump(s string) {
	f, err := os.OpenFile(deadlockDumpPath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(s)
}

// DumpDeadlockState writes this RDMA engine's outstanding-transaction state to
// the shared deadlock dump file when the engine halts. transactionsFromInside
// holds requests this RDMA forwarded toward a peer and is still awaiting the
// peer's response for — i.e. the in-flight evictions/reads whose acks never
// returned. Correlate these reqIDs against the L2's inflightEviction reqIDs to
// determine whether a stuck eviction is (a) genuinely forwarded-and-awaiting
// here, or (b) already completed here but never decremented at the L2.
func (c *Comp) DumpDeadlockState() {
	if len(c.transactionsFromInside) == 0 &&
		len(c.transactionsFromOutside) == 0 {
		return
	}

	var sb strings.Builder
	// [RETURN-PATH DIAG] rspOutEgress=FULL ⇒ home peer-response network egress
	// (RDMADataRspOutside) is saturated → home cannot send responses back →
	// fromOutside backs up → home stops serving. reqInsideIngress=FULL ⇒ the
	// sender-side path to the L2 (RDMARequestInside) is full → acks cannot
	// return → numRemoteInflightEviction pins. reqInsideHasIn ⇒ peer-serve
	// responses are accumulating unsent in RDMADataReqInside.
	fmt.Fprintf(&sb, "[RDMA dev=%d] fromInside=%d fromOutside=%d lostFromOutside=%d lostFromL2=%d ackRecvAtRspOut=%d | rspOutEgress=%s reqInsideIngress=%s reqInsideHasIn=%v\n",
		c.deviceID, len(c.transactionsFromInside), len(c.transactionsFromOutside),
		c.lostRspFromOutsideCount, c.lostRspFromL2Count, c.ackRecvAtRspOutsideCount,
		canSendState(c.RDMADataRspOutside), canSendState(c.RDMARequestInside),
		hasIncoming(c.RDMADataReqInside))

	for i, t := range c.transactionsFromInside {
		if i >= 16 {
			fmt.Fprintf(&sb, "  ...(%d more fromInside)\n", len(c.transactionsFromInside)-16)
			break
		}
		id, addr := transMsgInfo(t.fromInside)
		fmt.Fprintf(&sb, "  fromInside[%d] reqID=%s addr=%x home=%d\n",
			i, id, addr, addr/(4*1024*1024*1024))
	}
	for i, t := range c.transactionsFromOutside {
		if i >= 8 {
			fmt.Fprintf(&sb, "  ...(%d more fromOutside)\n", len(c.transactionsFromOutside)-8)
			break
		}
		id, addr := transMsgInfo(t.fromOutside)
		fmt.Fprintf(&sb, "  fromOutside[%d] reqID=%s addr=%x\n", i, id, addr)
	}

	appendDeadlockDump(sb.String())
}

// canSendState returns "FULL" when the port's outgoing buffer cannot accept a
// message (the egress is saturated = a likely deadlock back-up point), "ok"
// otherwise, "nil" for a nil (legacy) port.
func canSendState(p sim.Port) string {
	if p == nil {
		return "nil"
	}
	if p.CanSend() {
		return "ok"
	}
	return "FULL"
}

// hasIncoming reports whether the port has an undrained incoming message
// (responses accumulating unsent).
func hasIncoming(p sim.Port) bool {
	if p == nil {
		return false
	}
	return p.PeekIncoming() != nil
}

func transMsgInfo(m sim.Msg) (id string, addr uint64) {
	id = "<nil>"
	if m == nil {
		return
	}
	id = m.Meta().ID
	if am, ok := m.(mem.AccessReq); ok {
		addr = am.GetAddress()
	}
	return
}
