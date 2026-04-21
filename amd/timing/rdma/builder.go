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
	rdma.RDMARequestOutside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMARequestOutside")
	rdma.RDMADataInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataInside")
	rdma.RDMADataOutside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMADataOutside")
	rdma.RDMAInvInside = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".RDMAInvInside")
	rdma.CtrlPort = sim.NewPort(rdma, b.bufferSize, b.bufferSize, name+".CtrlPort")

	rdma.AddPort("RDMARequestInside", rdma.RDMARequestInside)
	rdma.AddPort("RDMARequestOutside", rdma.RDMARequestOutside)
	rdma.AddPort("RDMADataOutside", rdma.RDMADataOutside)
	rdma.AddPort("RDMADataInside", rdma.RDMADataInside)
	rdma.AddPort("RDMAInvInside", rdma.RDMAInvInside)
	rdma.AddPort("CtrlPort", rdma.CtrlPort)

	tracing.CollectTrace(rdma, b.visTracer)

	return rdma
}
