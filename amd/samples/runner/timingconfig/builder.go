// Package timingconfig contains the configuration for the timing simulation.
package timingconfig

import (
	"fmt"

	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/mem/vm/mmu"
	"github.com/sarchlab/akita/v4/noc/networking/networkconnector"
	"github.com/sarchlab/akita/v4/noc/networking/nvlink"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/sim/directconnection"
	"github.com/sarchlab/akita/v4/simulation"
	"github.com/sarchlab/mgpusim/v4/amd/driver"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner/timingconfig/r9nano"
)

// Builder builds a hardware platform for timing simulation.
type Builder struct {
	simulation *simulation.Simulation

	numGPUs                    int
	numCUPerSA                 int
	numSAPerGPU                int
	cpuMemSize                 uint64
	gpuMemSize                 uint64
	log2PageSize               uint64
	log2CacheBlockSize         uint64
	log2CoherenceUnitSize      uint64
	useMagicMemoryCopy         bool
	pageMigrationPolicy        uint64
	coherenceDirectory         uint64
	sdNumBanks                 int
	sdLog2NumSubEntry          uint64
	sdByteSize                 uint64
	sdDisableRSB               bool
	sdDisableCBF               bool
	sdFE                       bool
	sdDisableDemoteLock        bool
	sdPromoteRelaxed           bool
	sdUseRsbHintAlloc          bool
	sdRecordSilentEvict        bool
	sdPromoteAtEvict           bool
	sdPromoteAtEvictBiasVictim bool
	sdPromoteAtEvictMultiBank  bool
	mgdRegionSize              uint64
	recHalfSet                 bool
	invExtraLatency            int
	interGPUNoC                bool
	interGPUNoCSplitRsp        bool

	platform          *sim.Domain
	globalStorage     *mem.Storage
	rdmaAddressMapper *mem.BankedAddressPortMapper
	idealDirectory     bool
	cd8DeadlockFix     bool
	sdAckReserve       bool
	sdPeerServeReserve bool
	cdFifoReplacement  bool

	// gpuDeviceIDs are the NVLink-fabric device IDs returned by
	// nvlink.Connector.PlugInDevice when each GPU is attached. Used to
	// build the all-pairs NVLink mesh after every GPU has been plugged in.
	gpuDeviceIDs []int

	// [INTER-GPU DIRECTCONN] Plain directconnection that carries inter-GPU
	// RDMA traffic (the 4 typed RDMA Outside ports of every GPU) instead of
	// the NVLink mesh. A directconnection forwards port-to-port with no
	// internal network queues, so cross-GPU RDMA requests/responses cannot be
	// held in NVLink switch buffers (where the stencil2d REC eviction storm
	// was getting stuck out of sight of the port-level hang detector).
	// Used on the DEFAULT path (interGPUNoC == false).
	interGPUConn *directconnection.Comp

	// [INTER-GPU NOC] When interGPUNoC is true, the 4 RDMA ports of every GPU
	// ride this dedicated bandwidth-modeled NoC instead of interGPUConn. The
	// topology is one switch per GPU plus a dedicated link between every switch
	// pair (true 1:1 / all-pairs point-to-point among the 4 GPUs). Bandwidth is
	// governed by the flit-serialization model: per-direction BW = flitSize x
	// numChannels x freq. With flitSize = interGPUNoCBW (GB/s), 1 channel, 1
	// GHz this is exactly interGPUNoCBW GB/s per direction per pair link
	// (e.g. 300 => 300 GB/s each way); set via -inter-gpu-noc-bw.
	//
	// NOTE on akita's bandwidth model: in this version a network LINK is always
	// an *ideal* directconnection at the connector default frequency
	// (networkconnector.connectPorts only implements LinkParam.IsIdeal and
	// discards the link Frequency), so WithNVLinkBandwidth / a link frequency
	// CANNOT set bandwidth — only flit size x channels x freq does. That is why
	// we use a dedicated connector with its own flit size (rather than reusing
	// the PCIe/control NVLink mesh, whose 34 B flit is shared with the PCIe
	// tree).
	interGPURDMANoC  *networkconnector.Connector
	rdmaNoCSwitchIDs []int

	// [INTER-GPU NOC — REQ/RSP SPLIT] When interGPUNoCSplitRsp is true (and
	// interGPUNoC is true), the 4 RDMA ports ride TWO independent NoCs instead
	// of one shared endpoint: a REQUEST NoC carrying {RDMADataReq, RDMAInvReq}
	// and a RESPONSE NoC carrying {RDMADataRsp, RDMAInvRsp}. This stops a
	// request flit from head-of-line-blocking a response flit on a shared
	// endpoint FIFO/in-order delivery — the CD_8 cross-GPU invalidation
	// deadlock. interGPURDMANoC is reused as the request NoC; interGPURDMARspNoC
	// is the response NoC. Req ports only talk to req ports and rsp ports only
	// to rsp ports, so the two NoCs are fully self-contained (no message crosses
	// between them).
	interGPURDMARspNoC  *networkconnector.Connector
	rdmaRspNoCSwitchIDs []int

	// interGPUNoCBW is the per-direction bandwidth (GB/s, decimal) of each
	// inter-GPU NoC link. It maps directly to the flit byte size: BW =
	// flitBytes x numChannels x freq, and at 1 GHz / 1 channel flitBytes =
	// BW-in-GB/s, so 300 => 300 GB/s each way per dedicated GPU-pair link.
	// Only used when interGPUNoC is true. Defaults to interGPUNoCDefaultBW.
	interGPUNoCBW int
}

const (
	// interGPUNoCDefaultBW is the default per-direction inter-GPU NoC bandwidth
	// (GB/s, decimal) used when the -inter-gpu-noc-bw flag is not set. The flit
	// model gives BW = flitBytes x numChannels x freq; at 1 GHz, 1 channel,
	// flitBytes = BW-in-GB/s, so 300 => 300 GB/s per direction (decimal) per
	// dedicated pair link. For the REC paper's "300 GB/s bi-directional
	// aggregate" reading instead, pass 150 (= 150 GB/s each way); for binary
	// GiB/s (322 GB/s decimal), pass 322.
	interGPUNoCDefaultBW = 300

	// interGPUNoCBufSize is the per-port flit buffer depth on the NoC switches
	// and endpoints. Sized >= the directory inflight caps (numPeerInflight /
	// inflightInvToBottom = 256) so that, under a REC/SD eviction-or-inv storm,
	// the binding backpressure edge stays at the directory (where the hang
	// detector and diagnoses look) rather than in the smaller, less-visible
	// NoC switch buffers.
	interGPUNoCBufSize = 256

	// interGPUNoCSwitchLatency models the per-hop switch latency (cycles),
	// matching the NVLink switch latency used elsewhere.
	interGPUNoCSwitchLatency = 140
)

// MakeBuilder creates a new Builder with default parameters.
func MakeBuilder() Builder {
	return Builder{
		numGPUs:            1,
		numCUPerSA:         4,
		numSAPerGPU:        16,
		cpuMemSize:         4 * mem.GB,
		gpuMemSize:         4 * mem.GB,
		log2PageSize:       12,
		useMagicMemoryCopy: false,
		sdNumBanks:         5,
		sdLog2NumSubEntry:  2,
		sdByteSize:         512 * mem.KB,
		mgdRegionSize:      1024,
		interGPUNoCBW:      interGPUNoCDefaultBW,
	}
}

// WithSimulation sets the simulation to use.
func (b Builder) WithSimulation(sim *simulation.Simulation) Builder {
	b.simulation = sim
	return b
}

// WithNumGPUs sets the number of GPUs to simulate.
func (b Builder) WithNumGPUs(numGPUs int) Builder {
	b.numGPUs = numGPUs
	return b
}

// WithMagicMemoryCopy sets whether to use the magic memory copy middleware.
func (b Builder) WithMagicMemoryCopy() Builder {
	b.useMagicMemoryCopy = true
	return b
}

func (b Builder) WithLog2PageSize(size uint64) Builder {
	b.log2PageSize = size
	return b
}

func (b Builder) WithLog2CacheBlockSize(size uint64) Builder {
	b.log2CacheBlockSize = size
	return b
}

func (b Builder) WithLog2CoherenceUnitSize(size uint64) Builder {
	b.log2CoherenceUnitSize = size
	return b
}

func (b Builder) WithPageMigrationPolicy(policy uint64) Builder {
	b.pageMigrationPolicy = policy
	return b
}

func (b Builder) WithCoherenceDirectory(dir uint64) Builder {
	b.coherenceDirectory = dir
	return b
}

func (b Builder) WithIdealDirectory(bo bool) Builder {
	b.idealDirectory = bo
	return b
}

// WithCD8DeadlockFix toggles the CD_8 16KB cross-GPU writeback deadlock fix.
func (b Builder) WithCD8DeadlockFix(on bool) Builder {
	b.cd8DeadlockFix = on
	return b
}

// WithSDAckReserve toggles the SD 9-bank cross-GPU eviction-credit deadlock fix
// (L2 ackDisplaceReserve).
func (b Builder) WithSDAckReserve(on bool) Builder {
	b.sdAckReserve = on
	return b
}

// WithSDPeerServeReserve toggles the SD 9-bank cross-GPU capacity-cycle deadlock
// fix (SuperDir peer-serve inflight reserve).
func (b Builder) WithSDPeerServeReserve(on bool) Builder {
	b.sdPeerServeReserve = on
	return b
}

func (b Builder) WithCDFifoReplacement(v bool) Builder {
	b.cdFifoReplacement = v
	return b
}

func (b Builder) WithSDNumBanks(n int) Builder {
	b.sdNumBanks = n
	return b
}

func (b Builder) WithSDLog2NumSubEntry(n uint64) Builder {
	b.sdLog2NumSubEntry = n
	return b
}

func (b Builder) WithSDByteSize(size uint64) Builder {
	b.sdByteSize = size
	return b
}

func (b Builder) WithSDDisableRSB(v bool) Builder {
	b.sdDisableRSB = v
	return b
}

func (b Builder) WithSDDisableCBF(v bool) Builder {
	b.sdDisableCBF = v
	return b
}

func (b Builder) WithSDFE(v bool) Builder {
	b.sdFE = v
	return b
}

func (b Builder) WithSDDisableDemoteLock(v bool) Builder {
	b.sdDisableDemoteLock = v
	return b
}

func (b Builder) WithSDPromoteRelaxed(v bool) Builder {
	b.sdPromoteRelaxed = v
	return b
}

func (b Builder) WithSDUseRsbHintAlloc(v bool) Builder {
	b.sdUseRsbHintAlloc = v
	return b
}

func (b Builder) WithSDRecordSilentEvict(v bool) Builder {
	b.sdRecordSilentEvict = v
	return b
}

func (b Builder) WithSDPromoteAtEvict(v bool) Builder {
	b.sdPromoteAtEvict = v
	return b
}

func (b Builder) WithSDPromoteAtEvictBiasVictim(v bool) Builder {
	b.sdPromoteAtEvictBiasVictim = v
	return b
}

func (b Builder) WithSDPromoteAtEvictMultiBank(v bool) Builder {
	b.sdPromoteAtEvictMultiBank = v
	return b
}

func (b Builder) WithMGDRegionSize(bytes uint64) Builder {
	b.mgdRegionSize = bytes
	return b
}

// WithRECHalfSet halves REC's number of sets to reflect REC's 2x entry-size
// hardware overhead.
func (b Builder) WithRECHalfSet(v bool) Builder {
	b.recHalfSet = v
	return b
}

// WithInvExtraLatency forwards extra L2 invalidation-pipeline cycles down
// to the r9nano GPU builder.
func (b Builder) WithInvExtraLatency(n int) Builder {
	b.invExtraLatency = n
	return b
}

// WithInterGPUNoC selects the inter-GPU RDMA interconnect. When true, the 4
// RDMA ports of every GPU ride a dedicated 300 GB/s bandwidth-modeled NoC
// (one switch per GPU, dedicated all-pairs 1:1 links). When false (default),
// they ride the idealized bandwidth-less directconnection used for the
// deadlock-debugging workflow.
func (b Builder) WithInterGPUNoC(v bool) Builder {
	b.interGPUNoC = v
	return b
}

// WithInterGPUNoCBandwidth sets the per-direction bandwidth (GB/s, decimal) of
// each inter-GPU NoC link. The value maps directly to the flit byte size, so
// 300 => 300 GB/s each way per dedicated GPU-pair link. Only effective when
// the inter-GPU NoC is selected (WithInterGPUNoC(true)). A value <= 0 falls
// back to the default.
func (b Builder) WithInterGPUNoCBandwidth(gbPerSec int) Builder {
	if gbPerSec <= 0 {
		gbPerSec = interGPUNoCDefaultBW
	}
	b.interGPUNoCBW = gbPerSec
	return b
}

// WithInterGPUNoCSplitRsp, when true, routes inter-GPU RDMA requests and
// responses over TWO independent NoCs (request lane {RDMADataReq, RDMAInvReq}
// vs response lane {RDMADataRsp, RDMAInvRsp}) instead of one shared endpoint.
// This removes the request-blocks-response head-of-line deadlock (the CD_8
// cross-GPU invalidation hang). Only effective when the inter-GPU NoC is
// selected (WithInterGPUNoC(true)). Default false keeps the single shared NoC
// so prior results stay comparable.
func (b Builder) WithInterGPUNoCSplitRsp(v bool) Builder {
	b.interGPUNoCSplitRsp = v
	return b
}

// Build builds the hardware platform.
func (b Builder) Build() *sim.Domain {
	b.cpuGPUMemSizeMustEqual()

	b.platform = &sim.Domain{}

	b.globalStorage = mem.NewStorage(
		uint64(b.numGPUs+1)*b.gpuMemSize + b.cpuMemSize)

	mmuComp, pageTable := b.createMMU()
	gpuDriver := b.buildGPUDriver(pageTable)

	gpuBuilder := b.createGPUBuilder(gpuDriver, mmuComp)
	nvlinkConnector, rootComplexID :=
		b.createConnection(gpuDriver, mmuComp)

	// Build the inter-GPU RDMA fabric. Default: the idealized directconnection
	// (deadlock-debug-friendly). Opt-in (-inter-gpu-noc): a dedicated 300 GB/s
	// bandwidth-modeled NoC initialized here and wired per-GPU in createGPU.
	if b.interGPUNoC {
		b.initInterGPURDMANoC()
	} else {
		// [INTER-GPU DIRECTCONN] Build the plain directconnection that will
		// carry inter-GPU RDMA traffic. Each GPU's RDMA Outside ports are
		// plugged into this (and excluded from the NVLink fabric) in createGPU.
		b.interGPUConn = directconnection.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(1 * sim.GHz).
			Build("InterGPUDirect")
		b.simulation.RegisterComponent(b.interGPUConn)
	}

	mmuComp.MigrationServiceProvider = gpuDriver.GetPortByName("MMU").AsRemote()

	b.createRDMAAddrTable()
	pmcAddressTable := b.createPMCPageTable()

	b.createGPUs(
		rootComplexID, nvlinkConnector,
		gpuBuilder, gpuDriver,
		pmcAddressTable)

	// Build a full NVLink mesh among all GPUs. REC paper baseline assumes
	// uniform 300 GB/s bi-directional inter-GPU bandwidth, modelled here as
	// one direct NVLink between every GPU pair.
	for i := 0; i < len(b.gpuDeviceIDs); i++ {
		for j := i + 1; j < len(b.gpuDeviceIDs); j++ {
			nvlinkConnector.ConnectDevicesWithNVLink(
				b.gpuDeviceIDs[i], b.gpuDeviceIDs[j], 1)
		}
	}

	nvlinkConnector.EstablishRoute()

	// [INTER-GPU NOC] After every GPU has attached its RDMA ports to its own
	// switch (in createGPU), build a dedicated link between every switch pair
	// (all-pairs / true 1:1 point-to-point) and finalize the routing tables.
	if b.interGPUNoC {
		b.finalizeRDMANoC(b.interGPURDMANoC, b.rdmaNoCSwitchIDs)
		if b.interGPUNoCSplitRsp {
			b.finalizeRDMANoC(b.interGPURDMARspNoC, b.rdmaRspNoCSwitchIDs)
		}
	}

	return b.platform
}

// finalizeRDMANoC builds the dedicated all-pairs (1:1 point-to-point) links
// among the given per-GPU switches and finalizes the routing tables for one
// RDMA NoC.
func (b *Builder) finalizeRDMANoC(
	noc *networkconnector.Connector, swIDs []int,
) {
	for i := 0; i < len(swIDs); i++ {
		for j := i + 1; j < len(swIDs); j++ {
			noc.ConnectSwitches(
				swIDs[i], swIDs[j], b.rdmaNoCSwitchLinkParam())
		}
	}
	noc.EstablishRoute()
}

// initInterGPURDMANoC builds the empty dedicated RDMA NoC connector. Switches
// (one per GPU) and the device links are added in createGPU; the all-pairs
// switch links + routing are finalized at the end of Build().
func (b *Builder) initInterGPURDMANoC() {
	if b.interGPUNoCSplitRsp {
		// Two independent fabrics so a request flit can never head-of-line-block
		// a response flit. interGPURDMANoC carries requests; interGPURDMARspNoC
		// carries responses.
		b.interGPURDMANoC = b.makeRDMANoCConnector("InterGPURDMAReqNoC")
		b.interGPURDMARspNoC = b.makeRDMANoCConnector("InterGPURDMARspNoC")
		return
	}
	b.interGPURDMANoC = b.makeRDMANoCConnector("InterGPURDMANoC")
}

// makeRDMANoCConnector builds one empty bandwidth-modeled RDMA NoC connector.
// Switches (one per GPU) and device links are added per-GPU in createGPU; the
// all-pairs switch links + routing are finalized at the end of Build().
func (b *Builder) makeRDMANoCConnector(name string) *networkconnector.Connector {
	// flit bytes = per-direction GB/s (decimal) at 1 GHz / 1 channel, so the
	// bandwidth flag value maps straight through (300 => 300 GB/s each way).
	flitBytes := b.interGPUNoCBW
	if flitBytes <= 0 {
		flitBytes = interGPUNoCDefaultBW
	}
	conn := networkconnector.MakeConnector().
		WithEngine(b.simulation.GetEngine()).
		WithMonitor(b.simulation.GetMonitor()).
		WithDefaultFreq(1 * sim.GHz).
		WithFlitSize(flitBytes)
	c := &conn
	c.NewNetwork(name)
	return c
}

// rdmaNoCDeviceLinkParam describes the GPU<->its-own-switch link.
func (b *Builder) rdmaNoCDeviceLinkParam() networkconnector.DeviceToSwitchLinkParameter {
	return networkconnector.DeviceToSwitchLinkParameter{
		DeviceEndParam: networkconnector.LinkEndDeviceParameter{
			IncomingBufSize:  interGPUNoCBufSize,
			OutgoingBufSize:  interGPUNoCBufSize,
			NumInputChannel:  1,
			NumOutputChannel: 1,
		},
		SwitchEndParam: networkconnector.LinkEndSwitchParameter{
			IncomingBufSize:  interGPUNoCBufSize,
			OutgoingBufSize:  interGPUNoCBufSize,
			NumInputChannel:  1,
			NumOutputChannel: 1,
			Latency:          1,
		},
		LinkParam: networkconnector.LinkParameter{
			IsIdeal:       true,
			Frequency:     1 * sim.GHz,
			NumStage:      20,
			CyclePerStage: 1,
			PipelineWidth: 1,
		},
	}
}

// rdmaNoCSwitchLinkParam describes a dedicated switch<->switch link between a
// GPU pair (the per-pair 1:1 point-to-point edge).
func (b *Builder) rdmaNoCSwitchLinkParam() networkconnector.SwitchToSwitchLinkParameter {
	end := networkconnector.LinkEndSwitchParameter{
		IncomingBufSize:  interGPUNoCBufSize,
		OutgoingBufSize:  interGPUNoCBufSize,
		NumInputChannel:  1,
		NumOutputChannel: 1,
		Latency:          interGPUNoCSwitchLatency,
	}
	return networkconnector.SwitchToSwitchLinkParameter{
		LeftEndParam:  end,
		RightEndParam: end,
		LinkParam: networkconnector.LinkParameter{
			IsIdeal:       true,
			Frequency:     1 * sim.GHz,
			NumStage:      20,
			CyclePerStage: 1,
			PipelineWidth: 1,
		},
	}
}

func (b *Builder) cpuGPUMemSizeMustEqual() {
	if b.cpuMemSize != b.gpuMemSize {
		panic("currently only support cpuMemSize == gpuMemSize")
	}
}

func (b *Builder) createMMU() (*mmu.Comp, vm.LevelPageTable) {
	pageTable := vm.NewLevelPageTable(b.log2PageSize, 6, "MMU.PT")
	mmuBuilder := mmu.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(1 * sim.GHz).
		WithPageWalkingLatency(100).
		WithLog2PageSize(b.log2PageSize).
		WithPageTable(pageTable).
		WithPageMigrationPolicy(b.pageMigrationPolicy)

	mmuComponent := mmuBuilder.Build("MMU")

	b.simulation.RegisterComponent(mmuComponent)

	return mmuComponent, pageTable
}

func (b *Builder) buildGPUDriver(
	pageTable vm.LevelPageTable,
) *driver.Driver {
	gpuDriverBuilder := driver.MakeBuilder()

	if b.useMagicMemoryCopy {
		gpuDriverBuilder = gpuDriverBuilder.WithMagicMemoryCopyMiddleware()
	}

	gpuDriver := gpuDriverBuilder.
		WithEngine(b.simulation.GetEngine()).
		WithPageTable(pageTable).
		WithLog2PageSize(b.log2PageSize).
		WithLog2CacheLineSize(b.log2CacheBlockSize).
		WithGlobalStorage(b.globalStorage).
		WithD2HCycles(8500).
		WithH2DCycles(14500).
		WithVisTracer(b.simulation.GetVisTracer()).
		WithPageMigrationPolicy(b.pageMigrationPolicy).
		Build("Driver")

	b.simulation.RegisterComponent(gpuDriver)

	return gpuDriver
}

func (b *Builder) createGPUBuilder(
	gpuDriver *driver.Driver,
	mmuComponent *mmu.Comp,
) r9nano.Builder {
	gpuBuilder := r9nano.MakeBuilder().
		WithFreq(1 * sim.GHz).
		WithSimulation(b.simulation).
		WithMMU(mmuComponent).
		WithNumCUPerShaderArray(b.numCUPerSA).
		WithNumShaderArray(b.numSAPerGPU).
		WithNumMemoryBank(16).
		// Decouple bank interleaving from coherence-unit-size: keep
		// 128B (= 2 cache lines) regardless of CD so the L2/DRAM bank
		// striping doesn't move with coherence granularity. The
		// commented form below tied them together (2^(6+CD+1) bytes).
		WithLog2MemoryBankInterleavingSize(7).
		// WithLog2MemoryBankInterleavingSize(b.log2CacheBlockSize + b.log2CoherenceUnitSize + 1).
		WithLog2PageSize(b.log2PageSize).
		WithLog2CacheLineSize(b.log2CacheBlockSize).
		WithLog2CoherenceUnitSize(b.log2CoherenceUnitSize).
		WithGlobalStorage(b.globalStorage).
		WithDriver(gpuDriver).
		WithPageMigrationPolicy(b.pageMigrationPolicy).
		WithCoherenceDirectory(b.coherenceDirectory).
		WithIdealDirectory(b.idealDirectory).
		WithCD8DeadlockFix(b.cd8DeadlockFix).
		WithSDAckReserve(b.sdAckReserve).
		WithSDPeerServeReserve(b.sdPeerServeReserve).
		WithCDFifoReplacement(b.cdFifoReplacement).
		WithCohDirSize(b.sdByteSize).
		WithSDNumBanks(b.sdNumBanks).
		WithSDLog2NumSubEntry(b.sdLog2NumSubEntry).
		WithSDDisableRSB(b.sdDisableRSB).
		WithSDDisableCBF(b.sdDisableCBF).
		WithSDFE(b.sdFE).
		WithSDDisableDemoteLock(b.sdDisableDemoteLock).
		WithSDPromoteRelaxed(b.sdPromoteRelaxed).
		WithSDUseRsbHintAlloc(b.sdUseRsbHintAlloc).
		WithSDRecordSilentEvict(b.sdRecordSilentEvict).
		WithSDPromoteAtEvict(b.sdPromoteAtEvict).
		WithSDPromoteAtEvictBiasVictim(b.sdPromoteAtEvictBiasVictim).
		WithSDPromoteAtEvictMultiBank(b.sdPromoteAtEvictMultiBank).
		WithMGDRegionSize(b.mgdRegionSize).
		WithRECHalfSet(b.recHalfSet).
		WithInvExtraLatency(b.invExtraLatency)
	fmt.Printf("[r9nano Builder]\tCreating GPU Builder with log2CacheLineSize %d, log2PageSize %d coherenceDirectory %d.\n",
		b.log2CacheBlockSize, b.log2PageSize, b.coherenceDirectory)

	b.createRDMAAddressMapper()

	// gpuBuilder = b.setMemTracer(gpuBuilder)
	// gpuBuilder = b.setISADebugger(gpuBuilder)

	return gpuBuilder
}

func (b *Builder) createGPUs(
	rootComplexID int,
	nvlinkConnector *nvlink.Connector,
	gpuBuilder r9nano.Builder,
	gpuDriver *driver.Driver,
	pmcAddressTable *mem.BankedAddressPortMapper,
) {
	lastSwitchID := rootComplexID
	for i := 1; i < b.numGPUs+1; i++ {
		if i%2 == 1 {
			// nvlink.Connector exposes AddPCIeSwitch() without a parent
			// argument; wire it back to the root complex manually so the
			// CPU↔GPU PCIe tree matches the previous topology.
			lastSwitchID = nvlinkConnector.AddPCIeSwitch()
			nvlinkConnector.ConnectSwitchesWithPCIeLink(
				rootComplexID, lastSwitchID)
		}

		fmt.Printf("\nCreate GPU %d\n", i)
		b.createGPU(i, gpuBuilder, gpuDriver, pmcAddressTable,
			nvlinkConnector, lastSwitchID)

	}
}

func (b *Builder) createPMCPageTable() *mem.BankedAddressPortMapper {
	pmcAddressTable := new(mem.BankedAddressPortMapper)
	pmcAddressTable.BankSize = 4 * mem.GB
	pmcAddressTable.LowModules = append(pmcAddressTable.LowModules, "")
	return pmcAddressTable
}

func (b *Builder) createRDMAAddrTable() *mem.BankedAddressPortMapper {
	rdmaAddressTable := new(mem.BankedAddressPortMapper)
	rdmaAddressTable.BankSize = 4 * mem.GB
	rdmaAddressTable.LowModules = append(rdmaAddressTable.LowModules, "")
	return rdmaAddressTable
}

func (b *Builder) createConnection(
	gpuDriver *driver.Driver,
	mmuComponent *mmu.Comp,
) (*nvlink.Connector, int) {
	// PCIe tree carries CPU↔GPU control traffic; NVLink mesh carries
	// inter-GPU RDMA/coherence. REC paper Table 2: 300 GB/s bi-directional
	// inter-GPU bandwidth.
	nvlinkConnector := nvlink.NewConnector().
		WithEngine(b.simulation.GetEngine()).
		WithPCIeVersion(4, 16).
		WithPCIeSwitchLatency(140).
		// 150 GB/s per direction × 2 = 300 GB/s aggregate (paper's
		// "bi-directional" reading). Adjust to 300<<30 if the paper
		// number is interpreted as per-direction.
		WithNVLinkBandwidth(150 * (1 << 30)).
		WithNVLinkSwitchLatency(140)

	nvlinkConnector.CreateNetwork("InterGPU")
	rootComplexID := nvlinkConnector.AddRootComplex(
		[]sim.Port{
			gpuDriver.GetPortByName("GPU"),
			gpuDriver.GetPortByName("MMU"),
			mmuComponent.GetPortByName("Migration"),
			mmuComponent.GetPortByName("Top"),
		})

	return nvlinkConnector, rootComplexID
}

func (b *Builder) createRDMAAddressMapper() {
	b.rdmaAddressMapper = new(mem.BankedAddressPortMapper)
	b.rdmaAddressMapper.BankSize = b.gpuMemSize
	b.rdmaAddressMapper.LowModules = append(b.rdmaAddressMapper.LowModules,
		sim.RemotePort("CPU"))
}

func (b *Builder) createGPU(
	index int,
	gpuBuilder r9nano.Builder,
	gpuDriver *driver.Driver,
	pmcAddressTable *mem.BankedAddressPortMapper,
	nvlinkConnector *nvlink.Connector,
	pcieSwitchID int,
) *sim.Domain {
	name := fmt.Sprintf("GPU[%d]", index)
	memAddrOffset := uint64(index) * 4 * mem.GB
	gpu := gpuBuilder.
		WithGPUID(uint64(index)).
		WithMemAddrOffset(memAddrOffset).
		WithRDMAAddressMapper(b.rdmaAddressMapper).
		Build(name)

	gpuDriver.RegisterGPU(
		gpu.GetPortByName("CommandProcessor"),
		gpu.GetPortByName("PageMigrationController"),
		driver.DeviceProperties{
			CUCount:  b.numCUPerSA * b.numSAPerGPU,
			DRAMSize: 4 * mem.GB,
		},
	)
	// gpu.CommandProcessor.Driver = gpuDriver.GetPortByName("GPU")

	b.configRDMAEngine(gpu)
	// b.configPMC(gpu, gpuDriver, pmcAddressTable)

	// Route the 4 typed RDMA Outside ports off the NVLink/PCIe fabric onto the
	// inter-GPU RDMA interconnect. In BOTH modes the 4 RDMA ports are excluded
	// from nvlinkConnector.PlugInDevice (so each port has exactly one
	// connection); they go either to the directconnection (default) or to the
	// dedicated NoC (-inter-gpu-noc).
	rdmaOutsidePortNames := []string{
		"RDMADataReq", "RDMADataRsp", "RDMAInvReq", "RDMAInvRsp",
	}
	rdmaOutsidePorts := map[sim.Port]bool{}
	rdmaPortList := make([]sim.Port, 0, len(rdmaOutsidePortNames))
	for _, n := range rdmaOutsidePortNames {
		p := gpu.GetPortByName(n)
		if p == nil {
			continue
		}
		rdmaOutsidePorts[p] = true
		rdmaPortList = append(rdmaPortList, p)
	}

	if b.interGPUNoC {
		if b.interGPUNoCSplitRsp {
			// [INTER-GPU NOC — REQ/RSP SPLIT] Requests and responses ride
			// separate NoCs so a request flit can never head-of-line-block a
			// response. Req lane: {RDMADataReq, RDMAInvReq}; rsp lane:
			// {RDMADataRsp, RDMAInvRsp}. Each lane gets its own switch on its
			// own NoC; all-pairs links + routing finalized at end of Build().
			reqPorts := collectPortsByName(gpu,
				[]string{"RDMADataReq", "RDMAInvReq"})
			rspPorts := collectPortsByName(gpu,
				[]string{"RDMADataRsp", "RDMAInvRsp"})

			reqSw := b.interGPURDMANoC.AddSwitch()
			b.interGPURDMANoC.ConnectDevice(
				reqSw, reqPorts, b.rdmaNoCDeviceLinkParam())
			b.rdmaNoCSwitchIDs = append(b.rdmaNoCSwitchIDs, reqSw)

			rspSw := b.interGPURDMARspNoC.AddSwitch()
			b.interGPURDMARspNoC.ConnectDevice(
				rspSw, rspPorts, b.rdmaNoCDeviceLinkParam())
			b.rdmaRspNoCSwitchIDs = append(b.rdmaRspNoCSwitchIDs, rspSw)
		} else {
			// [INTER-GPU NOC] This GPU gets its own switch; its 4 RDMA ports
			// attach to it via one endpoint. Dedicated all-pairs switch links +
			// routing are finalized at the end of Build().
			swID := b.interGPURDMANoC.AddSwitch()
			b.interGPURDMANoC.ConnectDevice(
				swID, rdmaPortList, b.rdmaNoCDeviceLinkParam())
			b.rdmaNoCSwitchIDs = append(b.rdmaNoCSwitchIDs, swID)
		}
	} else {
		// [INTER-GPU DIRECTCONN] Plug the 4 RDMA ports into the bandwidth-less
		// directconnection.
		for _, p := range rdmaPortList {
			b.interGPUConn.PlugIn(p)
		}
	}

	pciePorts := make([]sim.Port, 0, len(gpu.Ports()))
	for _, p := range gpu.Ports() {
		if !rdmaOutsidePorts[p] {
			pciePorts = append(pciePorts, p)
		}
	}
	deviceID := nvlinkConnector.PlugInDevice(pcieSwitchID, pciePorts)
	b.gpuDeviceIDs = append(b.gpuDeviceIDs, deviceID)

	// b.gpus = append(b.gpus, gpu)

	return gpu
}

// collectPortsByName resolves the named ports on a GPU domain, skipping any
// that are absent (nil). Used to partition the RDMA ports into request and
// response lanes for the split-NoC wiring.
func collectPortsByName(gpu *sim.Domain, names []string) []sim.Port {
	out := make([]sim.Port, 0, len(names))
	for _, n := range names {
		if p := gpu.GetPortByName(n); p != nil {
			out = append(out, p)
		}
	}
	return out
}

func (b *Builder) configRDMAEngine(
	gpu *sim.Domain,
) {
	// [R1] Resolve peer GPU addresses to the typed RDMADataReq port
	// (peer's AccessReq ingress). Previously this used the multiplexed
	// "RDMAData" alias whose outgoingBuf was iter18-confirmed to mix
	// three outbound classes; with the split, AccessReq egress has its
	// own dedicated port and we route there directly.
	b.rdmaAddressMapper.LowModules = append(
		b.rdmaAddressMapper.LowModules,
		gpu.GetPortByName("RDMADataReq").AsRemote())
}

// func (b *Builder) configPMC(
// 	gpu *GPU,
// 	gpuDriver *driver.Driver,
// 	addrTable *mem.BankedAddressPortMapper,
// ) {
// 	gpu.PMC.RemotePMCAddressTable = addrTable
// 	addrTable.LowModules = append(
// 		addrTable.LowModules,
// 		gpu.PMC.GetPortByName("Remote").AsRemote())
// 	gpuDriver.RemotePMCPorts = append(
// 		gpuDriver.RemotePMCPorts, gpu.PMC.GetPortByName("Remote"))
// }
