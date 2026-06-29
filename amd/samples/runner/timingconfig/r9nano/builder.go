// Package r9nano contains the configuration of GPUs similar to AMD Radeon R9
// Nano.
package r9nano

import (
	"fmt"

	"github.com/sarchlab/akita/v4/mem/cache/MGD"
	"github.com/sarchlab/akita/v4/mem/cache/REC"
	"github.com/sarchlab/akita/v4/mem/cache/largeblkcache"
	"github.com/sarchlab/akita/v4/mem/cache/optdirectory"
	"github.com/sarchlab/akita/v4/mem/cache/superdirectory"
	"github.com/sarchlab/akita/v4/mem/cache/writebackcoh"

	"github.com/sarchlab/akita/v4/mem/dram"
	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/mem/vm/gmmu"
	"github.com/sarchlab/akita/v4/mem/vm/mmu"
	"github.com/sarchlab/akita/v4/mem/vm/tlb"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/sim/directconnection"
	"github.com/sarchlab/akita/v4/simulation"
	"github.com/sarchlab/mgpusim/v4/amd/driver"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner/timingconfig/shaderarray"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cp"
	"github.com/sarchlab/mgpusim/v4/amd/timing/pagemigrationcontroller"
	"github.com/sarchlab/mgpusim/v4/amd/timing/rdma"
)

// Builder builds a hardware platform for timing simulation.
type Builder struct {
	simulation *simulation.Simulation

	gpuID                          uint64
	name                           string
	freq                           sim.Freq
	numCUPerShaderArray            int
	numShaderArray                 int
	l2CacheSize                    uint64
	numMemoryBank                  int
	log2CacheLineSize              uint64
	log2PageSize                   uint64
	log2CoherenceUnitSize          uint64
	log2MemoryBankInterleavingSize uint64
	cohDirSize                     uint64 // 실제 크기는 아니고 커버하는 범위가 될 것
	sdNumBanks                     int
	sdLog2NumSubEntry              uint64
	sdDisableRSB                   bool
	sdDisableCBF                   bool
	sdFE                           bool
	sdDisableDemoteLock            bool
	sdPromoteRelaxed               bool
	sdUseRsbHintAlloc              bool
	sdRecordSilentEvict            bool
	sdPromoteAtEvict               bool
	sdPromoteAtEvictBiasVictim     bool
	sdPromoteAtEvictMultiBank      bool
	mgdRegionSize                  uint64
	recHalfSet                     bool
	invExtraLatency                int
	memAddrOffset                  uint64
	dramSize                       uint64
	globalStorage                  *mem.Storage
	mmu                            *mmu.Comp
	rdmaAddressMapper              mem.AddressToPortMapper
	rdmaLowModuleFinder            *mem.InterleavedAddressPortMapper
	rdmaInvLowModuleFinder         *mem.InterleavedAddressPortMapper
	rdmaBottomAddressMapper        *mem.InterleavedAddressPortMapper
	driver                         *driver.Driver

	gpu        *sim.Domain
	cp         *cp.CommandProcessor
	rdmaEngine *rdma.Comp
	pmc        *pagemigrationcontroller.PageMigrationController
	dmaEngine  *cp.DMAEngine
	sas        []*sim.Domain
	// cohDir     *coherence.Comp
	cohDir   *optdirectory.Comp   // b.coherenceDirectory == 0 || 1 || 4
	superDir *superdirectory.Comp // b.coherenceDirectory == 2
	recDir   *REC.Comp            // b.coherenceDirectory == 3
	mgdDir   *MGD.Comp            // b.coherenceDirectory == 5
	// l2Caches []*writeback.Comp
	l2Caches       []*writebackcoh.Comp  // b.coherenceDirectory == 0 || 2 || 3 || 4
	largeBlkCaches []*largeblkcache.Comp // b.coherenceDirectory == 1

	l2TLBs                          []*tlb.Comp
	drams                           []sim.Component
	internalConn                    *directconnection.Comp
	l2ToDramConnection              *directconnection.Comp
	l1AddressMapper                 *mem.InterleavedAddressPortMapper
	cohDirAddressMapper             *mem.InterleavedAddressPortMapper
	cohDirAddressMapperForRemoteReq *mem.InterleavedAddressPortMapper
	l1TLBAddressMapper              *mem.SinglePortMapper
	pmcAddressMapper                mem.AddressToPortMapper
	gmmu                            *gmmu.Comp

	accessCounter       *map[vm.PID]map[uint64]uint8
	dirtyMask           *[]map[vm.PID]map[uint64][]uint8
	readMask            *[]map[vm.PID]map[uint64][]uint8
	pageMigrationPolicy uint64
	coherenceDirectory  uint64
	idealDirectory      bool
	equalDirCap         bool // [EQUAL-DIR-CAP] CD/HMG/REC use SD's inflight cap (256/full-remote) for fair comparison
	cd8DeadlockFix      bool // [CD8-DEADLOCK FIX] toggle the L2 invDirtyFlushReserve
	sdAckReserve        bool // [SD-ACK-RESERVE] toggle the L2 ackDisplaceReserve
	sdPeerServeReserve  bool // [SD-PEER-SERVE-RESERVE] toggle the SuperDir peer-serve inflight reserve
	l2PeerEvictHeadroom bool // [L2-PEER-EVICT-HEADROOM] toggle the L2 peer-serve eviction credit headroom
	cdFifoReplacement   bool // FIFO replacement for CD/HMG (paper §4.2 baseline)
}

// MakeBuilder creates a new builder.
func MakeBuilder() Builder {
	return Builder{
		freq:                           1 * sim.GHz,
		numCUPerShaderArray:            4,
		numShaderArray:                 16,
		l2CacheSize:                    2 * mem.MB,
		numMemoryBank:                  16,
		log2CacheLineSize:              6,
		log2PageSize:                   12,
		log2MemoryBankInterleavingSize: 7,
		cohDirSize:                     512 * mem.KB,
		sdNumBanks:                     5,
		sdLog2NumSubEntry:              2,
		mgdRegionSize:                  1024,
		memAddrOffset:                  0,
		dramSize:                       4 * mem.GB,
		// l2CacheSize:                    2 * mem.MB,
		// cohDirSize:                     512 * mem.KB,
	}
}

// WithSimulation sets the simulation to use.
func (b Builder) WithSimulation(sim *simulation.Simulation) Builder {
	b.simulation = sim
	return b
}

// WithGPUID sets the GPU ID to use.
func (b Builder) WithGPUID(id uint64) Builder {
	b.gpuID = id
	return b
}

// WithFreq sets the frequency that the GPU works at.
func (b Builder) WithFreq(freq sim.Freq) Builder {
	b.freq = freq
	return b
}

// WithLog2MemoryBankInterleavingSize sets the log2 memory bank interleaving
// size.
func (b Builder) WithLog2MemoryBankInterleavingSize(size uint64) Builder {
	b.log2MemoryBankInterleavingSize = size
	return b
}

// WithLog2CacheLineSize sets the log2 cache line size.
func (b Builder) WithLog2CacheLineSize(size uint64) Builder {
	b.log2CacheLineSize = size
	return b
}

// WithLog2PageSize sets the log2 page size.
func (b Builder) WithLog2PageSize(size uint64) Builder {
	b.log2PageSize = size
	return b
}

func (b Builder) WithLog2CoherenceUnitSize(size uint64) Builder {
	b.log2CoherenceUnitSize = size
	return b
}

// WithMemAddrOffset sets the memory address offset.
func (b Builder) WithMemAddrOffset(offset uint64) Builder {
	b.memAddrOffset = offset
	return b
}

// WithNumCUPerShaderArray sets the number of CUs per shader array.
func (b Builder) WithNumCUPerShaderArray(numCUPerShaderArray int) Builder {
	b.numCUPerShaderArray = numCUPerShaderArray
	return b
}

// WithNumShaderArray sets the number of shader arrays.
func (b Builder) WithNumShaderArray(numShaderArray int) Builder {
	b.numShaderArray = numShaderArray
	return b
}

// WithL2CacheSize sets the size of the L2 cache.
func (b Builder) WithL2CacheSize(size uint64) Builder {
	b.l2CacheSize = size
	return b
}

// WithNumMemoryBank sets the number of memory banks.
func (b Builder) WithNumMemoryBank(numMemoryBank int) Builder {
	b.numMemoryBank = numMemoryBank
	return b
}

// WithDramSize sets the size of the DRAM.
func (b Builder) WithDramSize(size uint64) Builder {
	b.dramSize = size
	return b
}

// WithMMU sets the MMU that can provide the ultimate address translation.
func (b Builder) WithMMU(mmu *mmu.Comp) Builder {
	b.mmu = mmu
	return b
}

// WithGlobalStorage sets the global storage that can provide the ultimate address translation.
func (b Builder) WithGlobalStorage(
	globalStorage *mem.Storage,
) Builder {
	b.globalStorage = globalStorage
	return b
}

// WithDRAMSize sets the size of the DRAM.
func (b Builder) WithDRAMSize(size uint64) Builder {
	b.dramSize = size
	return b
}

// WithRDMAAddressMapper sets the RDMA address mapper.
func (b Builder) WithRDMAAddressMapper(mapper mem.AddressToPortMapper) Builder {
	b.rdmaAddressMapper = mapper
	return b
}

// WithRDMAAddressMapper sets the RDMA address mapper.
func (b Builder) WithDriver(driver *driver.Driver) Builder {
	b.driver = driver
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

// WithEqualDirCap, when on, makes CD/HMG/REC use the SAME inflight-fetch cap
// as SuperDirectory (total=256, cross-GPU remote=full budget) so the
// directory-admission throughput is matched across variants. Off (default)
// preserves each variant's historical caps (CD/HMG total=128/remote=96,
// REC total=128/remote=full). See -equal-dir-cap.
func (b Builder) WithEqualDirCap(on bool) Builder {
	b.equalDirCap = on
	return b
}

// dirInflightCap returns the total inflight-fetch cap for CD/HMG/REC:
// SD's 256 when equalDirCap is on, else the variant's historical base.
func (b Builder) dirInflightCap(base int) int {
	if b.equalDirCap {
		return 256
	}
	return base
}

// dirRemoteSubCap returns the cross-GPU outgoing fetch sub-cap for CD/HMG:
// 0 (disabled = full budget, SD parity) when equalDirCap is on, else base
// (-1 = auto 3/4).
func (b Builder) dirRemoteSubCap(base int) int {
	if b.equalDirCap {
		return 0
	}
	return base
}

// WithCD8DeadlockFix toggles the L2 invDirtyFlushReserve (CD_8 deadlock fix).
func (b Builder) WithCD8DeadlockFix(on bool) Builder {
	b.cd8DeadlockFix = on
	return b
}

// WithL2PeerEvictHeadroom toggles the L2 peer-serve eviction credit headroom
// (fixes the 4-GPU symmetric peer-eviction credit deadlock).
func (b Builder) WithL2PeerEvictHeadroom(on bool) Builder {
	b.l2PeerEvictHeadroom = on
	return b
}

// WithSDAckReserve toggles the L2 ackDisplaceReserve (SD 9-bank deadlock fix).
func (b Builder) WithSDAckReserve(on bool) Builder {
	b.sdAckReserve = on
	return b
}

// WithSDPeerServeReserve toggles the SuperDir peer-serve inflight reserve
// (SD 9-bank capacity-cycle deadlock fix).
func (b Builder) WithSDPeerServeReserve(on bool) Builder {
	b.sdPeerServeReserve = on
	return b
}

func (b Builder) WithCDFifoReplacement(v bool) Builder {
	b.cdFifoReplacement = v
	return b
}

func (b Builder) WithCohDirSize(size uint64) Builder {
	b.cohDirSize = size
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

// WithInvExtraLatency adds n extra directory-pipeline stages to the L2's
// dedicated invalidation pipeline (writebackcoh invPipeline). Used to
// measure how much cross-GPU InvReq handling cost contributes to runtime
// without perturbing the regular read/write paths.
func (b Builder) WithInvExtraLatency(n int) Builder {
	b.invExtraLatency = n
	return b
}

// Build builds the hardware platform.
func (b Builder) Build(name string) *sim.Domain {
	b.name = name
	b.gpu = sim.NewDomain(name)

	b.l1AddressMapper = mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize,
	)
	b.l1AddressMapper.UseAddressSpaceLimitation = false

	b.cohDirAddressMapper = mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize,
	)
	b.cohDirAddressMapper.UseAddressSpaceLimitation = false

	b.cohDirAddressMapperForRemoteReq = mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize,
	)
	b.cohDirAddressMapperForRemoteReq.UseAddressSpaceLimitation = false

	b.rdmaLowModuleFinder = mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize,
	)
	b.rdmaLowModuleFinder.UseAddressSpaceLimitation = false

	b.rdmaInvLowModuleFinder = mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize,
	)
	b.rdmaInvLowModuleFinder.UseAddressSpaceLimitation = false

	b.rdmaBottomAddressMapper = mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize,
	)
	b.rdmaBottomAddressMapper.UseAddressSpaceLimitation = false

	b.l1TLBAddressMapper = &mem.SinglePortMapper{}

	b.accessCounter = &map[vm.PID]map[uint64]uint8{}
	targetLen := int(b.gpuID)
	if len(b.driver.DirtyMask) < targetLen {
		diff := targetLen - len(b.driver.DirtyMask)
		for i := 0; i < diff; i++ {
			b.driver.DirtyMask = append(b.driver.DirtyMask, make(map[vm.PID]map[uint64][]uint8))
			b.driver.ReadMask = append(b.driver.ReadMask, make(map[vm.PID]map[uint64][]uint8))
		}
	}
	b.dirtyMask = &(b.driver.DirtyMask)
	b.readMask = &(b.driver.ReadMask)

	if b.dirtyMask == nil {
		fmt.Printf("[r9nanoBuilder]\tWarning: GPU %d has no dirty mask set.\n", b.gpuID)
	}
	if b.readMask == nil {
		fmt.Printf("[r9nanoBuilder]\tWarning: GPU %d has no read mask set.\n", b.gpuID)
	}

	b.buildSAs()
	b.buildDRAMControllers()
	b.buildCoherenceDirectory()
	b.buildL2Caches()
	b.buildCP()
	b.buildGMMU()
	b.buildL2TLB()

	b.connectCP()
	b.connectL2AndDRAM()
	b.connectL1ToCohDir()
	b.connectCohDirToL2()
	b.connectL1TLBToL2TLB()
	b.connectL2TLBToGMMU()

	b.populateExternalPorts()

	return b.gpu
}

func (b *Builder) populateExternalPorts() {
	b.gpu.AddPort("CommandProcessor", b.cp.ToDriver)

	// [R1] Four typed wire-side ports. Each direction × type has a
	// dedicated external port so the inter-GPU NVLink fabric can route
	// every message class independently and a stalled class can no
	// longer head-of-line block the others.
	b.gpu.AddPort("RDMADataReq", b.rdmaEngine.RDMADataReqOutside)
	b.gpu.AddPort("RDMADataRsp", b.rdmaEngine.RDMADataRspOutside)
	b.gpu.AddPort("RDMAInvReq", b.rdmaEngine.RDMAInvReqOutside)
	b.gpu.AddPort("RDMAInvRsp", b.rdmaEngine.RDMAInvRspOutside)

	// [R1] Legacy aliases removed — they aliased two names to the same
	// underlying RDMADataReqOutside port which caused InterGPU's PlugIn
	// to call SetConnection twice on the same port (panic). Any caller
	// using "RDMARequest" / "RDMAData" must migrate to the typed names
	// "RDMADataReq" / "RDMADataRsp" / "RDMAInvReq" / "RDMAInvRsp".

	b.gpu.AddPort("PageMigrationController",
		b.pmc.GetPortByName("Remote"))

	b.gpu.AddPort("Translation", b.gmmu.GetPortByName("Bottom"))
}

func (b *Builder) connectCP() {
	b.internalConn = directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".InternalConn")
	b.simulation.RegisterComponent(b.internalConn)

	b.internalConn.PlugIn(b.cp.ToDMA)
	b.internalConn.PlugIn(b.cp.ToCohDir)
	b.internalConn.PlugIn(b.cp.ToCaches)
	b.internalConn.PlugIn(b.cp.ToCUs)
	b.internalConn.PlugIn(b.cp.ToTLBs)
	b.internalConn.PlugIn(b.cp.ToAddressTranslators)
	b.internalConn.PlugIn(b.cp.ToRDMA)
	b.internalConn.PlugIn(b.cp.ToPMC)
	b.internalConn.PlugIn(b.cp.ToGMMU)
	b.internalConn.PlugIn(b.cp.ToROBs)

	b.cp.RDMA = b.rdmaEngine.CtrlPort
	b.internalConn.PlugIn(b.cp.RDMA)

	b.cp.DMAEngine = b.dmaEngine.ToCP
	b.internalConn.PlugIn(b.dmaEngine.ToCP)

	pmcControlPort := b.pmc.GetPortByName("Control")
	b.cp.PMC = pmcControlPort
	b.internalConn.PlugIn(pmcControlPort)

	gmmuControlPort := b.gmmu.GetPortByName("Control")
	b.cp.GMMU = gmmuControlPort
	b.internalConn.PlugIn(gmmuControlPort)

	b.connectCPWithCUs()
	b.connectCPWithAddressTranslators()
	b.connectCPWithTLBs()
	b.connectCPWithCohDir()
	b.connectCPWithCaches()
	b.connectCPWithROBs()
}

func (b *Builder) connectL1ToCohDir() {
	l1ToCohDir := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".L1ToCohDir")

	RDMAToCohDir := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".RDMAToCohDir")

	RDMAToCohDirForInv := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".RDMAToCohDirForInv")

	// [ITER7] Dedicated link for INV RSP separating it from INV REQ
	// on the RDMA<->Directory inside path. RDMA puts INV RSP into
	// RDMAInvRspInside; the directory's RDMAInvRspPort receives.
	RDMAToCohDirForInvRsp := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".RDMAToCohDirForInvRsp")

	// [R2 BUGFIX] All typed data ports share the SAME directconnection
	// (RDMAToCohDir).  Per-message-type ports give us HoL-free incomingBuf
	// at REC; the directconnection just routes by Dst name. Separate
	// connections would require RSP routing to find the original REQ's
	// Src port in its registry — which fails because that port is on the
	// other connection. One connection with all ports plugged makes lookup
	// work for any (Src, Dst) pair.
	RDMAToCohDir.PlugIn(b.rdmaEngine.RDMADataInside)
	RDMAToCohDirForInv.PlugIn(b.rdmaEngine.RDMAInvInside)
	RDMAToCohDirForInvRsp.PlugIn(b.rdmaEngine.RDMAInvRspInside)
	// [R2] New typed Inside ports — share RDMAToCohDir so REC's RSP from
	// RDMADataRspPort with Dst=RDMA.RDMADataReqInside routes correctly.
	RDMAToCohDir.PlugIn(b.rdmaEngine.RDMADataReqInside)
	RDMAToCohDir.PlugIn(b.rdmaEngine.RDMADataRspInside)

	if b.coherenceDirectory == 0 { // coherenceDirectory
		l1ToCohDir.PlugIn(b.cohDir.GetPortByName("Top"))
		// D4 (ported from SD): plug the L1-facing dedicated InvRsp
		// ingress into the same L1ToCohDir directconnection. Any L1
		// InvRsp addressed to this port reaches the directory without
		// being head-blocked by a stalled ReadReq at topPort.
		l1ToCohDir.PlugIn(b.cohDir.GetPortByName("TopInvRsp"))
		RDMAToCohDir.PlugIn(b.cohDir.GetPortByName("RDMA"))
		RDMAToCohDirForInv.PlugIn(b.cohDir.GetPortByName("RDMAInv"))
		// Fix: plug RDMAInvRsp for all variants whose directory builder
		// creates the port. Previously only REC (ITER7) had this. With
		// L2 NumReqPerCycle=4 the inv-rsp path through RDMA is exercised
		// even in CD-only sweeps; without the plug, directconnection
		// panics: "port GPU[X].CohDir.RDMAInvRspPort not found".
		RDMAToCohDirForInvRsp.PlugIn(b.cohDir.GetPortByName("RDMAInvRsp"))
		// S1 (ported from SD): plug the new InvRsp egress port into
		// the same RDMAToCohDirForInvRsp connection so outbound InvRsp
		// reaches RDMA's RDMAInvRspInside without competing with InvReq
		// egress (RDMA's processFromInvInside panics on InvRsp).
		RDMAToCohDirForInvRsp.PlugIn(b.cohDir.GetPortByName("RDMAInvRspOut"))

	} else if b.coherenceDirectory == 1 { // large block cache
		l1ToCohDir.PlugIn(b.cohDir.GetPortByName("Top"))
		// D4 / S1: see comments in branch 0.
		l1ToCohDir.PlugIn(b.cohDir.GetPortByName("TopInvRsp"))
		RDMAToCohDir.PlugIn(b.cohDir.GetPortByName("RDMA"))
		RDMAToCohDirForInv.PlugIn(b.cohDir.GetPortByName("RDMAInv"))
		RDMAToCohDirForInvRsp.PlugIn(b.cohDir.GetPortByName("RDMAInvRsp"))
		RDMAToCohDirForInvRsp.PlugIn(b.cohDir.GetPortByName("RDMAInvRspOut"))

	} else if b.coherenceDirectory == 2 { // superDirectory
		l1ToCohDir.PlugIn(b.superDir.GetPortByName("Top"))
		// D4 / S1: L1-facing InvRsp ingress + RDMA-facing InvRsp egress.
		l1ToCohDir.PlugIn(b.superDir.GetPortByName("TopInvRsp"))
		RDMAToCohDir.PlugIn(b.superDir.GetPortByName("RDMA"))
		RDMAToCohDirForInv.PlugIn(b.superDir.GetPortByName("RDMAInv"))
		RDMAToCohDirForInvRsp.PlugIn(b.superDir.GetPortByName("RDMAInvRsp"))
		RDMAToCohDirForInvRsp.PlugIn(b.superDir.GetPortByName("RDMAInvRspOut"))

	} else if b.coherenceDirectory == 3 { // REC
		l1ToCohDir.PlugIn(b.recDir.GetPortByName("Top"))
		RDMAToCohDir.PlugIn(b.recDir.GetPortByName("RDMA"))
		RDMAToCohDirForInv.PlugIn(b.recDir.GetPortByName("RDMAInv"))
		// [ITER7] Plug REC's RDMAInvRsp port (always created by REC
		// builder but previously left unplugged) to the dedicated
		// inv-rsp link.
		RDMAToCohDirForInvRsp.PlugIn(b.recDir.GetPortByName("RDMAInvRsp"))
		// [ITER19] Plug REC's dedicated InvRsp EGRESS port into ForInvRsp
		// (matches SD branch:561 / HMG branch:585). REC drains
		// sendToRDMAInvRspQue from this port; RDMA.RDMAInvRspInside is also
		// on ForInvRsp (builder:518), so the InvRsp routes cleanly.
		RDMAToCohDirForInvRsp.PlugIn(b.recDir.GetPortByName("RDMAInvRspOut"))
		// [R2] Plug REC's split data ports into the SHARED RDMAToCohDir
		// (along with R1's RDMADataReqInside/RspInside on the RDMA side).
		// Single connection lets the directory's RSP from RDMADataRspPort
		// reach RDMA's RDMADataReqInside (the Src of the original REQ).
		RDMAToCohDir.PlugIn(b.recDir.GetPortByName("RDMADataReq"))
		RDMAToCohDir.PlugIn(b.recDir.GetPortByName("RDMADataRsp"))

	} else if b.coherenceDirectory == 4 { // HMG
		l1ToCohDir.PlugIn(b.cohDir.GetPortByName("Top"))
		// D4 / S1: see comments in branch 0.
		l1ToCohDir.PlugIn(b.cohDir.GetPortByName("TopInvRsp"))
		RDMAToCohDir.PlugIn(b.cohDir.GetPortByName("RDMA"))
		RDMAToCohDirForInv.PlugIn(b.cohDir.GetPortByName("RDMAInv"))
		RDMAToCohDirForInvRsp.PlugIn(b.cohDir.GetPortByName("RDMAInvRsp"))
		RDMAToCohDirForInvRsp.PlugIn(b.cohDir.GetPortByName("RDMAInvRspOut"))

	} else if b.coherenceDirectory == 5 { // MGD
		l1ToCohDir.PlugIn(b.mgdDir.GetPortByName("Top"))
		RDMAToCohDir.PlugIn(b.mgdDir.GetPortByName("RDMA"))
		RDMAToCohDirForInv.PlugIn(b.mgdDir.GetPortByName("RDMAInv"))
		// MGD doesn't have RDMAInvRsp port — skip.

	}
	// b.rdmaEngine.SetLocalModuleFinder(b.l1AddressMapper)
	b.rdmaEngine.SetLocalModuleFinder(b.rdmaLowModuleFinder)
	b.rdmaEngine.SetLocalInvModuleFinder(b.rdmaInvLowModuleFinder)
	b.rdmaEngine.SetLocalModuleBottomFinder(b.rdmaBottomAddressMapper)

	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			l1ToCohDir.PlugIn(
				sa.GetPortByName(fmt.Sprintf("L1VCacheBottom[%d]", i)))
		}

		l1ToCohDir.PlugIn(sa.GetPortByName("L1SCacheBottom"))
		l1ToCohDir.PlugIn(sa.GetPortByName("L1ICacheBottom"))
	}

	if b.coherenceDirectory == 0 { // coherenceDirectory
		b.cohDir.ToRDMA = b.rdmaEngine.RDMADataInside.AsRemote()
		b.cohDir.ToRDMAInv = b.rdmaEngine.RDMAInvInside.AsRemote()
	} else if b.coherenceDirectory == 1 { // large block cache
		b.cohDir.ToRDMA = b.rdmaEngine.RDMADataInside.AsRemote()
		b.cohDir.ToRDMAInv = b.rdmaEngine.RDMAInvInside.AsRemote()
	} else if b.coherenceDirectory == 2 { // superDirectory
		b.superDir.ToRDMA = b.rdmaEngine.RDMADataInside.AsRemote()
		b.superDir.ToRDMAInv = b.rdmaEngine.RDMAInvInside.AsRemote()
	} else if b.coherenceDirectory == 3 { // REC
		b.recDir.ToRDMA = b.rdmaEngine.RDMADataInside.AsRemote()
		b.recDir.ToRDMAInv = b.rdmaEngine.RDMAInvInside.AsRemote()
		// [ITER7] Tell REC where to send INV RSPs going outbound.
		b.recDir.ToRDMAInvRsp = b.rdmaEngine.RDMAInvRspInside.AsRemote()
		// [R2] Tell REC where to send peer-facing data REQ / RSP outbound.
		// NOTE: these ToRDMADataReq/Rsp fields are vestigial — REC derives the
		// peer-serve rsp Dst from req.Src verbatim, never from these fields.
		// The actual deadlock fix is the processReqInsideRsp reader in
		// rdma/comp.go that drains RDMADataReqInside.IncomingBuf.
		b.recDir.ToRDMADataReq = b.rdmaEngine.RDMADataReqInside.AsRemote()
		b.recDir.ToRDMADataRsp = b.rdmaEngine.RDMADataRspInside.AsRemote()
	} else if b.coherenceDirectory == 4 { // HMG
		b.cohDir.ToRDMA = b.rdmaEngine.RDMADataInside.AsRemote()
		b.cohDir.ToRDMAInv = b.rdmaEngine.RDMAInvInside.AsRemote()
	} else if b.coherenceDirectory == 5 { // MGD
		b.mgdDir.ToRDMA = b.rdmaEngine.RDMADataInside.AsRemote()
		b.mgdDir.ToRDMAInv = b.rdmaEngine.RDMAInvInside.AsRemote()
	}
}

func (b *Builder) connectCohDirToL2() {
	CohDirToL2Conn := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".CohDirToL2")
	CohDirToL2ConnForRemote := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".CohDirToL2ForRemote")

	if b.coherenceDirectory == 0 { // coherenceDirectory
		CohDirToL2Conn.PlugIn(b.cohDir.GetPortByName("Bottom"))
		CohDirToL2ConnForRemote.PlugIn(b.cohDir.GetPortByName("RemoteBottom"))

		for _, l2 := range b.l2Caches {
			CohDirToL2Conn.PlugIn(l2.GetPortByName("Top"))
			CohDirToL2ConnForRemote.PlugIn(l2.GetPortByName("RemoteTop"))
		}
		b.cohDir.SetAddressToPortMapper(b.cohDirAddressMapper)
		b.cohDir.SetAddressToPortMapperForRemoteReq(b.cohDirAddressMapperForRemoteReq)

	} else if b.coherenceDirectory == 1 { // large block cache
		CohDirToL2Conn.PlugIn(b.cohDir.GetPortByName("Bottom"))
		CohDirToL2ConnForRemote.PlugIn(b.cohDir.GetPortByName("RemoteBottom"))

		for _, l2 := range b.largeBlkCaches {
			CohDirToL2Conn.PlugIn(l2.GetPortByName("Top"))
			CohDirToL2ConnForRemote.PlugIn(l2.GetPortByName("RemoteTop"))
		}
		b.cohDir.SetAddressToPortMapper(b.cohDirAddressMapper)
		b.cohDir.SetAddressToPortMapperForRemoteReq(b.cohDirAddressMapperForRemoteReq)

	} else if b.coherenceDirectory == 2 { // superDirectory
		CohDirToL2Conn.PlugIn(b.superDir.GetPortByName("Bottom"))
		CohDirToL2ConnForRemote.PlugIn(b.superDir.GetPortByName("RemoteBottom"))

		for _, l2 := range b.l2Caches {
			CohDirToL2Conn.PlugIn(l2.GetPortByName("Top"))
			CohDirToL2ConnForRemote.PlugIn(l2.GetPortByName("RemoteTop"))
		}
		b.superDir.SetAddressToPortMapper(b.cohDirAddressMapper)
		b.superDir.SetAddressToPortMapperForRemoteReq(b.cohDirAddressMapperForRemoteReq)

	} else if b.coherenceDirectory == 5 { // MGD
		CohDirToL2Conn.PlugIn(b.mgdDir.GetPortByName("Bottom"))
		CohDirToL2ConnForRemote.PlugIn(b.mgdDir.GetPortByName("RemoteBottom"))

		for _, l2 := range b.l2Caches {
			CohDirToL2Conn.PlugIn(l2.GetPortByName("Top"))
			CohDirToL2ConnForRemote.PlugIn(l2.GetPortByName("RemoteTop"))
		}
		b.mgdDir.SetAddressToPortMapper(b.cohDirAddressMapper)
		b.mgdDir.SetAddressToPortMapperForRemoteReq(b.cohDirAddressMapperForRemoteReq)

	} else if b.coherenceDirectory == 3 { // REC
		CohDirToL2Conn.PlugIn(b.recDir.GetPortByName("Bottom"))
		CohDirToL2ConnForRemote.PlugIn(b.recDir.GetPortByName("RemoteBottom"))

		for _, l2 := range b.l2Caches {
			CohDirToL2Conn.PlugIn(l2.GetPortByName("Top"))
			CohDirToL2ConnForRemote.PlugIn(l2.GetPortByName("RemoteTop"))
		}
		b.recDir.SetAddressToPortMapper(b.cohDirAddressMapper)
		b.recDir.SetAddressToPortMapperForRemoteReq(b.cohDirAddressMapperForRemoteReq)

	} else if b.coherenceDirectory == 4 { // HMG
		CohDirToL2Conn.PlugIn(b.cohDir.GetPortByName("Bottom"))
		CohDirToL2ConnForRemote.PlugIn(b.cohDir.GetPortByName("RemoteBottom"))

		for _, l2 := range b.l2Caches {
			CohDirToL2Conn.PlugIn(l2.GetPortByName("Top"))
			CohDirToL2ConnForRemote.PlugIn(l2.GetPortByName("RemoteTop"))
		}
		b.cohDir.SetAddressToPortMapper(b.cohDirAddressMapper)
		b.cohDir.SetAddressToPortMapperForRemoteReq(b.cohDirAddressMapperForRemoteReq)
	}
}

func (b *Builder) connectL2AndDRAM() {
	b.l2ToDramConnection = directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".L2ToDRAM")
	b.simulation.RegisterComponent(b.l2ToDramConnection)

	lowModuleFinder := mem.NewInterleavedAddressPortMapper(
		1 << b.log2MemoryBankInterleavingSize)

	b.l2ToDramConnection.PlugIn(b.rdmaEngine.RDMARequestInside)

	var mapperForDir *mem.L2BottomMapper
	if b.coherenceDirectory == 1 {
		for i, l2 := range b.largeBlkCaches {
			b.l2ToDramConnection.PlugIn(l2.GetPortByName("Bottom"))

			mapper := &mem.L2BottomMapper{
				LocalBank: b.drams[i].GetPortByName("Top").AsRemote(),
				RDMAPort:  b.rdmaEngine.RDMARequestInside.AsRemote(),
				LocalLow:  b.memAddrOffset,
				LocalHigh: b.memAddrOffset + b.dramSize,
			}
			l2.SetAddressToPortMapper(mapper)

			if i == 0 { // request가 remote/local data에 대한 것인지 판단하기 위함
				mapperForDir = mapper
			}
		}
	} else {
		for i, l2 := range b.l2Caches {
			b.l2ToDramConnection.PlugIn(l2.GetPortByName("Bottom"))
			// [L2 LOCAL/REMOTE SPLIT] remote-destined egress shares the same
			// connection (DRAM + RDMA both reachable); its own port gives it an
			// independent CanSend so local-DRAM egress can't starve it.
			b.l2ToDramConnection.PlugIn(l2.GetPortByName("RemoteBottom"))

			mapper := &mem.L2BottomMapper{
				LocalBank: b.drams[i].GetPortByName("Top").AsRemote(),
				RDMAPort:  b.rdmaEngine.RDMARequestInside.AsRemote(),
				LocalLow:  b.memAddrOffset,
				LocalHigh: b.memAddrOffset + b.dramSize,
			}
			l2.SetAddressToPortMapper(mapper)

			if i == 0 { // request가 remote/local data에 대한 것인지 판단하기 위함
				mapperForDir = mapper
			}
		}
	}

	if b.coherenceDirectory == 0 { // coherenceDirectory
		b.cohDir.SetL2AddressToPortMapper(mapperForDir)
	} else if b.coherenceDirectory == 1 { // large block cache
		b.cohDir.SetL2AddressToPortMapper(mapperForDir)
	} else if b.coherenceDirectory == 2 { // superDirectory
		b.superDir.SetL2AddressToPortMapper(mapperForDir)
	} else if b.coherenceDirectory == 3 { // REC
		b.recDir.SetL2AddressToPortMapper(mapperForDir)
	} else if b.coherenceDirectory == 4 { // HMG
		b.cohDir.SetL2AddressToPortMapper(mapperForDir)
	} else if b.coherenceDirectory == 5 { // MGD
		b.mgdDir.SetL2AddressToPortMapper(mapperForDir)
	}

	for _, dram := range b.drams {
		b.l2ToDramConnection.PlugIn(dram.GetPortByName("Top"))
		lowModuleFinder.LowModules = append(lowModuleFinder.LowModules,
			dram.GetPortByName("Top").AsRemote())
	}

	b.dmaEngine.SetLocalDataSource(lowModuleFinder)
	b.l2ToDramConnection.PlugIn(b.dmaEngine.ToMem)

	b.pmc.MemCtrlFinder = lowModuleFinder
	b.l2ToDramConnection.PlugIn(b.pmc.GetPortByName("LocalMem"))
}

func (b *Builder) connectL1TLBToL2TLB() {
	tlbConn := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".L1TLBToL2TLB")

	tlbConn.PlugIn(b.l2TLBs[0].GetPortByName("Top"))

	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			tlbConn.PlugIn(
				sa.GetPortByName(fmt.Sprintf("L1VTLBBottom[%d]", i)))
		}

		tlbConn.PlugIn(sa.GetPortByName("L1STLBBottom"))
		tlbConn.PlugIn(sa.GetPortByName("L1ITLBBottom"))
	}
}

func (b *Builder) connectL2TLBToGMMU() {

	gmmuConn := directconnection.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		Build(b.name + ".L2TLBToGMMU")

	gmmuConn.PlugIn(b.gmmu.GetPortByName("Top"))

	for _, l2 := range b.l2TLBs {
		gmmuConn.PlugIn(l2.GetPortByName("Bottom"))
	}
}

type cuInterfaceForCP struct {
	ctrlPort        sim.RemotePort
	dispatchingPort sim.RemotePort
	wfPoolSizes     []int
	vRegCounts      []int
	sRegCount       int
	ldsBytes        int
}

func (cu cuInterfaceForCP) ControlPort() sim.RemotePort {
	return cu.ctrlPort
}

func (cu cuInterfaceForCP) DispatchingPort() sim.RemotePort {
	return cu.dispatchingPort
}

func (cu cuInterfaceForCP) WfPoolSizes() []int {
	return cu.wfPoolSizes
}

func (cu cuInterfaceForCP) VRegCounts() []int {
	return cu.vRegCounts
}

func (cu cuInterfaceForCP) SRegCount() int {
	return cu.sRegCount
}

func (cu cuInterfaceForCP) LDSBytes() int {
	return cu.ldsBytes
}

func (b *Builder) connectCPWithCUs() {
	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			cuDispatchingPort := sa.GetPortByName(
				fmt.Sprintf("CU[%d]", i))
			cuCtrlPort := sa.GetPortByName(
				fmt.Sprintf("CUCtrl[%d]", i))
			cu := cuInterfaceForCP{
				ctrlPort:        cuCtrlPort.AsRemote(),
				dispatchingPort: cuDispatchingPort.AsRemote(),
				wfPoolSizes:     []int{10, 10, 10, 10},
				vRegCounts:      []int{16384, 16384, 16384, 16384},
				sRegCount:       3200,
				ldsBytes:        64 * 1024,
			}

			b.cp.RegisterCU(cu)

			b.internalConn.PlugIn(cuDispatchingPort)
			b.internalConn.PlugIn(cuCtrlPort)
		}
	}
}

func (b *Builder) connectCPWithAddressTranslators() {
	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			at := sa.GetPortByName(fmt.Sprintf("L1VAddrTransCtrl[%d]", i))
			b.cp.AddressTranslators = append(b.cp.AddressTranslators, at)
			b.internalConn.PlugIn(at)
		}

		l1sAT := sa.GetPortByName("L1SAddrTransCtrl")
		b.cp.AddressTranslators = append(b.cp.AddressTranslators, l1sAT)
		b.internalConn.PlugIn(l1sAT)

		l1iAT := sa.GetPortByName("L1IAddrTransCtrl")
		b.cp.AddressTranslators = append(b.cp.AddressTranslators, l1iAT)
		b.internalConn.PlugIn(l1iAT)
	}
}

func (b *Builder) connectCPWithTLBs() {
	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			tlb := sa.GetPortByName(fmt.Sprintf("L1VTLBCtrl[%d]", i))
			b.cp.TLBs = append(b.cp.TLBs, tlb)
			b.internalConn.PlugIn(tlb)
		}

		l1sTLB := sa.GetPortByName("L1STLBCtrl")
		b.cp.TLBs = append(b.cp.TLBs, l1sTLB)
		b.internalConn.PlugIn(l1sTLB)

		l1iTLB := sa.GetPortByName("L1ITLBCtrl")
		b.cp.TLBs = append(b.cp.TLBs, l1iTLB)
		b.internalConn.PlugIn(l1iTLB)
	}

	for _, tlb := range b.l2TLBs {
		ctrlPort := tlb.GetPortByName("Control")
		b.cp.TLBs = append(b.cp.TLBs, ctrlPort)
		b.internalConn.PlugIn(ctrlPort)
	}
}

func (b *Builder) connectCPWithCohDir() {
	var cohDirPort sim.Port

	if b.coherenceDirectory == 0 { // coherenceDirectory
		cohDirPort = b.cohDir.GetPortByName("Control")
	} else if b.coherenceDirectory == 1 { // large block cache
		cohDirPort = b.cohDir.GetPortByName("Control")
	} else if b.coherenceDirectory == 2 { // superDirectory
		cohDirPort = b.superDir.GetPortByName("Control")
	} else if b.coherenceDirectory == 3 { // REC
		cohDirPort = b.recDir.GetPortByName("Control")
	} else if b.coherenceDirectory == 4 { // HMG
		cohDirPort = b.cohDir.GetPortByName("Control")
	} else if b.coherenceDirectory == 5 { // MGD
		cohDirPort = b.mgdDir.GetPortByName("Control")
	}

	b.cp.CohDirectory = cohDirPort
	b.internalConn.PlugIn(cohDirPort)
}

func (b *Builder) connectCPWithCaches() {
	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			cache := sa.GetPortByName(fmt.Sprintf("L1VCacheCtrl[%d]", i))
			b.cp.L1VCaches = append(b.cp.L1VCaches, cache)
			b.internalConn.PlugIn(cache)
		}

		l1sCache := sa.GetPortByName("L1SCacheCtrl")
		b.cp.L1SCaches = append(b.cp.L1SCaches, l1sCache)
		b.internalConn.PlugIn(l1sCache)

		l1iCache := sa.GetPortByName("L1ICacheCtrl")
		b.cp.L1ICaches = append(b.cp.L1ICaches, l1iCache)
		b.internalConn.PlugIn(l1iCache)
	}

	if b.coherenceDirectory == 1 {
		for _, c := range b.largeBlkCaches {
			ctrlPort := c.GetPortByName("Control")
			b.cp.L2Caches = append(b.cp.L2Caches, ctrlPort)
			b.internalConn.PlugIn(ctrlPort)
		}
	} else {
		for _, c := range b.l2Caches {
			ctrlPort := c.GetPortByName("Control")
			b.cp.L2Caches = append(b.cp.L2Caches, ctrlPort)
			b.internalConn.PlugIn(ctrlPort)
		}

	}
}

func (b *Builder) connectCPWithROBs() {
	for _, sa := range b.sas {
		for i := range b.numCUPerShaderArray {
			l1vrob := sa.GetPortByName(fmt.Sprintf("L1VROBCtrl[%d]", i))
			b.cp.L1VROBs = append(b.cp.L1VROBs, l1vrob)
			b.internalConn.PlugIn(l1vrob)
		}

		l1srob := sa.GetPortByName("L1SROBCtrl")
		b.cp.L1SROBs = append(b.cp.L1SROBs, l1srob)
		b.internalConn.PlugIn(l1srob)

		l1irob := sa.GetPortByName("L1IROBCtrl")
		b.cp.L1IROBs = append(b.cp.L1IROBs, l1irob)
		b.internalConn.PlugIn(l1irob)
	}
}

func (b *Builder) buildSAs() {
	saBuilder := shaderarray.MakeBuilder().
		WithSimulation(b.simulation).
		WithFreq(b.freq).
		WithGPUID(b.gpuID).
		WithNumCUs(b.numCUPerShaderArray).
		WithLog2CacheLineSize(b.log2CacheLineSize).
		WithLog2PageSize(b.log2PageSize).
		WithL1AddressMapper(b.l1AddressMapper).
		WithL1TLBAddressMapper(b.l1TLBAddressMapper).
		WithVisTracer(b.simulation.GetVisTracer()).
		WithAccessCounter(b.accessCounter).
		WithDirtyMask(b.dirtyMask).
		WithReadMask(b.readMask).
		WithPageMigrationPolicy(b.pageMigrationPolicy)

	// if b.enableISADebugging {
	// 	saBuilder = saBuilder.withIsaDebugging()
	// }

	// if b.enableMemTracing {
	// 	saBuilder = saBuilder.withMemTracer(b.memTracer)
	// }

	for i := 0; i < b.numShaderArray; i++ {
		saName := fmt.Sprintf("%s.SA[%d]", b.name, i)
		sa := saBuilder.Build(saName)

		b.sas = append(b.sas, sa)
	}
}

func (b *Builder) buildCoherenceDirectory() {
	if b.coherenceDirectory == 0 { // coherenceDirectory
		byteSize := b.cohDirSize
		// dir := coherence.MakeBuilder().
		dir := optdirectory.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2UnitSize(b.log2CoherenceUnitSize).
			WithWayAssociativity(8).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			// Total directory access = dirLatency + bankLatency (default 1) = 10.
			// SD branch uses 17 for a +8 cycle hierarchical-lookup penalty (=18 total).
			WithDirectoryLatency(9).
			// Phase 2 inv-emit budget: at most 8 InvReqs per output
			// channel per cycle (RDMA-bound, local-L2-bound, and — since
			// [INV-FIDELITY C4] — the dir→peer-dir fan-out lane via
			// RDMAInvPort, each counted separately). Models the directory
			// controller's outgoing-channel serialization that the
			// unbounded "drain until port full" baseline missed. The peer
			// lane previously drained unbudgeted at up to 16 InvReq/cycle.
			// Same value applied to SD/REC for fair comparison.
			WithMaxInvEmitPerCycle(8).
			WithAddressMapperType("interleaved").
			// WithToRDMA(b.rdmaEngine.RDMADataInside.AsRemote()).
			WithIdealDirectory(b.idealDirectory).
			WithFetchSingleCacheLine(true).
			WithFIFOReplacement(b.cdFifoReplacement).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask).
			// [EQUAL-DIR-CAP] match SD's inflight cap when -equal-dir-cap.
			WithMaxInflightFetch(b.dirInflightCap(128)).
			WithMaxOutgoingRemoteInflight(b.dirRemoteSubCap(-1)).
			Build(fmt.Sprintf("%s.CohDir", b.name))

		b.simulation.RegisterComponent(dir)
		b.cohDir = dir
		b.l1AddressMapper.LowModules = append(
			b.l1AddressMapper.LowModules,
			dir.GetPortByName("Top").AsRemote(),
		)
		b.rdmaLowModuleFinder.LowModules = append(
			b.rdmaLowModuleFinder.LowModules,
			dir.GetPortByName("RDMA").AsRemote(),
		)
		b.rdmaInvLowModuleFinder.LowModules = append(
			b.rdmaInvLowModuleFinder.LowModules,
			dir.GetPortByName("RDMAInv").AsRemote(),
		)

	} else if b.coherenceDirectory == 1 { // largeBlkCache
		byteSize := b.cohDirSize * 1 << b.log2CoherenceUnitSize
		// dir := coherence.MakeBuilder().
		dir := optdirectory.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize + b.log2CoherenceUnitSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2UnitSize(0).
			WithWayAssociativity(8).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			// Total = dirLatency + bankLatency (default 1) = 10. See CD branch.
			WithDirectoryLatency(9).
			// Phase 2 inv-emit budget — same value as other variants.
			WithMaxInvEmitPerCycle(8).
			WithAddressMapperType("interleaved").
			// WithToRDMA(b.rdmaEngine.RDMADataInside.AsRemote()).
			WithIdealDirectory(b.idealDirectory).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask).
			Build(fmt.Sprintf("%s.CohDir", b.name))

		b.simulation.RegisterComponent(dir)
		b.cohDir = dir
		b.l1AddressMapper.LowModules = append(
			b.l1AddressMapper.LowModules,
			dir.GetPortByName("Top").AsRemote(),
		)
		b.rdmaLowModuleFinder.LowModules = append(
			b.rdmaLowModuleFinder.LowModules,
			dir.GetPortByName("RDMA").AsRemote(),
		)
		b.rdmaInvLowModuleFinder.LowModules = append(
			b.rdmaInvLowModuleFinder.LowModules,
			dir.GetPortByName("RDMAInv").AsRemote(),
		)

	} else if b.coherenceDirectory == 2 { // superDirectory
		byteSize := b.cohDirSize
		// SD FE layout: shrink the coarser banks (all except the finest two)
		// to 1/4 of the default numSets. Default layout uses the uniform
		// doubling formula `set >> bank << i` (set = byteSize/(way*block)).
		// When -sd-fe is set, banks [0 .. numBanks-3] are divided by 4
		// (equivalent to `set >> (bank+2) << i`). Slice is nil otherwise so
		// the SD builder keeps the legacy formula.
		var sdNumSetsPerBank []int
		if b.sdFE {
			const sdWay = 8
			blockSize := 1 << b.log2CacheLineSize
			set := int(byteSize) / (sdWay * blockSize)
			sdNumSetsPerBank = make([]int, b.sdNumBanks)
			for i := 0; i < b.sdNumBanks; i++ {
				if i < b.sdNumBanks-2 {
					sdNumSetsPerBank[i] = set >> (b.sdNumBanks + 2) << i
				} else {
					sdNumSetsPerBank[i] = set >> b.sdNumBanks << i
				}
				if sdNumSetsPerBank[i] <= 0 {
					panic(fmt.Sprintf(
						"SD FE: numSetsPerBank[%d] <= 0 (set=%d numBanks=%d). "+
							"Increase -sd-byte-size or decrease -sd-num-banks.",
						i, set, b.sdNumBanks))
				}
			}
		}
		// [SD-PEER-SERVE-RESERVE] bounded peer-serve headroom = maxInflightRequest/4
		// (=64; mirrors the 3/4-1/4 ORIGIN-SPLIT convention). 0 when flag off.
		sdPeerServeReserveN := 0
		if b.sdPeerServeReserve {
			sdPeerServeReserveN = 256 / 4
		}
		dir := superdirectory.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithSDPeerServeReserve(sdPeerServeReserveN).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2NumSubEntry(b.sdLog2NumSubEntry).
			WithNumBanks(b.sdNumBanks).
			WithNumSetsPerBank(sdNumSetsPerBank). // nil → builder uses legacy formula
			WithWayAssociativity(8).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			WithBankLatency(1).
			// SD directory pipeline is +8 cycles vs other variants to model
			// the extra tag-array traversal cost of SD's multi-bank
			// hierarchical lookup. Total = dirLatency + bankLatency = 18
			// (others: 9 + 1 = 10).
			WithDirectoryLatency(17).
			// Phase A (S1): scaled inv-emit budget. SD's multi-bank
			// hierarchical lookup means a single incoming InvReq unrolls
			// into 2^(regionLen-log2BlockSize) per-line InvReqs on the
			// `sendToRemoteBottomInvQue` egress; with the previous cap=2
			// the coarsest bank's 16-line burst held remoteBottomPort
			// for ~8 cycles, HoL-blocking data fetches. cap=8 lets the
			// burst drain in 2 cycles. CD/REC stay at 2 (no multi-bank
			// unroll). See /root/.claude/plans/unit-size-gleaming-sun.md.
			WithMaxInvEmitPerCycle(8).
			// Phase A (S1): inflight caps doubled for SD because each
			// InvalidateAndUpdate trans lives ~4× longer than CD due to
			// per-line inv burst drain time. smoke test (2000x2000 iter=2)
			// confirmed completion with stall_inflight_inv → 0 and
			// stall_inflight_fetch reduced to 40M (sqlite cohDir_metrics).
			WithMaxInflightFetch(256).
			WithMaxInflightEviction(512).
			WithAddressMapperType("interleaved").
			WithFetchSingleCacheLine(true).
			WithDisableRSB(b.sdDisableRSB).
			WithDisableCBF(b.sdDisableCBF).
			WithDisableDemoteLock(b.sdDisableDemoteLock).
			WithPromoteRelaxed(b.sdPromoteRelaxed).
			WithUseRsbHintAlloc(b.sdUseRsbHintAlloc).
			WithRecordSilentEvict(b.sdRecordSilentEvict).
			WithPromoteAtEvict(b.sdPromoteAtEvict).
			WithPromoteAtEvictBiasVictim(b.sdPromoteAtEvictBiasVictim).
			WithPromoteAtEvictMultiBank(b.sdPromoteAtEvictMultiBank).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask).
			Build(fmt.Sprintf("%s.SuperDir", b.name))

		b.simulation.RegisterComponent(dir)
		b.superDir = dir
		b.l1AddressMapper.LowModules = append(
			b.l1AddressMapper.LowModules,
			dir.GetPortByName("Top").AsRemote(),
		)
		b.rdmaLowModuleFinder.LowModules = append(
			b.rdmaLowModuleFinder.LowModules,
			dir.GetPortByName("RDMA").AsRemote(),
		)
		b.rdmaInvLowModuleFinder.LowModules = append(
			b.rdmaInvLowModuleFinder.LowModules,
			dir.GetPortByName("RDMAInv").AsRemote(),
		)

	} else if b.coherenceDirectory == 3 { // REC
		byteSize := b.cohDirSize
		dir := REC.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2NumSubEntry(4).
			// WithLog2UnitSize(0).
			WithWayAssociativity(8).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			WithBankLatency(1).
			// Total = dirLatency + bankLatency = 10. See CD branch.
			WithDirectoryLatency(9).
			// Phase 2 inv-emit budget — same value as CD/SD for fair
			// cross-variant comparison.
			WithMaxInvEmitPerCycle(8).
			// [ITER1 FIX 20260605] REC outgoing-remote sub-cap DISABLED (0).
			// stencil2d empirical results:
			//   L2 outgoing cap (384) only:      sim 19.80 ms hang
			//   L2 + REC outgoing cap (96):      sim 18.69 ms hang
			// Adding the REC cap WORSENED the hang point by 1.1 ms —
			// the 96 cap over-throttled REC.bottomSender's outgoing path,
			// causing earlier saturation elsewhere. Diagnostic dump at
			// the 18.69 ms hang shows all lost-rsp counters = 0 (so the
			// hang is NOT an ID-matching issue) but L2/REC mostly empty
			// while RDMA holds 1500+ outstanding transactions — over-
			// throttling pattern. Disable; keep the L2 outgoing cap which
			// did improve over the no-fix baseline.
			WithMaxOutgoingRemoteInflight(0).
			WithAddressMapperType("interleaved").
			// WithToRDMA(b.rdmaEngine.RDMADataInside.AsRemote()).
			// WithIdealDirectory(b.idealDirectory).
			WithHalfSet(b.recHalfSet).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask).
			// [EQUAL-DIR-CAP] match SD's total inflight cap when -equal-dir-cap
			// (REC's remote sub-cap is already disabled above).
			WithMaxInflightFetch(b.dirInflightCap(128)).
			Build(fmt.Sprintf("%s.RECDir", b.name))

		b.simulation.RegisterComponent(dir)
		b.recDir = dir
		b.l1AddressMapper.LowModules = append(
			b.l1AddressMapper.LowModules,
			dir.GetPortByName("Top").AsRemote(),
		)
		// [R2 BUGFIX] Use the typed RDMADataReq port — peer-incoming
		// AccessReq from RDMA must route via RDMADataReqPort (paired with
		// rdma.RDMADataReqInside on RDMAToCohDirForDataReq). The legacy
		// RDMAPort is still allocated for backward references, but the
		// rdmaLowModuleFinder must point at the typed REQ port so
		// directconnection.forwardMany can resolve the Dst against the
		// matching ports registry. Using the legacy "RDMA" name set the
		// Dst to RECDir.RDMAPort, which is plugged into RDMAToCohDir
		// (different directconnection) and not into RDMAToCohDirForDataReq
		// → "port not found" panic when peer AccessReq arrives.
		b.rdmaLowModuleFinder.LowModules = append(
			b.rdmaLowModuleFinder.LowModules,
			dir.GetPortByName("RDMADataReq").AsRemote(),
		)
		b.rdmaInvLowModuleFinder.LowModules = append(
			b.rdmaInvLowModuleFinder.LowModules,
			dir.GetPortByName("RDMAInv").AsRemote(),
		)

	} else if b.coherenceDirectory == 5 { // MGD
		byteSize := b.cohDirSize
		dir := MGD.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithRegionSize(b.mgdRegionSize).
			WithWayAssociativity(8).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			WithBankLatency(1).
			// Total = dirLatency + bankLatency = 10. See CD branch.
			WithDirectoryLatency(9).
			WithAddressMapperType("interleaved").
			WithFetchSingleCacheLine(true).
			WithDisableRSB(b.sdDisableRSB).
			WithDisableCBF(b.sdDisableCBF).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask).
			Build(fmt.Sprintf("%s.MGDDir", b.name))

		b.simulation.RegisterComponent(dir)
		b.mgdDir = dir
		b.l1AddressMapper.LowModules = append(
			b.l1AddressMapper.LowModules,
			dir.GetPortByName("Top").AsRemote(),
		)
		b.rdmaLowModuleFinder.LowModules = append(
			b.rdmaLowModuleFinder.LowModules,
			dir.GetPortByName("RDMA").AsRemote(),
		)
		b.rdmaInvLowModuleFinder.LowModules = append(
			b.rdmaInvLowModuleFinder.LowModules,
			dir.GetPortByName("RDMAInv").AsRemote(),
		)

	} else if b.coherenceDirectory == 4 { // HMG
		byteSize := b.cohDirSize
		// dir := coherence.MakeBuilder().
		dir := optdirectory.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2UnitSize(2).
			WithFetchSingleCacheLine(true).
			WithWayAssociativity(8).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			// Total = dirLatency + bankLatency (default 1) = 10. See CD branch.
			WithDirectoryLatency(9).
			// Phase 2 inv-emit budget — same value as CD/SD/REC for
			// fair comparison. Previously omitted from this HMG branch
			// only, causing HMG to behave like Phase-2-disabled CD_2.
			WithMaxInvEmitPerCycle(8).
			WithAddressMapperType("interleaved").
			// WithToRDMA(b.rdmaEngine.RDMADataInside.AsRemote()).
			WithIdealDirectory(b.idealDirectory).
			WithFIFOReplacement(b.cdFifoReplacement).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask).
			// [EQUAL-DIR-CAP] match SD's inflight cap when -equal-dir-cap.
			WithMaxInflightFetch(b.dirInflightCap(128)).
			WithMaxOutgoingRemoteInflight(b.dirRemoteSubCap(-1)).
			Build(fmt.Sprintf("%s.HMGDir", b.name))

		b.simulation.RegisterComponent(dir)
		b.cohDir = dir
		b.l1AddressMapper.LowModules = append(
			b.l1AddressMapper.LowModules,
			dir.GetPortByName("Top").AsRemote(),
		)
		b.rdmaLowModuleFinder.LowModules = append(
			b.rdmaLowModuleFinder.LowModules,
			dir.GetPortByName("RDMA").AsRemote(),
		)
		b.rdmaInvLowModuleFinder.LowModules = append(
			b.rdmaInvLowModuleFinder.LowModules,
			dir.GetPortByName("RDMAInv").AsRemote(),
		)
	}
}

func (b *Builder) buildL2Caches() {
	if b.coherenceDirectory == 1 {
		byteSize := b.l2CacheSize / uint64(b.numMemoryBank)
		l2Builder := largeblkcache.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2UnitSize(b.log2CoherenceUnitSize).
			WithWayAssociativity(16).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			WithNumReqPerCycle(16).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask)

		for i := 0; i < b.numMemoryBank; i++ {
			cacheName := fmt.Sprintf("%s.L2Cache[%d]", b.name, i)
			l2 := l2Builder.WithInterleaving(
				1<<(b.log2MemoryBankInterleavingSize-b.log2CacheLineSize),
				b.numMemoryBank,
				i).
				WithAddressMapperType("single").
				WithRemotePorts(b.drams[i].GetPortByName("Top").AsRemote()).
				Build(cacheName)

			b.simulation.RegisterComponent(l2)
			b.largeBlkCaches = append(b.largeBlkCaches, l2)

			// b.l1AddressMapper.LowModules = append(
			b.cohDirAddressMapper.LowModules = append(
				b.cohDirAddressMapper.LowModules,
				l2.GetPortByName("Top").AsRemote(),
			)

			// b.l1AddressMapper.LowModules = append(
			b.cohDirAddressMapperForRemoteReq.LowModules = append(
				b.cohDirAddressMapperForRemoteReq.LowModules,
				l2.GetPortByName("RemoteTop").AsRemote(),
			)

			b.rdmaBottomAddressMapper.LowModules = append(
				b.rdmaBottomAddressMapper.LowModules,
				l2.GetPortByName("Bottom").AsRemote(),
			)

			// if b.enableMemTracing {
			// 	tracing.CollectTrace(l2, b.memTracer)
			// }
		}
	} else {
		byteSize := b.l2CacheSize / uint64(b.numMemoryBank)
		l2Builder := writebackcoh.MakeBuilder().
			WithEngine(b.simulation.GetEngine()).
			WithFreq(b.freq).
			WithDeviceID(int(b.gpuID)).
			WithCD8DeadlockFix(b.cd8DeadlockFix). // [CD8-DEADLOCK FIX] toggle
			WithAckReserveFix(b.sdAckReserve).    // [SD-ACK-RESERVE] toggle
			WithL2PeerEvictHeadroom(b.l2PeerEvictHeadroom). // [L2-PEER-EVICT-HEADROOM] toggle
			WithLog2BlockSize(b.log2CacheLineSize).
			WithLog2PageSize(b.log2PageSize).
			WithLog2UnitSize(b.log2CoherenceUnitSize).
			WithWayAssociativity(16).
			WithByteSize(byteSize).
			WithNumMSHREntry(64).
			// L2 commits-per-cycle: reduced from 16 to 4 to bring the
			// directory/bank arbitration closer to a real L2's port
			// count. With the unified-budget commit-only loop in
			// directorystage.processTransaction, this is the actual
			// max number of (read|write|inv) commits per cycle that
			// share remote+local arbitration. 2 was too tight for
			// SuperDirectory variant (deadlock under PTE invalidation
			// pressure during page migration); 4 leaves enough
			// headroom while still significantly below the original 16.
			// CD-only experiment: temporarily reduced to 2 to test if
			// inv-message processing becomes the bottleneck (stencil2d
			// CD_0..8 sweep with iter=10).
			//
			// Phase A/B post-mortem (2026-06-06): with NumReqPerCycle=2,
			// the L2 remoteTopPort.incomingBuf cap = 2*2 = 4 and the
			// topparser/dirStage process at 2 msgs/cycle. SD with
			// MaxInvEmitPerCycle=8 saturates this immediately — that's
			// why Phase A's cap=8 (-91% stall_inflight_fetch but kernel
			// time unchanged) and Phase B's port split (-3% port_busy)
			// both failed to move kernel_time. Restoring NumReqPerCycle=4
			// returns to the SD-safe original value documented above and
			// removes the L2 ingress as the dominant bottleneck.
			WithNumReqPerCycle(4).
			// [INV-FIDELITY] Inv-cost model (writebackcoh, shared by
			// CD/SD/REC/HMG variants). All InvReqs traverse a dedicated
			// 4-wide invPipeline of dirLatency+snoopLatency stages, but
			// probes contend with demand accesses at BOTH shared
			// arbitration points of the directory stage:
			//   - admission: one numReqPerCycle(4) token pool per cycle
			//     shared by inv/remote/local pipeline entry (the
			//     tag-array port model, C2);
			//   - commit: one numReqPerCycle(4) slot budget per cycle,
			//     where an inv commit costs invCostInSlots=2 (state-bit
			//     write occupancy), i.e. 1 inv displaces 2 of 4 demand
			//     commits. Wasted invs pay the same traversal as
			//     productive ones (hit/miss-agnostic floor); an inv that
			//     kills a DIRTY line additionally produces a real local
			//     victim writeback through the writeBuffer/DRAM path (C3).
			//
			// dirStage ticks exactly ONCE per cycle (C1) — stages are
			// real cycles and the commit budget is a true 4 slots/cycle.
			// (Before C1 it was ticked numReqPerCycle× per cycle, which
			// quartered the latencies below and inflated commit bandwidth
			// to 16/cycle, making the inv slot cost non-binding.)
			//
			// L2 hit latency: dirLatency (16, NoC routing + tag-array
			// lookup) + bankLatency (184, data-array pipeline including
			// ECC) = 200 cycles total. Matches NVIDIA A100 L2 hit
			// (~190-210) and AMD CDNA2 L2 hit (~270 / 1.35× = ~200 at
			// our 1 GHz vs their 1.35 GHz).
			// Inv handling latency: dirLatency (16, shared tag access) +
			// snoopLatency (-inv-extra-latency, default 8: state-bit
			// write + snoop rsp generation) = 24 cycles, the low end of
			// realistic per-probe cost.
			WithDirectoryLatency(16).
			WithSnoopLatency(b.invExtraLatency).
			WithBankLatency(184).
			// [OUTGOING-REMOTE CAP FIX] Cap per-L2 outgoing remote
			// evictions (pending+inflight) at 384, leaving 640 wB
			// headroom (out of 1024) for incoming-write-triggered
			// evictions at the receiver side. With existing
			// maxInflightEviction=128, the pending portion caps at
			// 256, so backpressure stays on sender's dirStage
			// long before wB reaches the full-cap that would HoL-
			// block the receiver's incoming WriteReq path.
			WithMaxOutgoingRemotePending(384).
			WithReadMask(b.readMask).
			WithDirtyMask(b.dirtyMask)

		for i := 0; i < b.numMemoryBank; i++ {
			cacheName := fmt.Sprintf("%s.L2Cache[%d]", b.name, i)
			l2 := l2Builder.WithInterleaving(
				1<<(b.log2MemoryBankInterleavingSize-b.log2CacheLineSize),
				b.numMemoryBank,
				i).
				WithAddressMapperType("single").
				WithRemotePorts(b.drams[i].GetPortByName("Top").AsRemote()).
				Build(cacheName)

			b.simulation.RegisterComponent(l2)
			b.l2Caches = append(b.l2Caches, l2)

			// b.l1AddressMapper.LowModules = append(
			b.cohDirAddressMapper.LowModules = append(
				b.cohDirAddressMapper.LowModules,
				l2.GetPortByName("Top").AsRemote(),
			)

			// b.l1AddressMapper.LowModules = append(
			b.cohDirAddressMapperForRemoteReq.LowModules = append(
				b.cohDirAddressMapperForRemoteReq.LowModules,
				l2.GetPortByName("RemoteTop").AsRemote(),
			)

			b.rdmaBottomAddressMapper.LowModules = append(
				b.rdmaBottomAddressMapper.LowModules,
				l2.GetPortByName("Bottom").AsRemote(),
			)

			// if b.enableMemTracing {
			// 	tracing.CollectTrace(l2, b.memTracer)
			// }
		}
	}
}

func (b *Builder) buildDRAMControllers() {
	// REC paper baseline: HBM, 1 TB/s aggregate (Table 2). Use the akita
	// dram timing model instead of idealmemcontroller so coherence/RDMA
	// traffic actually sees DRAM queueing.
	memCtrlBuilder := b.createDramControllerBuilder()

	for i := 0; i < b.numMemoryBank; i++ {
		dramName := fmt.Sprintf("%s.DRAM[%d]", b.name, i)
		dram := memCtrlBuilder.Build(dramName)
		b.simulation.RegisterComponent(dram)
		b.drams = append(b.drams, dram)
	}
}

func (b *Builder) createDramControllerBuilder() dram.Builder {
	memBankSize := 4 * mem.GB / uint64(b.numMemoryBank)
	if 4*mem.GB%uint64(b.numMemoryBank) != 0 {
		panic("GPU memory size is not a multiple of the number of memory banks")
	}

	dramCol := 64
	dramRow := 16384
	dramDeviceWidth := 128
	dramBankSize := dramCol * dramRow * dramDeviceWidth
	dramBank := 4
	dramBankGroup := 4
	dramBusWidth := 256
	dramDevicePerRank := dramBusWidth / dramDeviceWidth
	dramRankSize := dramBankSize * dramDevicePerRank * dramBank
	dramRank := int(memBankSize * 8 / uint64(dramRankSize))

	// REC paper baseline targets 1 TB/s aggregate. With 16 controllers ×
	// 256-bit bus × burstLength 4, 500 MHz yields ~512 GB/s; bumping to
	// 1 GHz lands close to 1 TB/s.
	memCtrlBuilder := dram.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(1 * sim.GHz).
		WithProtocol(dram.HBM).
		WithBurstLength(4).
		WithDeviceWidth(dramDeviceWidth).
		WithBusWidth(dramBusWidth).
		WithNumChannel(1).
		WithNumRank(dramRank).
		WithNumBankGroup(dramBankGroup).
		WithNumBank(dramBank).
		WithNumCol(dramCol).
		WithNumRow(dramRow).
		WithCommandQueueSize(8).
		WithTransactionQueueSize(32).
		WithTCL(7).
		WithTCWL(2).
		WithTRCDRD(7).
		WithTRCDWR(7).
		WithTRP(7).
		WithTRAS(17).
		WithTREFI(1950).
		WithTRRDS(2).
		WithTRRDL(3).
		WithTWTRS(3).
		WithTWTRL(4).
		WithTWR(8).
		WithTCCDS(1).
		WithTCCDL(1).
		WithTRTRS(0).
		WithTRTP(3).
		WithTPPD(2)

	if b.globalStorage != nil {
		memCtrlBuilder = memCtrlBuilder.WithGlobalStorage(b.globalStorage)
	}

	return memCtrlBuilder
}

func (b *Builder) buildRDMAEngine() {
	name := fmt.Sprintf("%s.RDMA", b.name)
	b.rdmaEngine = rdma.MakeBuilder().
		WithDeviceID(b.gpuID).
		WithEngine(b.simulation.GetEngine()).
		WithVisTracer(b.simulation.GetVisTracer()).
		WithFreq(1 * sim.GHz).
		WithBufferSize(4096).
		// WithLocalModules(b.l1AddressMapper).
		WithLocalModules(b.rdmaLowModuleFinder).
		WithAccessCounter(b.accessCounter).
		WithDirtyMask(b.dirtyMask).
		WithReadMask(b.readMask).
		WithLog2CacheLineSize(b.log2CacheLineSize).
		WithLog2PageSize(b.log2PageSize).
		Build(name)

	b.rdmaEngine.RemoteRDMAAddressTable = b.rdmaAddressMapper

	b.simulation.RegisterComponent(b.rdmaEngine)
}

func (b *Builder) buildPageMigrationController() {
	b.pmc = pagemigrationcontroller.NewPageMigrationController(
		fmt.Sprintf("%s.PMC", b.name),
		b.gpuID,
		b.simulation.GetEngine(),
		b.pmcAddressMapper,
		nil)

	b.simulation.RegisterComponent(b.pmc)
}

func (b *Builder) buildDMAEngine() {
	b.dmaEngine = cp.NewDMAEngine(
		fmt.Sprintf("%s.DMA", b.name),
		b.simulation.GetEngine(),
		nil)

	b.simulation.RegisterComponent(b.dmaEngine)
}

func (b *Builder) buildCP() {
	b.cp = cp.MakeBuilder().
		WithDeviceID(uint32(b.gpuID)).
		WithEngine(b.simulation.GetEngine()).
		WithVisTracer(b.simulation.GetVisTracer()).
		WithFreq(b.freq).
		WithMonitor(b.simulation.GetMonitor()).
		WithDriver(b.driver).
		WithPageMigrationPolicy(b.pageMigrationPolicy).
		Build(b.name + ".CommandProcessor")

	b.simulation.RegisterComponent(b.cp)

	b.buildDMAEngine()
	b.buildRDMAEngine()
	b.buildPageMigrationController()
}

func (b *Builder) buildL2TLB() {
	numWays := 64
	builder := tlb.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		WithNumWays(numWays).
		WithNumSets(int(b.dramSize / (1 << b.log2PageSize) / uint64(numWays))).
		WithNumMSHREntry(64).
		WithNumReqPerCycle(1024).
		// WithPageSize(1 << b.log2PageSize).
		WithLog2PageSize(b.log2PageSize).
		WithLowModule(b.gmmu.GetPortByName("Top").AsRemote()).
		WithPageMigrationPolicy(b.pageMigrationPolicy).
		WithAccessCounter(b.accessCounter)
		// WithAddressMapper(&mem.SinglePortMapper{
		// 	Port: b.gmmu.GetPortByName("Top").AsRemote(),
		// })

	l2TLB := builder.Build(fmt.Sprintf("%s.L2TLB", b.name))

	b.simulation.RegisterComponent(l2TLB)
	b.l2TLBs = append(b.l2TLBs, l2TLB)

	b.l1TLBAddressMapper.Port = l2TLB.GetPortByName("Top").AsRemote()
}

func (b *Builder) buildGMMU() {
	builder := gmmu.MakeBuilder().
		WithEngine(b.simulation.GetEngine()).
		WithFreq(b.freq).
		WithLog2PageSize(b.log2PageSize).
		WithPageTable(vm.NewLevelPageTable(b.log2PageSize, 6, fmt.Sprintf("GMMU[%d].PT", b.gpuID))).
		// WithMaxNumReqInFlight(16).
		WithMaxNumReqInFlight(8).
		WithPageWalkingLatency(100).
		WithDeviceID(b.gpuID).
		WithAccessCounter(b.accessCounter).
		WithLowModule(b.mmu.GetPortByName("Top").AsRemote()).
		WithPageTableLogSize(20).
		WithDirtyMask(b.dirtyMask).
		WithReadMask(b.readMask).
		WithPageMigrationPolicy(b.pageMigrationPolicy)

	b.gmmu = builder.Build(fmt.Sprintf("%s.GMMU", b.name))

	b.simulation.RegisterComponent(b.gmmu)
}

func (b *Builder) numCU() int {
	return b.numCUPerShaderArray * b.numShaderArray
}
