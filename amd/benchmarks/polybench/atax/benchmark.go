// Package atax implements the ATAX benchmark from Polybench.
package atax

import (
	"log"
	"math"
	"math/rand"

	// embed hsaco files
	_ "embed"

	"github.com/sarchlab/mgpusim/v4/amd/driver"
	"github.com/sarchlab/mgpusim/v4/amd/insts"
	"github.com/sarchlab/mgpusim/v4/amd/kernels"
)

// Kernel1Args list first set of kernel arguments
type Kernel1Args struct {
	A                   driver.Ptr
	X                   driver.Ptr
	Tmp                 driver.Ptr
	NX                  int32
	NY                  int32
	HiddenGlobalOffsetX int64
	HiddenGlobalOffsetY int64
	HiddenGlobalOffsetZ int64
}

// Kernel2Args list second set of kernel arguments
type Kernel2Args struct {
	A                   driver.Ptr
	Y                   driver.Ptr
	Tmp                 driver.Ptr
	NX, NY              int32
	HiddenGlobalOffsetX int64
	HiddenGlobalOffsetY int64
	HiddenGlobalOffsetZ int64
}

// Benchmark defines a benchmark
type Benchmark struct {
	driver           *driver.Driver
	context          *driver.Context
	gpus             []int
	queues           []*driver.CommandQueue
	kernel1, kernel2 *insts.HsaCo

	NX, NY                int
	a, x, y, yOutput, tmp []float32
	dA, dX, dY, dTmp      driver.Ptr
	cpuY                  []float32

	useUnifiedMemory bool
}

// NewBenchmark makes a new benchmark
func NewBenchmark(driver *driver.Driver) *Benchmark {
	b := new(Benchmark)
	b.driver = driver
	b.context = driver.Init()
	b.loadProgram()
	return b
}

// SelectGPU selects GPU
func (b *Benchmark) SelectGPU(gpus []int) {
	b.gpus = gpus
}

// SetUnifiedMemory uses Unified Memory
func (b *Benchmark) SetUnifiedMemory() {
	b.useUnifiedMemory = true
}

//go:embed kernels.hsaco
var hsacoBytes []byte

func (b *Benchmark) loadProgram() {
	b.kernel1 = kernels.LoadProgramFromMemory(
		hsacoBytes, "atax_kernel1")
	if b.kernel1 == nil {
		log.Panic("Failed to load kernel binary")
	}

	b.kernel2 = kernels.LoadProgramFromMemory(
		hsacoBytes, "atax_kernel2")
	if b.kernel2 == nil {
		log.Panic("Failed to load kernel binary")
	}
}

// Run runs
func (b *Benchmark) Run() {
	for _, gpu := range b.gpus {
		b.driver.SelectGPU(b.context, gpu)
		b.queues = append(b.queues, b.driver.CreateCommandQueue(b.context))
	}

	b.initMem()
	b.exec()
}

func (b *Benchmark) initMem() {
	rand.Seed(1)
	b.a = make([]float32, b.NX*b.NY)
	b.x = make([]float32, b.NX)
	b.y = make([]float32, b.NY)
	b.yOutput = make([]float32, b.NY)
	b.tmp = make([]float32, b.NX)

	for i := 0; i < b.NX; i++ {
		b.x[i] = float32(i) * math.Pi
		for j := 0; j < b.NY; j++ {
			b.a[i*b.NY+j] = float32(i) * float32(j) / float32(b.NX)
		}
	}

	if b.useUnifiedMemory {
		b.dA = b.driver.AllocateUnifiedMemory(b.context,
			uint64(b.NY*b.NX*4))
		b.dX = b.driver.AllocateUnifiedMemory(b.context,
			uint64(b.NY*4))
		b.dY = b.driver.AllocateUnifiedMemory(b.context,
			uint64(b.NY*4))
		b.dTmp = b.driver.AllocateUnifiedMemory(b.context,
			uint64(b.NX*4))
	} else {
		b.dA = b.driver.AllocateMemory(b.context,
			uint64(b.NY*b.NX*4))
		b.dX = b.driver.AllocateMemory(b.context,
			uint64(b.NY*4))
		b.dY = b.driver.AllocateMemory(b.context,
			uint64(b.NY*4))
		b.dTmp = b.driver.AllocateMemory(b.context,
			uint64(b.NX*4))
	}
}

func (b *Benchmark) exec() {
	b.driver.MemCopyH2D(b.context, b.dA, b.a)
	b.driver.MemCopyH2D(b.context, b.dX, b.x)

	localSize := [3]uint16{256, 1, 1}
	nGPU := len(b.gpus)
	// 각 GPU가 동일한 work-item을 받도록 (localSize × nGPU) 단위로 정렬
	alignment := 256 * nGPU

	// ===== Kernel1: NX 차원에서 병렬 (tmp[i] = A[i,*]·x) =====
	totalNX := ((b.NX-1)/alignment + 1) * alignment
	perGPUNX := totalNX / nGPU

	for i, gpu := range b.gpus {
		b.driver.SelectGPU(b.context, gpu)

		kernel1Arg := Kernel1Args{
			A:                   b.dA,
			X:                   b.dX,
			Tmp:                 b.dTmp,
			NX:                  int32(b.NX),
			NY:                  int32(b.NY),
			HiddenGlobalOffsetX: int64(i * perGPUNX),
		}
		b.driver.EnqueueLaunchKernel(
			b.queues[i],
			b.kernel1,
			[3]uint32{uint32(perGPUNX), 1, 1},
			localSize,
			&kernel1Arg,
		)
	}

	// kernel2 가 tmp 를 읽기 전에 kernel1 결과 동기화 필수
	for _, q := range b.queues {
		b.driver.DrainCommandQueue(q)
	}

	// ===== Kernel2: NY 차원에서 병렬 (y[j] = A[*,j]ᵀ·tmp) =====
	totalNY := ((b.NY-1)/alignment + 1) * alignment
	perGPUNY := totalNY / nGPU

	for i, gpu := range b.gpus {
		b.driver.SelectGPU(b.context, gpu)

		kernel2Arg := Kernel2Args{
			A:                   b.dA,
			Y:                   b.dY,
			Tmp:                 b.dTmp,
			NX:                  int32(b.NX),
			NY:                  int32(b.NY),
			HiddenGlobalOffsetX: int64(i * perGPUNY),
		}
		b.driver.EnqueueLaunchKernel(
			b.queues[i],
			b.kernel2,
			[3]uint32{uint32(perGPUNY), 1, 1},
			localSize,
			&kernel2Arg,
		)
	}

	for _, q := range b.queues {
		b.driver.DrainCommandQueue(q)
	}

	b.driver.MemCopyD2H(b.context, b.yOutput, b.dY)
}

// Verify verifies
func (b *Benchmark) Verify() {
	b.cpuAtax()

	for i := 0; i < b.NY; i++ {
		if b.cpuY[i] != b.yOutput[i] {
			log.Panicf("Mismatch at %d, expected %f, but get %f",
				i, b.cpuY[i], b.yOutput[i])
		}
	}

	log.Printf("Passed!\n")
}

func (b *Benchmark) cpuAtax() {
	b.cpuY = make([]float32, b.NY)
	tmp := make([]float32, b.NX)

	for i := 0; i < b.NY; i++ {
		b.cpuY[i] = 0
	}

	for i := 0; i < b.NX; i++ {
		tmp[i] = 0

		for j := 0; j < b.NY; j++ {
			tmp[i] += b.a[i*b.NY+j] * b.x[j]
		}

		for j := 0; j < b.NY; j++ {
			b.cpuY[j] += b.a[i*b.NY+j] * tmp[i]
		}
	}
}
