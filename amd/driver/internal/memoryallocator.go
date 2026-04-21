// Package internal provides support for the driver implementation.
package internal

import (
	"fmt"
	"sync"

	"github.com/sarchlab/akita/v4/mem/vm"
)

// A MemoryAllocator can allocate memory on the CPU and GPUs
type MemoryAllocator interface {
	RegisterDevice(device *Device)
	GetDeviceIDByPAddr(pAddr uint64) int
	Allocate(pid vm.PID, byteSize uint64, deviceID int) uint64
	AllocateUnified(pid vm.PID, byteSize uint64) uint64
	Free(vAddr uint64)
	Remap(pid vm.PID, pageVAddr, byteSize uint64, deviceID int)
	RemovePage(vAddr uint64)
	AllocatePageWithGivenVAddr(
		pid vm.PID,
		deviceID int,
		vAddr uint64,
		unified bool,
	) vm.Page
	CountMemUsage(deviceID uint64, byteSize uint64, remove bool)
	GetMemUsage(deviceID uint64) uint64
}

// NewMemoryAllocator creates a new memory allocator.
func NewMemoryAllocator(
	pageTable vm.LevelPageTable,
	log2PageSize uint64,
) MemoryAllocator {
	a := &memoryAllocatorImpl{
		pageTable:            pageTable,
		totalStorageByteSize: 1 << log2PageSize, // Starting with a page to avoid 0 address.
		log2PageSize:         log2PageSize,
		processMemoryStates:  make(map[vm.PID]*processMemoryState),
		vAddrToPageMapping:   make(map[uint64]vm.Page),
		devices:              make(map[int]*Device),
	}
	return a
}

type processMemoryState struct {
	pid       vm.PID
	nextVAddr uint64
}

// A memoryAllocatorImpl provides the default implementation for
// memoryAllocator
type memoryAllocatorImpl struct {
	sync.Mutex
	pageTable            vm.LevelPageTable
	log2PageSize         uint64
	vAddrToPageMapping   map[uint64]vm.Page
	processMemoryStates  map[vm.PID]*processMemoryState
	devices              map[int]*Device
	totalStorageByteSize uint64
	allocatedMemSize     map[uint64]uint64 // deviceID -> bytes allocated
	nextDeviceID         int               // page 할당할 device ID: RR 방식으로 할당
}

func (a *memoryAllocatorImpl) RegisterDevice(device *Device) {
	a.Lock()
	defer a.Unlock()

	state := device.MemState
	state.setInitialAddress(a.totalStorageByteSize)

	a.totalStorageByteSize += state.getStorageSize()

	a.devices[device.ID] = device
}

func (a *memoryAllocatorImpl) GetDeviceIDByPAddr(pAddr uint64) int {
	a.Lock()
	defer a.Unlock()

	return a.deviceIDByPAddr(pAddr)
}

func (a *memoryAllocatorImpl) deviceIDByPAddr(pAddr uint64) int {
	for id, dev := range a.devices {
		state := dev.MemState
		if isPAddrOnDevice(pAddr, state) {
			return id
		}
	}

	panic("device not found")
}

func isPAddrOnDevice(
	pAddr uint64,
	state DeviceMemoryState,
) bool {
	return pAddr >= state.getInitialAddress() &&
		pAddr < state.getInitialAddress()+state.getStorageSize()
}

func (a *memoryAllocatorImpl) Allocate(
	pid vm.PID,
	byteSize uint64,
	deviceID int,
) uint64 {
	if byteSize == 0 {
		panic("Allocating 0 bytes.")
	}

	a.Lock()
	defer a.Unlock()

	pageSize := uint64(1 << a.log2PageSize)
	numPages := (byteSize-1)/pageSize + 1

	fmt.Printf("[MemoryAllocator]\t%d Byte Request\n\t\t\tAllocating %d pages for pid %d on device %d\n", byteSize, numPages, pid, deviceID)
	return a.allocatePages(int(numPages), pid, deviceID, false)
}

func (a *memoryAllocatorImpl) AllocateUnified(
	pid vm.PID,
	byteSize uint64,
) uint64 {
	if byteSize == 0 {
		panic("Allocating 0 bytes.")
	}

	a.Lock()
	defer a.Unlock()

	const chunkBytes = 2 * 1024 * 1024 // 2MB per RR chunk
	pageSize := uint64(1 << a.log2PageSize)
	numPages := (byteSize-1)/pageSize + 1
	pagesPerChunk := uint64(chunkBytes) / pageSize
	if pagesPerChunk == 0 {
		pagesPerChunk = 1
	}

	var firstVAddr uint64
	remainingPages := numPages
	first := true

	for remainingPages > 0 {
		chunkPages := pagesPerChunk
		if chunkPages > remainingPages {
			chunkPages = remainingPages
		}

		deviceID := a.nextDeviceID + 2
		a.nextDeviceID = (a.nextDeviceID + 1) % (len(a.devices) - 3)

		vAddr := a.allocatePages(int(chunkPages), pid, deviceID, true)
		if first {
			firstVAddr = vAddr
			first = false
		}
		remainingPages -= chunkPages
	}

	fmt.Printf("[MemoryAllocator]\t%d Byte Request\n\t\t\tAllocating %d pages in 2MB RR chunks across GPUs\n", byteSize, numPages)
	return firstVAddr
}

func (a *memoryAllocatorImpl) allocatePages(
	numPages int,
	pid vm.PID,
	deviceID int,
	unified bool,
) (firstPageVAddr uint64) {
	pState, found := a.processMemoryStates[pid]
	if !found {
		a.processMemoryStates[pid] = &processMemoryState{
			pid:       pid,
			nextVAddr: uint64(1 << a.log2PageSize),
		}
		pState = a.processMemoryStates[pid]
	}
	device := a.devices[deviceID]

	pageSize := uint64(1 << a.log2PageSize)
	nextVAddr := pState.nextVAddr

	for i := 0; i < numPages; i++ {
		pAddr := device.allocatePage()
		vAddr := nextVAddr + uint64(i)*pageSize
		a.CountMemUsage(uint64(deviceID), pageSize, false)

		page := vm.Page{
			PID:      pid,
			VAddr:    vAddr,
			PAddr:    pAddr,
			PageSize: pageSize,
			Valid:    true,
			Unified:  unified,
			DeviceID: uint64(a.deviceIDByPAddr(pAddr)),
		}

		// fmt.Printf("page.addr is %x piage Device ID is %d \n", page.PAddr, page.DeviceID)
		// debug.PrintStack()
		a.pageTable.Insert(page)
		a.vAddrToPageMapping[page.VAddr] = page
	}

	pState.nextVAddr += pageSize * uint64(numPages)

	return nextVAddr
}

func (a *memoryAllocatorImpl) CountMemUsage(
	deviceID uint64,
	byteSize uint64,
	remove bool,
) {
	if a.allocatedMemSize == nil {
		a.allocatedMemSize = make(map[uint64]uint64)
	}

	if _, found := a.allocatedMemSize[deviceID]; !found {
		a.allocatedMemSize[deviceID] = 0
	}

	if remove {
		if a.allocatedMemSize[deviceID] < byteSize {
			a.allocatedMemSize[deviceID] = 0
		} else {
			a.allocatedMemSize[deviceID] -= byteSize
		}
	} else {
		a.allocatedMemSize[deviceID] += byteSize
	}
}

func (a *memoryAllocatorImpl) Remap(
	pid vm.PID,
	pageVAddr, byteSize uint64,
	deviceID int,
) {
	a.Lock()
	defer a.Unlock()

	pageSize := uint64(1 << a.log2PageSize)
	addr := pageVAddr
	vAddrs := make([]uint64, 0)
	for addr < pageVAddr+byteSize {
		vAddrs = append(vAddrs, addr)
		addr += pageSize
	}

	a.allocateMultiplePagesWithGivenVAddrs(pid, deviceID, vAddrs, false)
}

func (a *memoryAllocatorImpl) RemovePage(vAddr uint64) {
	a.Lock()
	defer a.Unlock()

	a.removePage(vAddr)
}

func (a *memoryAllocatorImpl) removePage(vAddr uint64) {
	page, ok := a.vAddrToPageMapping[vAddr]

	if !ok {
		panic("page not found")
	}

	deviceID := a.deviceIDByPAddr(page.PAddr)
	dState := a.devices[deviceID].MemState
	dState.addSinglePAddr(page.PAddr)

	a.CountMemUsage(uint64(deviceID), a.log2PageSize, true)
	a.pageTable.Remove(page.PID, page.VAddr)
}

func (a *memoryAllocatorImpl) AllocatePageWithGivenVAddr(
	pid vm.PID,
	deviceID int,
	vAddr uint64,
	isUnified bool,
) vm.Page {
	a.Lock()
	defer a.Unlock()

	return a.allocatePageWithGivenVAddr(pid, deviceID, vAddr, isUnified)
}

func (a *memoryAllocatorImpl) allocatePageWithGivenVAddr(
	pid vm.PID,
	deviceID int,
	vAddr uint64,
	isUnified bool,
) vm.Page {
	pageSize := uint64(1 << a.log2PageSize)

	device := a.devices[deviceID]
	pAddr := device.allocatePage()

	page := vm.Page{
		PID:      pid,
		VAddr:    vAddr,
		PAddr:    pAddr,
		PageSize: pageSize,
		Valid:    true,
		DeviceID: uint64(deviceID),
		Unified:  isUnified,
	}
	a.vAddrToPageMapping[page.VAddr] = page
	a.pageTable.Update(page)
	a.CountMemUsage(uint64(deviceID), pageSize, false)

	return page
}

func (a *memoryAllocatorImpl) allocateMultiplePagesWithGivenVAddrs(
	pid vm.PID,
	deviceID int,
	vAddrs []uint64,
	isUnified bool,
) (pages []vm.Page) {
	pageSize := uint64(1 << a.log2PageSize)

	device := a.devices[deviceID]
	pAddrs := device.allocateMultiplePages(len(vAddrs))

	for i, vAddr := range vAddrs {
		page := vm.Page{
			PID:      pid,
			VAddr:    vAddr,
			PAddr:    pAddrs[i],
			PageSize: pageSize,
			Valid:    true,
			DeviceID: uint64(deviceID),
			Unified:  isUnified,
		}
		a.vAddrToPageMapping[page.VAddr] = page
		a.pageTable.Update(page)
		pages = append(pages, page)
		a.CountMemUsage(uint64(deviceID), pageSize, false)
	}

	return pages
}

func (a *memoryAllocatorImpl) Free(ptr uint64) {
	a.Lock()
	defer a.Unlock()

	a.removePage(ptr)
}

func (a *memoryAllocatorImpl) GetMemUsage(deviceID uint64) uint64 {
	if a.allocatedMemSize == nil {
		return 0
	}

	if _, found := a.allocatedMemSize[deviceID]; !found {
		return 0
	}

	return a.allocatedMemSize[deviceID]
}
