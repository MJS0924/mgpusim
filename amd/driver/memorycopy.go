package driver

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/sarchlab/akita/v4/mem/vm"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/mgpusim/v4/amd/protocol"
)

// defaultMemoryCopyMiddleware handles memory copy commands and related
// communication.
type defaultMemoryCopyMiddleware struct {
	driver *Driver

	cyclesPerH2D int
	cyclesPerD2H int
	cyclesLeft   int

	awaitingReqs []sim.Msg

	processingH2DCmd *MemCopyH2DCommand
	processingD2HCmd *MemCopyD2HCommand
	CmdQueue         *CommandQueue
	CmdQueuePID      vm.PID

	CohDirACK int
	CacheACK  int
}

func (m *defaultMemoryCopyMiddleware) ProcessCommand(
	cmd Command,
	queue *CommandQueue,
) (processed bool) {
	switch cmd := cmd.(type) {
	case *MemCopyH2DCommand:
		return m.processMemCopyH2DCommand_1(cmd, queue)
	case *MemCopyD2HCommand:
		return m.processMemCopyD2HCommand_1(cmd, queue)
	}

	return false
}

func (m *defaultMemoryCopyMiddleware) processMemCopyH2DCommand_1(
	cmd *MemCopyH2DCommand,
	queue *CommandQueue,
) bool {
	fmt.Printf("[Driver] Processing MemCopyH2DCommand\n")

	if m.needFlushing(queue.Context, cmd.Dst, uint64(binary.Size(cmd.Src))) {
		m.processingH2DCmd = cmd
		m.processingD2HCmd = nil
		m.CmdQueue = queue
		m.CmdQueuePID = queue.Context.pid
		// queue.Dequeue()
		queue.IsRunning = true

		m.sendCohDirFlushRequest(cmd)
		queue.Context.removeFreedBuffers()

		return true
	}

	buffer := bytes.NewBuffer(nil)
	err := binary.Write(buffer, binary.LittleEndian, cmd.Src)
	if err != nil {
		panic(err)
	}
	rawBytes := buffer.Bytes()

	offset := uint64(0)
	addr := uint64(cmd.Dst)
	sizeLeft := uint64(len(rawBytes))
	for sizeLeft > 0 {
		page, found := m.driver.pageTable.Find(queue.Context.pid, addr)
		if !found {
			panic("page not found")
		}

		pAddr := page.PAddr + (addr - page.VAddr)
		sizeLeftInPage := page.PageSize - (addr - page.VAddr)
		sizeToCopy := sizeLeftInPage
		if sizeLeft < sizeLeftInPage {
			sizeToCopy = sizeLeft
		}

		gpuID := m.driver.memAllocator.GetDeviceIDByPAddr(pAddr)
		req := protocol.NewMemCopyH2DReq(
			m.driver.gpuPort, m.driver.GPUs[gpuID-1],
			rawBytes[offset:offset+sizeToCopy],
			pAddr)
		cmd.Reqs = append(cmd.Reqs, req)
		m.awaitingReqs = append(m.awaitingReqs, req)
		// m.driver.requestsToSend = append(m.driver.requestsToSend, req)

		sizeLeft -= sizeToCopy
		addr += sizeToCopy
		offset += sizeToCopy

		m.driver.logTaskToGPUInitiate(cmd, req)
	}

	m.cyclesLeft = m.cyclesPerH2D

	queue.IsRunning = true

	return true
}

func (m *defaultMemoryCopyMiddleware) processMemCopyH2DCommand_2(
	cmd *MemCopyH2DCommand,
	queue *CommandQueue,
) bool {
	fmt.Printf("[Driver] Processing MemCopyH2DCommand after Dir/Cache flush\n")

	buffer := bytes.NewBuffer(nil)
	err := binary.Write(buffer, binary.LittleEndian, cmd.Src)
	if err != nil {
		panic(err)
	}
	rawBytes := buffer.Bytes()

	offset := uint64(0)
	addr := uint64(cmd.Dst)
	sizeLeft := uint64(len(rawBytes))
	for sizeLeft > 0 {
		page, found := m.driver.pageTable.Find(m.CmdQueuePID, addr)
		if !found {
			panic("page not found")
		}

		pAddr := page.PAddr + (addr - page.VAddr)
		sizeLeftInPage := page.PageSize - (addr - page.VAddr)
		sizeToCopy := sizeLeftInPage
		if sizeLeft < sizeLeftInPage {
			sizeToCopy = sizeLeft
		}

		gpuID := m.driver.memAllocator.GetDeviceIDByPAddr(pAddr)
		req := protocol.NewMemCopyH2DReq(
			m.driver.gpuPort, m.driver.GPUs[gpuID-1],
			rawBytes[offset:offset+sizeToCopy],
			pAddr)
		cmd.Reqs = append(cmd.Reqs, req)
		m.awaitingReqs = append(m.awaitingReqs, req)
		// m.driver.requestsToSend = append(m.driver.requestsToSend, req)

		sizeLeft -= sizeToCopy
		addr += sizeToCopy
		offset += sizeToCopy

		m.driver.logTaskToGPUInitiate(cmd, req)
	}

	m.cyclesLeft = m.cyclesPerH2D

	queue.IsRunning = true

	return true
}

func (m *defaultMemoryCopyMiddleware) processMemCopyD2HCommand_1(
	cmd *MemCopyD2HCommand,
	queue *CommandQueue,
) bool {
	fmt.Printf("[Driver] Processing MemCopyD2HCommand\n")

	if m.needFlushing(queue.Context, cmd.Src, uint64(binary.Size(cmd.Dst))) {
		m.processingD2HCmd = cmd
		m.processingH2DCmd = nil
		m.CmdQueue = queue
		m.CmdQueuePID = queue.Context.pid
		// queue.Dequeue()
		queue.IsRunning = true

		m.sendCohDirFlushRequest(cmd)
		queue.Context.removeFreedBuffers()

		return true
	}

	m.processingD2HCmd = cmd
	m.CmdQueue = queue

	cmd.RawData = make([]byte, binary.Size(cmd.Dst))

	offset := uint64(0)
	addr := uint64(cmd.Src)
	sizeLeft := uint64(len(cmd.RawData))
	for sizeLeft > 0 {
		page, found := m.driver.pageTable.Find(queue.Context.pid, addr)
		if !found {
			panic("page not found")
		}

		pAddr := page.PAddr + (addr - page.VAddr)
		sizeLeftInPage := page.PageSize - (addr - page.VAddr)
		sizeToCopy := sizeLeftInPage
		if sizeLeft < sizeLeftInPage {
			sizeToCopy = sizeLeft
		}

		gpuID := m.driver.memAllocator.GetDeviceIDByPAddr(pAddr)
		req := protocol.NewMemCopyD2HReq(
			m.driver.gpuPort, m.driver.GPUs[gpuID-1],
			pAddr, cmd.RawData[offset:offset+sizeToCopy])
		cmd.Reqs = append(cmd.Reqs, req)
		m.awaitingReqs = append(m.awaitingReqs, req)
		// m.driver.requestsToSend = append(m.driver.requestsToSend, req)

		sizeLeft -= sizeToCopy
		addr += sizeToCopy
		offset += sizeToCopy

		m.driver.logTaskToGPUInitiate(cmd, req)
	}

	m.cyclesLeft = m.cyclesPerD2H

	queue.IsRunning = true
	return true
}

func (m *defaultMemoryCopyMiddleware) processMemCopyD2HCommand_2(
	cmd *MemCopyD2HCommand,
	queue *CommandQueue,
) bool {
	fmt.Printf("[Driver] Processing MemCopyD2HCommand after Dir/Cache flush\n")

	cmd.RawData = make([]byte, binary.Size(cmd.Dst))

	offset := uint64(0)
	addr := uint64(cmd.Src)
	sizeLeft := uint64(len(cmd.RawData))
	for sizeLeft > 0 {
		page, found := m.driver.pageTable.Find(m.CmdQueuePID, addr)
		if !found {
			panic("page not found")
		}

		pAddr := page.PAddr + (addr - page.VAddr)
		sizeLeftInPage := page.PageSize - (addr - page.VAddr)
		sizeToCopy := sizeLeftInPage
		if sizeLeft < sizeLeftInPage {
			sizeToCopy = sizeLeft
		}

		gpuID := m.driver.memAllocator.GetDeviceIDByPAddr(pAddr)
		req := protocol.NewMemCopyD2HReq(
			m.driver.gpuPort, m.driver.GPUs[gpuID-1],
			pAddr, cmd.RawData[offset:offset+sizeToCopy])
		cmd.Reqs = append(cmd.Reqs, req)
		m.awaitingReqs = append(m.awaitingReqs, req)
		// m.driver.requestsToSend = append(m.driver.requestsToSend, req)

		sizeLeft -= sizeToCopy
		addr += sizeToCopy
		offset += sizeToCopy

		m.driver.logTaskToGPUInitiate(cmd, req)
	}

	m.cyclesLeft = m.cyclesPerD2H

	queue.IsRunning = true
	return true
}

func (m *defaultMemoryCopyMiddleware) needFlushing(
	ctx *Context,
	vAddr Ptr,
	size uint64,
) bool {
	startAddr := uint64(vAddr)
	endAddr := uint64(vAddr) + size
	for _, buf := range ctx.buffers {
		bufStartAddr := uint64(buf.vAddr)
		bufEndAddr := uint64(buf.vAddr) + buf.size
		if memRangeOverlap(bufStartAddr, bufEndAddr, startAddr, endAddr) {
			if buf.l2Dirty {
				return true
			}
		}
	}

	return false
}

func memRangeOverlap(
	start1, end1, start2, end2 uint64,
) bool {
	if start1 <= start2 && end1 >= start2 {
		return true
	}

	if start1 <= end2 && end1 >= end2 {
		return true
	}

	return false
}

func (m *defaultMemoryCopyMiddleware) sendCacheFlushRequest(
	cmd Command,
) bool {
	for _, gpu := range m.driver.GPUs {
		req := protocol.NewFlushReq(m.driver.gpuPort, gpu)
		req.Cache = true
		m.driver.requestsToSend = append(m.driver.requestsToSend, req)
		cmd.AddReq(req)

		m.driver.logTaskToGPUInitiate(cmd, req)
		m.CacheACK++
	}

	return true
}

func (m *defaultMemoryCopyMiddleware) sendCohDirFlushRequest(
	cmd Command,
) bool {
	fmt.Printf("[Driver] Sending CohDir flush for command\n")
	for _, gpu := range m.driver.GPUs {
		req := protocol.NewFlushReq(m.driver.gpuPort, gpu)
		req.CohDir = true
		m.driver.requestsToSend = append(m.driver.requestsToSend, req)
		cmd.AddReq(req)

		m.driver.logTaskToGPUInitiate(cmd, req)
		m.CohDirACK++
	}

	return true
}

func (m *defaultMemoryCopyMiddleware) Tick() (madeProgress bool) {
	madeProgress = false

	if m.cyclesLeft > 0 {
		m.cyclesLeft--
		madeProgress = true
	} else if m.cyclesLeft == 0 {
		m.driver.requestsToSend = append(m.driver.requestsToSend, m.awaitingReqs...)
		m.awaitingReqs = nil
		m.cyclesLeft = -1
		madeProgress = true

		fmt.Printf("[Driver.MemCpy]\tAppend awaitingReqs to requestsToSend\n")
	}

	req := m.driver.gpuPort.PeekIncoming()
	if req == nil {
		return madeProgress
	}

	switch req := req.(type) {
	case *sim.GeneralRsp:
		madeProgress = m.processGeneralRsp(req)
	}

	return madeProgress
}

func (m *defaultMemoryCopyMiddleware) processGeneralRsp(
	rsp *sim.GeneralRsp,
) bool {
	madeProgress := false
	originalReq := rsp.OriginalReq

	switch originalReq := originalReq.(type) {
	case *protocol.FlushReq:
		if originalReq.Cache {
			madeProgress = m.processFlushReturn(originalReq)
			m.CacheACK--

			if m.CacheACK == 0 {
				fmt.Printf("[Driver]\tComplete Cache Flush\n")

				if m.processingH2DCmd != nil {
					madeProgress = m.processMemCopyH2DCommand_2(m.processingH2DCmd, m.CmdQueue)
				} else {
					madeProgress = m.processMemCopyD2HCommand_2(m.processingD2HCmd, m.CmdQueue)
				}
			}
		} else {
			madeProgress = m.processFlushReturn(originalReq) || madeProgress
			m.CohDirACK--

			if m.CohDirACK == 0 {
				fmt.Printf("[Driver]\tComplete Coh Dir Flush\n\t\tSend Cache Flush Request\n")

				if m.processingH2DCmd != nil {
					madeProgress = m.sendCacheFlushRequest(m.processingH2DCmd)
				} else {
					madeProgress = m.sendCacheFlushRequest(m.processingD2HCmd)
				}
			}
		}
	case *protocol.MemCopyH2DReq:
		madeProgress = m.processMemCopyH2DReturn(originalReq)
	case *protocol.MemCopyD2HReq:
		madeProgress = m.processMemCopyD2HReturn(originalReq)
	}

	return madeProgress
}

func (m *defaultMemoryCopyMiddleware) processMemCopyH2DReturn(
	req *protocol.MemCopyH2DReq,
) bool {
	m.driver.gpuPort.RetrieveIncoming()

	m.driver.logTaskToGPUClear(req)

	cmd, cmdQueue := m.driver.findCommandByReq(req)

	copyCmd := cmd.(*MemCopyH2DCommand)
	newReqs := make([]sim.Msg, 0, len(copyCmd.Reqs)-1)
	for _, r := range copyCmd.GetReqs() {
		if r != req {
			newReqs = append(newReqs, r)
		}
	}
	copyCmd.Reqs = newReqs

	if len(copyCmd.Reqs) == 0 {
		cmdQueue.IsRunning = false
		cmdQueue.Dequeue()

		m.driver.logCmdComplete(cmd)
	}

	return true
}

func (m *defaultMemoryCopyMiddleware) processMemCopyD2HReturn(
	req *protocol.MemCopyD2HReq,
) bool {
	m.driver.gpuPort.RetrieveIncoming()

	m.driver.logTaskToGPUClear(req)

	cmd, cmdQueue := m.driver.findCommandByReq(req)

	copyCmd := cmd.(*MemCopyD2HCommand)
	copyCmd.RemoveReq(req)

	if len(copyCmd.Reqs) == 0 {
		cmdQueue.IsRunning = false
		buf := bytes.NewReader(copyCmd.RawData)
		err := binary.Read(buf, binary.LittleEndian, copyCmd.Dst)
		if err != nil {
			panic(err)
		}

		cmdQueue.Dequeue()

		m.driver.logCmdComplete(copyCmd)
	}

	return true
}

func (m *defaultMemoryCopyMiddleware) processFlushReturn(
	req *protocol.FlushReq,
) bool {
	m.driver.gpuPort.RetrieveIncoming()

	m.driver.logTaskToGPUClear(req)

	cmd, _ := m.driver.findCommandByReq(req)

	cmd.RemoveReq(req)

	m.driver.logTaskToGPUClear(req)

	return true
}
