package driver

import (
	"fmt"
	"log"
	"reflect"
	"runtime/debug"
	"sync"

	"github.com/rs/xid"
	"github.com/sarchlab/akita/v4/mem/mem"
	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/tracing"
	"github.com/sarchlab/mgpusim/v4/amd/driver/internal"
	"github.com/sarchlab/mgpusim/v4/amd/kernels"
	"github.com/sarchlab/mgpusim/v4/amd/protocol"
	"github.com/tebeka/atexit"
)

// Driver is an Akita component that controls the simulated GPUs
type Driver struct {
	sim.TickingComponent
	sim.HookableBase

	memAllocator  internal.MemoryAllocator
	distributor   distributor
	globalStorage *mem.Storage

	GPUs        []sim.Port
	devices     []*internal.Device
	pageTable   vm.LevelPageTable
	middlewares []Middleware

	requestsToSend []sim.Msg

	contextMutex sync.Mutex
	contexts     []*Context

	mmuPort sim.Port
	gpuPort sim.Port

	driverStopped      chan bool
	enqueueSignal      chan bool
	engineMutex        sync.Mutex
	engineRunning      bool
	engineRunningMutex sync.Mutex
	simulationID       string

	Log2PageSize      uint64
	Log2CacheLineSize uint64

	currentPageMigrationReq         *vm.PageMigrationReqToDriver
	toSendToMMU                     *vm.PageMigrationRspFromDriver
	migrationReqToSendToCP          []*protocol.PageMigrationReqToCP
	invalidationReqToSendToCP       []*vm.PTEInvalidationReq
	isCurrentlyHandlingMigrationReq bool
	numRDMADrainACK                 uint64
	numRDMARestartACK               uint64
	numShootDownACK                 uint64
	numRestartACK                   uint64
	numPagesMigratingACK            uint64
	numInvalidationACK              uint64
	isCurrentlyMigratingOnePage     bool

	RemotePMCPorts []sim.Port
	shootDownReqID map[uint64]string

	DirtyMask           []map[vm.PID]map[uint64][]uint8
	ReadMask            []map[vm.PID]map[uint64][]uint8
	pageMigrationPolicy uint64

	returnValue       []string
	runAsyncOperating bool
	printOnce         bool
}

// Run starts a new threads that handles all commands in the command queues
func (d *Driver) Run() {
	d.logSimulationStart()
	go d.runAsync()
}

// Terminate stops the driver thread execution.
func (d *Driver) Terminate() {
	d.driverStopped <- true
	d.logSimulationTerminate()
}

func (d *Driver) logSimulationStart() {
	d.simulationID = xid.New().String()
	tracing.StartTask(
		d.simulationID,
		"",
		d,
		"Simulation", "Simulation",
		nil,
	)
}

func (d *Driver) logSimulationTerminate() {
	tracing.EndTask(d.simulationID, d)
}

func (d *Driver) runAsync() {
	d.runAsyncOperating = false
	for {
		select {
		case <-d.driverStopped:
			// 해당 채널로 신호가 들어오면 return을 통해 반복문 탈출
			d.runAsyncOperating = false
			return
		case <-d.enqueueSignal:
			// 새로운 작업을 큐에 추가
			d.engineMutex.Lock()
			d.Engine.Pause()
			d.TickLater()
			fmt.Printf("[Driver DEBUF 1]\tqueue len: %d, 2ndQueue len: %d\n",
				d.TickingComponent.Engine.(*sim.SerialEngine).GetQueue().Len(), d.TickingComponent.Engine.(*sim.SerialEngine).GetSecondaryQueue().Len())
			d.Engine.Continue()
			d.engineMutex.Unlock()
			// 상태를 변경하기 전에 engine을 일시정지

			d.engineRunningMutex.Lock()
			if d.engineRunning {
				fmt.Printf("[Driver DEBUF 2]\tEngine is running, skip d.runEngine\n")
				d.engineRunningMutex.Unlock()
				continue
			}

			d.engineRunning = true
			fmt.Printf("[Driver DEBUF 3]\tqueue len: %d, 2ndQueue len: %d\n",
				d.TickingComponent.Engine.(*sim.SerialEngine).GetQueue().Len(), d.TickingComponent.Engine.(*sim.SerialEngine).GetSecondaryQueue().Len())
			go d.runEngine()
			d.engineRunningMutex.Unlock()
		}

		d.runAsyncOperating = true
	}
}

func (d *Driver) runEngine() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic: %v", r)
			debug.PrintStack()
			atexit.Exit(1)
		}
	}()

	d.engineMutex.Lock()
	defer d.engineMutex.Unlock()
	fmt.Printf("[Driver DEBUF 4]\tqueue len: %d, 2ndQueue len: %d\n",
		d.TickingComponent.Engine.(*sim.SerialEngine).GetQueue().Len(), d.TickingComponent.Engine.(*sim.SerialEngine).GetSecondaryQueue().Len())
	err := d.Engine.Run()
	if err != nil {
		panic(err)
	}

	d.engineRunningMutex.Lock()
	fmt.Printf("[Driver DEBUF 5]\tEngine stops running\n")
	d.engineRunning = false
	d.engineRunningMutex.Unlock()
}

// DeviceProperties defines the properties of a device
type DeviceProperties struct {
	CUCount  int
	DRAMSize uint64
}

// RegisterGPU tells the driver about the existence of a GPU
func (d *Driver) RegisterGPU(
	commandProcessorPort sim.Port,
	pageMigrationController sim.Port,
	properties DeviceProperties,
) {
	d.GPUs = append(d.GPUs, commandProcessorPort)
	d.RemotePMCPorts = append(d.RemotePMCPorts, pageMigrationController)

	gpuDevice := &internal.Device{
		ID:       len(d.GPUs),
		Type:     internal.DeviceTypeGPU,
		MemState: internal.NewDeviceMemoryState(d.Log2PageSize),
		Properties: internal.DeviceProperties{
			CUCount:  properties.CUCount,
			DRAMSize: properties.DRAMSize,
		},
	}
	gpuDevice.SetTotalMemSize(properties.DRAMSize)
	d.memAllocator.RegisterDevice(gpuDevice)

	d.devices = append(d.devices, gpuDevice)
}

// Tick ticks
func (d *Driver) Tick() bool {
	madeProgress := false

	madeProgress = d.sendToGPUs() || madeProgress
	madeProgress = d.sendToMMU() || madeProgress
	madeProgress = d.sendMigrationReqToCP() || madeProgress
	madeProgress = d.sendInvalidationReqToCP() || madeProgress

	for _, mw := range d.middlewares {
		madeProgress = mw.Tick() || madeProgress
	}

	madeProgress = d.processReturnReq() || madeProgress
	madeProgress = d.processNewCommand() || madeProgress
	madeProgress = d.parseFromMMU() || madeProgress

	return madeProgress
}

func (d *Driver) sendToGPUs() bool {
	d.returnValue[0] = "true"
	if len(d.requestsToSend) == 0 {
		d.returnValue[0] = "false"
		return false
	}

	req := d.requestsToSend[0]
	err := d.gpuPort.Send(req)
	if err == nil {
		d.requestsToSend = d.requestsToSend[1:]

		switch req := req.(type) {
		case *protocol.RDMARestartCmdFromDriver:
			fmt.Printf("[Driver]\tSuccess to send RDMA restart cmd. to %s\n", req.Dst)
		case *protocol.MemCopyD2HReq:
			fmt.Printf("[Driver]\tSuccess to send mem copy D2H req. to %s\n", req.Dst)
		}
		return true
	}

	return false
}

//nolint:gocyclo
func (d *Driver) processReturnReq() bool {
	d.returnValue[3] = "true"
	req := d.gpuPort.PeekIncoming()
	if req == nil {
		d.returnValue[3] = "false"
		return false
	}

	switch req := req.(type) {
	case *protocol.LaunchKernelRsp:
		d.gpuPort.RetrieveIncoming()
		return d.processLaunchKernelReturn(req)
	case *protocol.RDMADrainRspToDriver:
		d.gpuPort.RetrieveIncoming()
		return d.processRDMADrainRsp(req)
	case *protocol.ShootDownCompleteRsp:
		d.gpuPort.RetrieveIncoming()
		return d.processShootdownCompleteRsp(req)
	case *protocol.PageMigrationRspToDriver:
		d.gpuPort.RetrieveIncoming()
		return d.processPageMigrationRspFromCP(req)
	case *vm.PTEInvalidationRsp:
		d.gpuPort.RetrieveIncoming()
		return d.processInvalidationRspFromCP(req)
	case *protocol.RDMARestartRspToDriver:
		d.gpuPort.RetrieveIncoming()
		return d.processRDMARestartRspToDriver(req)
	case *protocol.GPURestartRsp:
		d.gpuPort.RetrieveIncoming()
		return d.handleGPURestartRsp(req)
	}

	return false
}

func (d *Driver) processNewCommand() bool {
	madeProgress := false

	d.contextMutex.Lock()
	for _, ctx := range d.contexts {
		madeProgress = d.processNewCommandFromContext(ctx) || madeProgress
	}
	d.contextMutex.Unlock()

	if madeProgress {
		d.returnValue[26] = "true"
	} else {
		d.returnValue[26] = "false"
	}
	return madeProgress
}

func (d *Driver) processNewCommandFromContext(
	ctx *Context,
) bool {
	madeProgress := false
	ctx.queueMutex.Lock()
	for _, q := range ctx.queues {
		madeProgress = d.processNewCommandFromCmdQueue(q) || madeProgress
	}
	ctx.queueMutex.Unlock()

	if madeProgress {
		d.returnValue[25] = "true"
	} else {
		d.returnValue[25] = "false"
	}
	return madeProgress
}

func (d *Driver) processNewCommandFromCmdQueue(
	q *CommandQueue,
) bool {
	d.returnValue[5] = "true"
	d.returnValue[6] = "true"

	if q.NumCommand() == 0 {
		d.returnValue[5] = "false"
		return false
	}

	if q.IsRunning {
		d.returnValue[6] = "false"
		return false
	}

	return d.processOneCommand(q)
}

func (d *Driver) processOneCommand(
	cmdQueue *CommandQueue,
) bool {
	cmd := cmdQueue.Peek()

	switch cmd := cmd.(type) {
	case *LaunchKernelCommand:
		d.logCmdStart(cmd)
		return d.processLaunchKernelCommand(cmd, cmdQueue)
	case *NoopCommand:
		d.logCmdStart(cmd)
		return d.processNoopCommand(cmd, cmdQueue)
	case *LaunchUnifiedMultiGPUKernelCommand:
		d.logCmdStart(cmd)
		return d.processUnifiedMultiGPULaunchKernelCommand(cmd, cmdQueue)
	default:
		return d.processCommandWithMiddleware(cmd, cmdQueue)
	}
}

func (d *Driver) processCommandWithMiddleware(
	cmd Command,
	cmdQueue *CommandQueue,
) bool {
	d.returnValue[8] = "true"

	for _, m := range d.middlewares {
		processed := m.ProcessCommand(cmd, cmdQueue)

		if processed {
			d.logCmdStart(cmd)
			return true
		}
	}

	d.returnValue[8] = "false"
	return false
}

func (d *Driver) logCmdStart(cmd Command) {
	tracing.StartTask(
		cmd.GetID(),
		d.simulationID,
		d,
		"Driver Command",
		reflect.TypeOf(cmd).String(),
		nil,
	)
}

func (d *Driver) logCmdComplete(cmd Command) {
	tracing.EndTask(cmd.GetID(), d)
}

func (d *Driver) processNoopCommand(
	cmd *NoopCommand,
	queue *CommandQueue,
) bool {
	queue.Dequeue()
	return true
}

func (d *Driver) logTaskToGPUInitiate(
	cmd Command,
	req sim.Msg,
) {
	tracing.TraceReqInitiate(req, d, cmd.GetID())
}

func (d *Driver) logTaskToGPUClear(
	req sim.Msg,
) {
	tracing.TraceReqFinalize(req, d)
}

func (d *Driver) processLaunchKernelCommand(
	cmd *LaunchKernelCommand,
	queue *CommandQueue,
) bool {
	req := protocol.NewLaunchKernelReq(d.gpuPort,
		d.GPUs[queue.GPUID-1])
	req.PID = queue.Context.pid
	req.HsaCo = cmd.CodeObject

	req.Packet = cmd.Packet
	req.PacketAddress = uint64(cmd.DPacket)

	queue.IsRunning = true
	cmd.Reqs = append(cmd.Reqs, req)

	d.requestsToSend = append(d.requestsToSend, req)

	queue.Context.l2Dirty = true
	queue.Context.markAllBuffersDirty()

	d.logTaskToGPUInitiate(cmd, req)

	return true
}

func (d *Driver) processUnifiedMultiGPULaunchKernelCommand(
	cmd *LaunchUnifiedMultiGPUKernelCommand,
	queue *CommandQueue,
) bool {
	d.returnValue[10] = "true"
	wgDist := d.distributeWGToGPUs(queue, cmd)

	dev := d.devices[queue.GPUID]
	for i, gpuID := range dev.UnifiedGPUIDs {
		if wgDist[i+1]-wgDist[i] == 0 {
			continue
		}

		req := protocol.NewLaunchKernelReq(d.gpuPort, d.GPUs[gpuID-1])
		req.PID = queue.Context.pid
		req.HsaCo = cmd.CodeObject
		req.Packet = cmd.PacketArray[i]
		req.PacketAddress = uint64(cmd.DPacketArray[i])

		currentGPUIndex := i
		req.WGFilter = func(
			pkt *kernels.HsaKernelDispatchPacket,
			wg *kernels.WorkGroup,
		) bool {
			numWGX := (pkt.GridSizeX-1)/uint32(pkt.WorkgroupSizeX) + 1
			numWGY := (pkt.GridSizeY-1)/uint32(pkt.WorkgroupSizeY) + 1

			flattenedID :=
				wg.IDZ*int(numWGX)*int(numWGY) +
					wg.IDY*int(numWGX) +
					wg.IDX

			if flattenedID >= wgDist[currentGPUIndex] &&
				flattenedID < wgDist[currentGPUIndex+1] {
				return true
			}

			d.returnValue[10] = "false"
			return false
		}

		queue.IsRunning = true
		cmd.Reqs = append(cmd.Reqs, req)

		d.requestsToSend = append(d.requestsToSend, req)

		queue.Context.l2Dirty = true
		queue.Context.markAllBuffersDirty()

		d.logTaskToGPUInitiate(cmd, req)
	}

	return true
}

func (d *Driver) distributeWGToGPUs(
	queue *CommandQueue,
	cmd *LaunchUnifiedMultiGPUKernelCommand,
) []int {
	dev := d.devices[queue.GPUID]
	actualGPUs := dev.UnifiedGPUIDs
	wgAllocated := 0
	wgDist := make([]int, len(actualGPUs)+1)

	totalCUCount := 0
	for i, devID := range actualGPUs {
		if i == 0 {
			continue
		}
		totalCUCount += d.devices[devID].Properties.CUCount
	}

	numWGX := (cmd.PacketArray[0].GridSizeX-1)/uint32(cmd.PacketArray[0].WorkgroupSizeX) + 1
	numWGY := (cmd.PacketArray[0].GridSizeY-1)/uint32(cmd.PacketArray[0].WorkgroupSizeY) + 1
	numWGZ := (cmd.PacketArray[0].GridSizeZ-1)/uint32(cmd.PacketArray[0].WorkgroupSizeZ) + 1
	totalWGCount := int(numWGX * numWGY * numWGZ)
	wgPerCU := (totalWGCount-1)/totalCUCount + 1

	for i, devID := range actualGPUs {
		if i == 0 {
			continue
		}
		cuCount := d.devices[devID].Properties.CUCount
		wgToAllocate := cuCount * wgPerCU
		wgDist[i+1] = wgAllocated + wgToAllocate
		wgAllocated += wgToAllocate
	}

	if wgAllocated < totalWGCount {
		panic("not all wg allocated")
	}

	// fmt.Printf("[Driver]\tUnified Multi-GPU Kernel Launch WG Distribution: %v\n", wgDist)
	return wgDist
}

func (d *Driver) processLaunchKernelReturn(
	rsp *protocol.LaunchKernelRsp,
) bool {
	req, cmd, cmdQueue := d.findCommandByReqID(rsp.RspTo)
	cmd.RemoveReq(req)

	d.logTaskToGPUClear(req)

	if len(cmd.GetReqs()) == 0 {
		cmdQueue.IsRunning = false
		cmdQueue.Dequeue()

		d.logCmdComplete(cmd)
	}

	return true
}

func (d *Driver) findCommandByReq(req sim.Msg) (Command, *CommandQueue) {
	d.contextMutex.Lock()
	defer d.contextMutex.Unlock()

	for _, ctx := range d.contexts {
		ctx.queueMutex.Lock()
		for _, q := range ctx.queues {
			cmd := q.Peek()
			if cmd == nil {
				continue
			}

			reqs := cmd.GetReqs()
			for _, r := range reqs {
				if r == req {
					ctx.queueMutex.Unlock()
					return cmd, q
				}
			}
		}
		ctx.queueMutex.Unlock()
	}

	panic("cannot find command")
}

func (d *Driver) findCommandByReqID(reqID string) (
	sim.Msg,
	Command,
	*CommandQueue,
) {
	d.contextMutex.Lock()
	defer d.contextMutex.Unlock()

	for _, ctx := range d.contexts {
		ctx.queueMutex.Lock()

		for _, q := range ctx.queues {
			cmd := q.Peek()
			if cmd == nil {
				continue
			}

			reqs := cmd.GetReqs()
			for _, r := range reqs {
				if r.Meta().ID == reqID {
					ctx.queueMutex.Unlock()
					return r, cmd, q
				}
			}
		}

		ctx.queueMutex.Unlock()
	}

	panic("cannot find command")
}

func (d *Driver) parseFromMMU() bool {
	d.returnValue[12] = "true"
	d.returnValue[13] = "true"

	req := d.mmuPort.PeekIncoming()
	if req == nil {
		d.returnValue[12] = "false"
		return false
	}

	switch req := req.(type) {
	case *vm.PageMigrationReqToDriver:
		if d.isCurrentlyHandlingMigrationReq {
			d.returnValue[13] = "false"
			return false
		}

		d.currentPageMigrationReq = req
		d.isCurrentlyHandlingMigrationReq = true
		d.initiateShootDown()
		// d.initiateRDMADrain()
	case *vm.RemoveRequestRspFromMMU:
		d.processRemoveRequestRspFromMMU(req)
	default:
		log.Panicf("Driver cannot handle request of type %s",
			reflect.TypeOf(req))
	}

	d.mmuPort.RetrieveIncoming()
	return true
}

func (d *Driver) initiateShootDown() bool {

	kind := "Page Migration"
	if d.currentPageMigrationReq.ForDuplication {
		kind = "Page Duplication"
	} else if d.currentPageMigrationReq.ForInvalidation {
		kind = "Page Invalidation"
	}
	tracing.StartTask(
		d.currentPageMigrationReq.Meta().ID,
		d.simulationID,
		d,
		kind,
		fmt.Sprintf("From GPU %d to GPU %d, VA %d",
			d.currentPageMigrationReq.CurrPageHostGPU, d.currentPageMigrationReq.RequestingDevice,
			d.currentPageMigrationReq.MigrationInfo.GPUReqToVAddrMap[d.currentPageMigrationReq.RequestingDevice][0]),
		nil,
	)

	va := d.currentPageMigrationReq.MigrationInfo.GPUReqToVAddrMap[d.currentPageMigrationReq.RequestingDevice][0]
	fmt.Printf("\n======================================================================================\n")
	fmt.Printf("[Driver]\tStart %s VPN %x from GPU %d to GPU %d\n",
		kind,
		va,
		d.currentPageMigrationReq.CurrPageHostGPU,
		d.currentPageMigrationReq.RequestingDevice,
	)
	// for i, list := range d.DirtyMask {
	// 	fmt.Printf("\t\tDirtyMask GPU %d: %v\n", i+1, list[d.currentPageMigrationReq.PID][va>>d.Log2PageSize])
	// }
	// for i, list := range d.ReadMask {
	// 	fmt.Printf("\t\tReadMask  GPU %d: %v\n", i+1, list[d.currentPageMigrationReq.PID][va>>d.Log2PageSize])
	// }
	fmt.Printf("======================================================================================\n\n")

	if d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1] == nil || d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1] == nil {
		d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1] = make(map[vm.PID]map[uint64][]uint8)
		d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1] = make(map[vm.PID]map[uint64][]uint8)
	}
	if d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] == nil || d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] == nil {
		d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] = make(map[uint64][]uint8)
		d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] = make(map[uint64][]uint8)
	}

	if kind != "Page Duplication" {
		d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID][va>>d.Log2PageSize] = nil
		d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID][va>>d.Log2PageSize] = nil
	}

	d.sendShootDownReqs()

	return true
}

func (d *Driver) sendShootDownReqs() bool {
	vAddr := make([]uint64, 0)
	migrationInfo := d.currentPageMigrationReq.MigrationInfo

	numReqsGPUInMap := 0
	for i := 1; i < d.GetNumGPUs()+1; i++ {
		pages, found := migrationInfo.GPUReqToVAddrMap[uint64(i)]

		if found {
			numReqsGPUInMap++
			for j := 0; j < len(pages); j++ {
				vAddr = append(vAddr, pages[j])
			}
		}
	}

	fmt.Printf("[Driver]\tCurrent Page Host GPU: %d\n", d.currentPageMigrationReq.CurrPageHostGPU)
	if d.currentPageMigrationReq.CurrPageHostGPU == 1 {
		fmt.Printf("[Driver]\tSkip GPU Shoot Down Because of 1st touch\n")
		d.numShootDownACK++
		return d.processShootdownCompleteRsp(nil)
	}

	accessingGPUs := d.currentPageMigrationReq.CurrAccessingGPUs
	pid := d.currentPageMigrationReq.PID

	fmt.Printf("[Driver]\tInitiate GPU Shoot Down: ")
	for i, _ := range d.GPUs {
		// remote caching이 추가됨에 따라 모든 GPU가 멈춰야 함..
		// 단, GPU 1에서 이동하는 경우는 멈춤 x

		if i == 0 { // GPU 1은 dummy라서 shootdown 필요 없음
			continue
		}

		toShootdownGPU := uint64(i)
		shootDownReq := protocol.NewShootdownCommand(
			d.gpuPort, d.GPUs[toShootdownGPU],
			vAddr, pid)
		for _, j := range accessingGPUs {
			if uint64(i) == j {
				shootDownReq.ToAccessingGPU = true
				break
			}
		}

		d.requestsToSend = append(d.requestsToSend, shootDownReq)
		d.numShootDownACK++

		kind := "Page Migration"
		if d.currentPageMigrationReq.ForDuplication {
			kind = "Page Duplication"
			shootDownReq.ForDuplication = true
		} else if d.currentPageMigrationReq.ForInvalidation {
			kind = "Page Invalidation"
			shootDownReq.ForInvalidation = true
		}
		tracing.StartTask(
			shootDownReq.Meta().ID,
			d.currentPageMigrationReq.Meta().ID,
			d,
			kind,
			fmt.Sprintf("Shootdown GPU %d", toShootdownGPU+1),
			nil,
		)
		d.shootDownReqID[toShootdownGPU] = shootDownReq.Meta().ID

		va := d.currentPageMigrationReq.MigrationInfo.GPUReqToVAddrMap[d.currentPageMigrationReq.RequestingDevice][0]

		if d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1] == nil || d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1] == nil {
			d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1] = make(map[vm.PID]map[uint64][]uint8)
			d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1] = make(map[vm.PID]map[uint64][]uint8)
		}
		if d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] == nil || d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] == nil {
			d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] = make(map[uint64][]uint8)
			d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID] = make(map[uint64][]uint8)
		}
		d.DirtyMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID][va>>d.Log2PageSize] = nil
		d.ReadMask[d.currentPageMigrationReq.RequestingDevice-1][d.currentPageMigrationReq.PID][va>>d.Log2PageSize] = nil

		if d.DirtyMask[toShootdownGPU] == nil || d.ReadMask[toShootdownGPU] == nil {
			d.DirtyMask[toShootdownGPU] = make(map[vm.PID]map[uint64][]uint8)
			d.ReadMask[toShootdownGPU] = make(map[vm.PID]map[uint64][]uint8)
		}
		if d.DirtyMask[toShootdownGPU][d.currentPageMigrationReq.PID] == nil || d.ReadMask[toShootdownGPU][d.currentPageMigrationReq.PID] == nil {
			d.DirtyMask[toShootdownGPU][d.currentPageMigrationReq.PID] = make(map[uint64][]uint8)
			d.ReadMask[toShootdownGPU][d.currentPageMigrationReq.PID] = make(map[uint64][]uint8)
		}

		if kind != "Page Duplication" {
			d.DirtyMask[toShootdownGPU][d.currentPageMigrationReq.PID][va>>d.Log2PageSize] = nil
			d.ReadMask[toShootdownGPU][d.currentPageMigrationReq.PID][va>>d.Log2PageSize] = nil
		}

		fmt.Printf("GPU %d ", toShootdownGPU+1)
	}

	fmt.Print("\n")
	return true
}

func (d *Driver) processShootdownCompleteRsp(
	req *protocol.ShootDownCompleteRsp,
) bool {
	d.numShootDownACK--

	if d.numShootDownACK == 0 {
		fmt.Printf("[Driver]\tComplete ShootDown\n\t\tInitiate RDMA Drain\n")

		return d.initiateRDMADrain()
	}

	return true
}

func (d *Driver) initiateRDMADrain() bool {
	if d.currentPageMigrationReq.CurrPageHostGPU == 1 {
		fmt.Printf("[Driver]\tSkip RDMA Drain Because of 1st touch\n")
		d.numRDMADrainACK++
		d.processRDMADrainRsp(nil)

		return true
	}

	for i := 0; i < len(d.GPUs); i++ {
		req := protocol.NewRDMADrainCmdFromDriver(d.gpuPort,
			d.GPUs[i])
		d.requestsToSend = append(d.requestsToSend, req)
		d.numRDMADrainACK++
	}

	return true
}

func (d *Driver) processRDMADrainRsp(
	req *protocol.RDMADrainRspToDriver,
) bool {
	d.numRDMADrainACK--

	if d.numRDMADrainACK == 0 {
		fmt.Printf("[Driver]\tComplete RDMA Drain\n\t\tPrepare Migration Req to CP: ")

		toRequestFromGPU := d.currentPageMigrationReq.CurrPageHostGPU
		toRequestFromPMCPort := d.RemotePMCPorts[toRequestFromGPU-1]

		migrationInfo := d.currentPageMigrationReq.MigrationInfo

		requestingGPUs := d.findRequestingGPUs(migrationInfo)
		context := d.findContext(d.currentPageMigrationReq.PID)

		pageVaddrs := make(map[uint64][]uint64)

		for i := 0; i < len(requestingGPUs); i++ {
			pageVaddrs[requestingGPUs[i]] =
				migrationInfo.GPUReqToVAddrMap[requestingGPUs[i]+1]
		}

		for gpuID, vAddrs := range pageVaddrs {
			for i := 0; i < len(vAddrs); i++ {
				fmt.Printf("%x ", vAddrs[i])

				vAddr := vAddrs[i]
				var page *vm.Page
				var oldPAddr, oldDeviceID uint64
				if d.currentPageMigrationReq.ForDuplication {
					page, oldPAddr, oldDeviceID = d.preparePageForDuplication(vAddr, context, gpuID)
				} else if d.currentPageMigrationReq.ForInvalidation {
					page, oldPAddr, oldDeviceID = d.preparePageForInvalidation(vAddr, context, gpuID)
				} else {
					page, oldPAddr, oldDeviceID = d.preparePageForMigration(vAddr, context, gpuID)
				}

				if page.DeviceID == oldDeviceID && d.currentPageMigrationReq.ForInvalidation {
					// Duplication policy에서 invalidation 시, 이미 해당 page가 GPU에 존재하는 경우, migration을 하지 않음
					continue
				}

				req := protocol.NewPageMigrationReqToCP(d.gpuPort,
					d.GPUs[gpuID])
				req.DestinationPMCPort = toRequestFromPMCPort
				req.ToReadFromPhysicalAddress = oldPAddr
				req.ToWriteToPhysicalAddress = page.PAddr
				req.PageSize = d.currentPageMigrationReq.PageSize
				req.VirtualAddress = vAddr
				req.PID = context.pid

				d.migrationReqToSendToCP = append(d.migrationReqToSendToCP, req)
				d.numPagesMigratingACK++
			}
		}

		if d.numPagesMigratingACK == 0 {
			d.numPagesMigratingACK++
			d.processPageMigrationRspFromCP(nil)
		}

		fmt.Printf("\n")

		return true
	}

	return true
}

func (d *Driver) findRequestingGPUs(
	migrationInfo *vm.PageMigrationInfo,
) []uint64 {
	requestingGPUs := make([]uint64, 0)

	for i := 1; i < d.GetNumGPUs()+1; i++ {
		_, found := migrationInfo.GPUReqToVAddrMap[uint64(i)]
		if found {
			requestingGPUs = append(requestingGPUs, uint64(i-1))
		}
	}
	return requestingGPUs
}

func (d *Driver) findContext(pid vm.PID) *Context {
	context := &Context{}
	for i := 0; i < len(d.contexts); i++ {
		if d.contexts[i].pid == d.currentPageMigrationReq.PID {
			context = d.contexts[i]
		}
	}
	if context == nil {
		log.Panicf("Process does not exist")
	}
	return context
}

func (d *Driver) preparePageForMigration(
	vAddr uint64,
	context *Context,
	gpuID uint64,
) (*vm.Page, uint64, uint64) {
	page, found := d.pageTable.Find(context.pid, vAddr)
	if !found {
		panic("page not founds")
	}
	oldPAddr := page.PAddr
	oldDeviceID := page.DeviceID

	newPage := d.memAllocator.AllocatePageWithGivenVAddr(
		context.pid, int(gpuID+1), vAddr, true)
	newPage.DeviceID = gpuID + 1

	newPage.IsMigrating = true
	d.pageTable.Update(newPage)

	d.memAllocator.CountMemUsage(oldDeviceID, page.PageSize, true)
	return &newPage, oldPAddr, oldDeviceID
}

func (d *Driver) preparePageForDuplication(
	vAddr uint64,
	context *Context,
	gpuID uint64,
) (*vm.Page, uint64, uint64) {
	page, found := d.pageTable.Find(context.pid, vAddr)
	if !found {
		panic("page not founds")
	}
	oldPAddr := page.PAddr
	oldDeviceID := page.DeviceID

	newPage := d.memAllocator.AllocatePageWithGivenVAddr(
		context.pid, int(gpuID+1), vAddr, true)
	newPage.DeviceID = gpuID + 1
	newPage.IsShared = true

	page.IsShared = true
	page.SharedPages = append(page.SharedPages, newPage)
	d.pageTable.Update(page)

	return &page, oldPAddr, oldDeviceID
}

func (d *Driver) preparePageForInvalidation(
	vAddr uint64,
	context *Context,
	gpuID uint64,
) (*vm.Page, uint64, uint64) {

	page, found := d.pageTable.Find(context.pid, vAddr)
	if !found {
		panic("page not founds")
	}
	oldPAddr := page.PAddr
	oldDeviceID := page.DeviceID

	newPage, f := d.GetSharedPageByDeviceID(page, int(gpuID+1))
	if !f {
		newPage = d.memAllocator.AllocatePageWithGivenVAddr(
			context.pid, int(gpuID+1), vAddr, true)
		newPage.DeviceID = gpuID + 1
	}

	newPage.IsShared = false
	newPage.IsMigrating = true
	d.pageTable.Update(newPage)

	if newPage.DeviceID != oldDeviceID {
		d.memAllocator.CountMemUsage(oldDeviceID, page.PageSize, true)
	}
	for _, pg := range page.SharedPages {
		if pg.DeviceID == newPage.DeviceID {
			continue
		}
		d.memAllocator.CountMemUsage(pg.DeviceID, page.PageSize, true)
	}

	return &newPage, oldPAddr, oldDeviceID
}

func (d *Driver) sendMigrationReqToCP() bool {
	d.returnValue[15] = "true"
	d.returnValue[16] = "true"
	d.returnValue[17] = "true"
	if len(d.migrationReqToSendToCP) == 0 {
		d.returnValue[15] = "false"
		return false
	}

	if d.isCurrentlyMigratingOnePage {
		d.returnValue[16] = "false"
		return false
	}

	req := d.migrationReqToSendToCP[0]

	err := d.gpuPort.Send(req)
	if err == nil {
		d.migrationReqToSendToCP = d.migrationReqToSendToCP[1:]
		d.isCurrentlyMigratingOnePage = true

		fmt.Printf("[Driver]\tSend Migration Req(%x -> %x) to %s\n", req.ToReadFromPhysicalAddress, req.ToWriteToPhysicalAddress, req.Dst)
		return true
	}

	fmt.Printf("[Driver]\tFailed to Send Migration Req(%x -> %x) to %s", req.ToReadFromPhysicalAddress, req.ToWriteToPhysicalAddress, req.Dst)
	d.returnValue[17] = "false"
	return false
}

func (d *Driver) processPageMigrationRspFromCP(
	rsp *protocol.PageMigrationRspToDriver,
) bool {
	d.numPagesMigratingACK--
	d.isCurrentlyMigratingOnePage = false

	if d.numPagesMigratingACK == 0 {
		fmt.Printf("[Driver]\tMigration Complete\n\t\tPrepare PTE Invalidation\n")

		if d.currentPageMigrationReq.ForDuplication {
			d.RemoveRequestInMMU()
		} else {
			d.preparePTEInvalidationReqToCP()
		}
	}

	return true
}

func (d *Driver) preparePTEInvalidationReqToCP() bool {
	accessingGPUs := d.currentPageMigrationReq.CurrAccessingGPUs
	requestingGPU := d.currentPageMigrationReq.RequestingDevice
	VAs := d.currentPageMigrationReq.MigrationInfo.GPUReqToVAddrMap[requestingGPU]

	for _, gpuID := range accessingGPUs {
		if gpuID == requestingGPU {
			continue
		}

		for _, va := range VAs {
			invReq := vm.PTEInvalidationReqBuilder{}.
				WithSrc(d.gpuPort.AsRemote()).
				WithDst(d.GPUs[gpuID-1].AsRemote()).
				WithVAddr(va).
				WithPID(d.currentPageMigrationReq.PID).
				WithPageSize(d.currentPageMigrationReq.PageSize).
				Build()

			d.invalidationReqToSendToCP = append(d.invalidationReqToSendToCP, invReq)
			d.numInvalidationACK++
		}
	}

	return true
}

func (d *Driver) sendInvalidationReqToCP() bool {
	d.returnValue[19] = "true"
	d.returnValue[20] = "true"

	if len(d.invalidationReqToSendToCP) == 0 {
		d.returnValue[19] = "false"
		return false
	}

	req := d.invalidationReqToSendToCP[0]

	err := d.gpuPort.Send(req)
	if err == nil {
		d.invalidationReqToSendToCP = d.invalidationReqToSendToCP[1:]
		return true
	}

	d.returnValue[20] = "false"
	return false
}

func (d *Driver) processInvalidationRspFromCP(
	rsp *vm.PTEInvalidationRsp,
) bool {
	d.numInvalidationACK--

	if d.numInvalidationACK == 0 {
		d.RemoveRequestInMMU()
	}

	return true
}

func (d *Driver) RemoveRequestInMMU() {
	req := vm.NewRemoveRequestInMMUFromDriver(
		d.mmuPort.AsRemote(),
		d.currentPageMigrationReq.Src,
	)

	err := d.mmuPort.Send(req)
	if err != nil {
		fmt.Printf("[Driver]\t[Warning]\tFailed to send RemoveRequestInMMU request to MMU\n")
		return
	}
}

func (d *Driver) processRemoveRequestRspFromMMU(
	rsp *vm.RemoveRequestRspFromMMU,
) bool {
	if d.currentPageMigrationReq.CurrPageHostGPU == 1 {
		fmt.Printf("[Driver]\tSkip RDMA Restart\n")
		d.numRDMARestartACK++
		d.processRDMARestartRspToDriver(nil)

		return true
	}

	d.prepareRDMARestartReqs()

	return true
}

func (d *Driver) prepareRDMARestartReqs() {
	for i := 0; i < len(d.GPUs); i++ {
		fmt.Printf("[Driver]\tSend RDMA Restart cmd to GPU %d\n", i+1)

		req := protocol.NewRDMARestartCmdFromDriver(d.gpuPort, d.GPUs[i])
		d.requestsToSend = append(d.requestsToSend, req)
		d.numRDMARestartACK++

		tracing.AddTaskStep(
			d.currentPageMigrationReq.Meta().ID,
			d,
			fmt.Sprintf("Send RDMA Restart Cmd to GPU %d", i+1),
		)
	}
}

func (d *Driver) processRDMARestartRspToDriver(
	rsp *protocol.RDMARestartRspToDriver) bool {
	d.numRDMARestartACK--

	if d.numRDMARestartACK == 0 {
		fmt.Printf("[Driver]\tComplete RDMA Restart\n\t\t\tSend GPU Restart Req\n")
		d.prepareGPURestartReqs()
	}

	return true
}

func (d *Driver) prepareGPURestartReqs() {
	if d.currentPageMigrationReq.CurrPageHostGPU == 1 {
		fmt.Printf("[Driver]\tSkip GPU Restart Because of 1st touch\n")
		d.numRestartACK++
		d.handleGPURestartRsp(nil)

		return
	}

	for i, _ := range d.GPUs {
		if uint64(i) == 0 {
			continue
		}

		restartGPUID := i
		restartReq := protocol.NewGPURestartReq(
			d.gpuPort,
			d.GPUs[restartGPUID])
		d.requestsToSend = append(d.requestsToSend, restartReq)
		d.numRestartACK++
	}
}

func (d *Driver) handleGPURestartRsp(
	req *protocol.GPURestartRsp,
) bool {
	d.numRestartACK--

	if d.numRestartACK == 0 {
		d.preparePageMigrationRspToMMU()
		d.processMigrationCompletion()

		if req != nil {
			tracing.EndTask(
				d.shootDownReqID[uint64(req.DeviceID-1)],
				d,
			)
		}
	}

	return true
}

func (d *Driver) preparePageMigrationRspToMMU() {
	requestingGPUs := make([]uint64, 0)

	migrationInfo := d.currentPageMigrationReq.MigrationInfo

	for i := 1; i < d.GetNumGPUs()+1; i++ {
		_, found := migrationInfo.GPUReqToVAddrMap[uint64(i)]
		if found {
			requestingGPUs = append(requestingGPUs, uint64(i-1))
		}
	}

	pageVaddrs := make(map[uint64][]uint64)

	for i := 0; i < len(requestingGPUs); i++ {
		pageVaddrs[requestingGPUs[i]] = migrationInfo.GPUReqToVAddrMap[requestingGPUs[i]+1]
	}

	req := vm.NewPageMigrationRspFromDriver(d.mmuPort.AsRemote(),
		d.currentPageMigrationReq.Src, d.currentPageMigrationReq)

	for _, vAddrs := range pageVaddrs {
		for j := 0; j < len(vAddrs); j++ {
			req.VAddr = append(req.VAddr, vAddrs[j])
		}
	}
	req.RspToTop = d.currentPageMigrationReq.RespondToTop
	d.toSendToMMU = req
}

func (d *Driver) sendToMMU() bool {
	d.returnValue[22] = "true"
	d.returnValue[23] = "true"

	if d.toSendToMMU == nil {
		d.returnValue[22] = "false"
		return false
	}

	req := d.toSendToMMU
	err := d.mmuPort.Send(req)
	if err == nil {
		d.toSendToMMU = nil
		return true
	}

	d.returnValue[23] = "false"
	return false
}

func (d *Driver) processMigrationCompletion() bool {
	fmt.Printf("[Driver]\tComplete Migration\n")

	tracing.EndTask(d.currentPageMigrationReq.Meta().ID, d)

	memUsg := "[ "
	// fmt.Printf("[Driver]\tCompleted Page Migration Request for VA %x\n", d.currentPageMigrationReq.MigrationInfo.GPUReqToVAddrMap[d.currentPageMigrationReq.RequestingDevice][0])
	// fmt.Printf("\t\t\tMemory Usage: [ ")
	for i := 1; i <= d.GetNumGPUs(); i++ {
		usage := d.memAllocator.GetMemUsage(uint64(i)) >> d.Log2PageSize
		// fmt.Printf("%d pages, ", usage)
		memUsg += fmt.Sprintf("%d ", usage)
	}
	// fmt.Printf("]\n\n")
	memUsg += fmt.Sprintf("]")
	tracing.StartTask(
		d.currentPageMigrationReq.Meta().ID+"_MemUsage",
		"",
		d,
		"Memory Usage",
		memUsg,
		nil,
	)
	tracing.EndTask(d.currentPageMigrationReq.Meta().ID+"_MemUsage", d)

	d.currentPageMigrationReq = nil
	d.isCurrentlyHandlingMigrationReq = false

	return true
}

func (d *Driver) GetSharedPageByDeviceID(page vm.Page, deviceID int) (vm.Page, bool) {
	if page.DeviceID == uint64(deviceID) {
		return page, true
	}

	for _, pg := range page.SharedPages {
		if pg.DeviceID == uint64(deviceID) {
			return pg, true
		}
	}

	return vm.Page{}, false
}
