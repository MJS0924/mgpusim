package rdma

import (
	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/tracing"
)

type Builder struct {
	name                   string
	deviceID               uint64
	engine                 sim.Engine
	visTracer              tracing.Tracer
	freq                   sim.Freq
	localModules           mem.AddressToPortMapper
	RemoteRDMAAddressTable mem.AddressToPortMapper
	bufferSize             int

	incomingReqPerCycle int
	incomingRspPerCycle int
	outgoingReqPerCycle int
	outgoingRspPerCycle int

	accessCounter *map[vm.PID]map[uint64]uint8
	dirtyMask     *[]map[vm.PID]map[uint64][]uint8
	readMask      *[]map[vm.PID]map[uint64][]uint8

	log2PageSize      uint64
	log2CacheLineSize uint64
}

// MakeBuilder creates a new builder with default configuration values.
func MakeBuilder() Builder {
	return Builder{
		freq:                1 * sim.GHz,
		bufferSize:          128,
		incomingReqPerCycle: 1,
		incomingRspPerCycle: 1,
		outgoingReqPerCycle: 1,
		outgoingRspPerCycle: 1,
	}
}

func (b Builder) WithDeviceID(id uint64) Builder {
	b.deviceID = id
	return b
}

// WithEngine sets the even-driven simulation engine to use.
func (b Builder) WithEngine(engine sim.Engine) Builder {
	b.engine = engine
	return b
}

// WithVisTracer enables tracing for visualization on the command processor and
// the dispatchers.
func (b Builder) WithVisTracer(tracer tracing.Tracer) Builder {
	b.visTracer = tracer
	return b
}

// WithFreq sets the frequency that the Command Processor works at.
func (b Builder) WithFreq(freq sim.Freq) Builder {
	b.freq = freq
	return b
}

// WithBufferSize sets the number of transactions that the buffer can handle.
func (b Builder) WithBufferSize(n int) Builder {
	b.bufferSize = n
	return b
}

// WithLocalModules sets the local modules.
func (b Builder) WithLocalModules(m mem.AddressToPortMapper) Builder {
	b.localModules = m
	return b
}

// WithRemoteModules sets the remote modules.
func (b Builder) WithRemoteModules(m mem.AddressToPortMapper) Builder {
	b.RemoteRDMAAddressTable = m
	return b
}

func (b Builder) WithIncomingReqPerCycle(n int) Builder {
	b.incomingReqPerCycle = n
	return b
}

func (b Builder) WithIncomingRspPerCycle(n int) Builder {
	b.incomingRspPerCycle = n
	return b
}

func (b Builder) WithOutgoingReqPerCycle(n int) Builder {
	b.outgoingReqPerCycle = n
	return b
}

func (b Builder) WithOutgoingRspPerCycle(n int) Builder {
	b.outgoingRspPerCycle = n
	return b
}

func (b Builder) WithAccessCounter(ac *map[vm.PID]map[uint64]uint8) Builder {
	b.accessCounter = ac
	return b
}

func (b Builder) WithDirtyMask(mask *[]map[vm.PID]map[uint64][]uint8) Builder {
	b.dirtyMask = mask
	return b
}

func (b Builder) WithReadMask(mask *[]map[vm.PID]map[uint64][]uint8) Builder {
	b.readMask = mask
	return b
}

func (b Builder) WithLog2PageSize(log2PageSize uint64) Builder {
	b.log2PageSize = log2PageSize
	return b
}

func (b Builder) WithLog2CacheLineSize(log2CacheLineSize uint64) Builder {
	b.log2CacheLineSize = log2CacheLineSize
	return b
}

// Build creates a RDMA with the given parameters.
func (b Builder) Build(name string) *Comp {
	rdma := &Comp{}

	rdma.deviceID = b.deviceID

	rdma.TickingComponent = sim.NewTickingComponent(name, b.engine, b.freq, rdma)

	rdma.localModules = b.localModules
	rdma.RemoteRDMAAddressTable = b.RemoteRDMAAddressTable
	rdma.incomingReqPerCycle = b.incomingReqPerCycle
	rdma.incomingRspPerCycle = b.incomingRspPerCycle
	rdma.outgoingReqPerCycle = b.outgoingReqPerCycle
	rdma.outgoingRspPerCycle = b.outgoingRspPerCycle

	rdma.AccessCounter = b.accessCounter
	rdma.dirtyMask = b.dirtyMask
	rdma.readMask = b.readMask

	rdma.log2PageSize = b.log2PageSize
	rdma.log2CacheLineSize = b.log2CacheLineSize
	// fmt.Printf("RDMA Log2PageSize: %d, Log2CacheLineSize: %d\n", rdma.log2PageSize, rdma.log2CacheLineSize)

	rdma.RDMARequestInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMARequestInside")
	rdma.RDMADataInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataInside")
	rdma.RDMAInvInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMAInvInside")
	// [ITER7] Dedicated port for INV RSP. Separates RSP egress from
	// REQ on the inside-facing inv link so a stalled REQ stream cannot
	// head-of-line block a RSP that would unblock the peer's
	// inflightInvToBottom cap.
	rdma.RDMAInvRspInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMAInvRspInside")

	// [R1] Four typed wire-side ports replace the previous bidirectional
	// RDMARequestOutside / RDMADataOutside pair. Each port is dedicated
	// to ONE direction × ONE message type so a stalled type cannot
	// head-of-line block the others on the cross-GPU path. Same buffer
	// size as the existing Outside ports.
	rdma.RDMADataReqOutside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataReqOutside")
	rdma.RDMADataRspOutside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataRspOutside")
	rdma.RDMAInvReqOutside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMAInvReqOutside")
	rdma.RDMAInvRspOutside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMAInvRspOutside")

	// [R1 INSIDE SPLIT] Typed Inside ports paired with the new Outside ports.
	// REC connects via these — REQ traffic on RDMADataReqInside, RSP traffic
	// on RDMADataRspInside. RDMAInvInside / RDMAInvRspInside (iter7) continue
	// to handle inv-side typed paths from the inside.
	rdma.RDMADataReqInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataReqInside")
	rdma.RDMADataRspInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataRspInside")

	// [R1 LEGACY] The legacy RDMARequestOutside / RDMADataOutside fields
	// are deliberately left nil. Any remaining caller is forced to fail
	// loudly (nil-port panic) rather than silently funnel mixed traffic
	// back into a multiplexed buffer. Migrate callers to the typed ports.

	rdma.CtrlPort = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".CtrlPort")

	rdma.AddPort("RDMARequestInside", rdma.RDMARequestInside)
	rdma.AddPort("RDMADataInside", rdma.RDMADataInside)
	rdma.AddPort("RDMAInvInside", rdma.RDMAInvInside)
	rdma.AddPort("RDMAInvRspInside", rdma.RDMAInvRspInside)

	// [R1] New typed wire-side ports.
	rdma.AddPort("RDMADataReqOutside", rdma.RDMADataReqOutside)
	rdma.AddPort("RDMADataRspOutside", rdma.RDMADataRspOutside)
	rdma.AddPort("RDMAInvReqOutside", rdma.RDMAInvReqOutside)
	rdma.AddPort("RDMAInvRspOutside", rdma.RDMAInvRspOutside)

	// [R1 INSIDE SPLIT] new typed Inside ports.
	rdma.AddPort("RDMADataReqInside", rdma.RDMADataReqInside)
	rdma.AddPort("RDMADataRspInside", rdma.RDMADataRspInside)

	rdma.AddPort("CtrlPort", rdma.CtrlPort)

	tracing.CollectTrace(rdma, b.visTracer)

	// [DEADLOCK DUMP] post-mortem state dump on engine halt (RTM-independent).
	if se, ok := b.engine.(*sim.SerialEngine); ok {
		se.RegisterStopHook(rdma.DumpDeadlockState)
	}

	return rdma
}
