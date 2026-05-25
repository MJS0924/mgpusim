// Package minerva implements minerva network training.
package minerva

import (
	"math"

	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/dnn/gputensor"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/mccl"

	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/dnn/dataset/mnist"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/dnn/gputraining"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/dnn/layers"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/dnn/training"
	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/dnn/training/optimization"
	"github.com/sarchlab/mgpusim/v4/amd/driver"
)

// Benchmark defines the Mineva network training benchmark.
type Benchmark struct {
	driver           *driver.Driver
	ctx              *driver.Context
	to               []*gputensor.GPUOperator
	gpus             []int
	contexts         []*driver.Context
	useUnifiedMemory bool

	networks []training.Network
	trainer  gputraining.DataParallelismMultiGPUTrainer

	BatchSize          int
	Epoch              int
	MaxBatchPerEpoch   int
	EnableTesting      bool
	EnableVerification bool
}

// NewBenchmark creates a new benchmark.
func NewBenchmark(driver *driver.Driver) *Benchmark {
	b := new(Benchmark)

	b.driver = driver
	b.ctx = driver.Init()

	return b
}

// SelectGPU selects the GPU to use.
func (b *Benchmark) SelectGPU(gpuIDs []int) {
	b.gpus = gpuIDs
}

func (b *Benchmark) init() {
	for _, gpu := range b.gpus {
		b.defineNetwork(gpu)
	}

	b.createTrainer()
	b.randomizeParams()
}

func (b *Benchmark) defineNetwork(gpuID int) {
	context := b.driver.InitWithExistingPID(b.ctx)
	b.driver.SelectGPU(context, gpuID)
	to := gputensor.NewGPUOperator(b.driver, context)

	if b.EnableVerification {
		to.EnableVerification()
	}

	if b.useUnifiedMemory {
		to.EnableUnifiedMemory()
	}

	// Enlarged hidden dimensions to push the working set beyond per-GPU L2
	// capacity (~4 MB) so directory pressure and coherence variant behavior
	// actually exercise differently across CD/SD/REC/HMG. The original
	// 256/100/100 hidden dims kept the entire model + activations resident
	// in L2 and produced near-identical per-variant results.
	//
	// Hidden 2048 (downsized from 4096 to bring single-batch wall time to
	// ~1 day while keeping working set ≫ L2):
	//   FC0  784×2048 + 2048      ≈ 1.6 M params (~6.4 MB)
	//   FC2  2048×2048 + 2048     ≈ 4.2 M params (~16.8 MB)
	//   FC4  2048×2048 + 2048     ≈ 4.2 M params (~16.8 MB)
	//   FC6  2048×10 + 10         ≈  20 K params
	//   Total                    ≈ 10 M params (~40 MB)
	//   × 4 (params + grads + Adam V + S) ≈ 160 MB per GPU
	// GEMM compute (FC2/FC4): 2048³ = 8.6 GFLOPs vs prior 4096³ = 68.7 GFLOPs
	//   → 8× less compute per heavy kernel, 4× less weight traffic.
	network := training.Network{
		Layers: []layers.Layer{
			layers.NewFullyConnectedLayer(0, to, 784, 2048),
			layers.NewReluLayer(to),
			layers.NewFullyConnectedLayer(2, to, 2048, 2048),
			layers.NewReluLayer(to),
			layers.NewFullyConnectedLayer(4, to, 2048, 2048),
			layers.NewReluLayer(to),
			layers.NewFullyConnectedLayer(6, to, 2048, 10),
		},
	}

	b.networks = append(b.networks, network)
	b.contexts = append(b.contexts, context)
	b.to = append(b.to, to)
}

func (b *Benchmark) createTrainer() {
	sources := make([]training.DataSource, len(b.networks))
	alg := make([]optimization.Alg, len(b.networks))
	testers := make([]*training.Tester, len(b.networks))
	lossFuncs := make([]training.LossFunction, len(b.networks))

	for i := 0; i < len(b.networks); i++ {
		sources[i] = mnist.NewTrainingDataSource(b.to[i])
		alg[i] = optimization.NewAdam(b.to[i], 0.001)
		lossFuncs[i] = training.NewSoftmaxCrossEntropy(b.to[i])

		if b.EnableTesting {
			testers[i] = &training.Tester{
				DataSource: mnist.NewTestDataSource(b.to[i]),
				Network:    b.networks[i],
				BatchSize:  math.MaxInt32,
			}
		}
	}

	b.trainer = gputraining.DataParallelismMultiGPUTrainer{
		TensorOperators:  b.to,
		DataSource:       sources,
		Networks:         b.networks,
		LossFunc:         lossFuncs,
		OptimizationAlg:  alg,
		Tester:           testers,
		Epoch:            b.Epoch,
		MaxBatchPerEpoch: b.MaxBatchPerEpoch,
		BatchSize:        b.BatchSize,
		ShowBatchInfo:    true,
		GPUs:             b.gpus,
		Contexts:         b.contexts,
		Driver:           b.driver,
	}
}

func (b *Benchmark) randomizeParams() {
	initNet := b.networks[0]
	for _, l := range initNet.Layers {
		l.Randomize()
	}

	gpuNum := len(b.networks)

	for i := range b.networks[0].Layers {
		if b.networks[0].Layers[i].Parameters() == nil {
			continue
		}

		params := make([]*gputensor.Tensor, gpuNum)
		datas := make([]driver.Ptr, gpuNum)

		for j := 0; j < gpuNum; j++ {
			params[j] = b.networks[j].Layers[i].Parameters().(*gputensor.Tensor)
		}

		dataSizeArr := params[0].Size()
		dataSize := 1
		for i := 0; i < len(dataSizeArr); i++ {
			dataSize *= dataSizeArr[i]
		}

		for i := 0; i < len(params); i++ {
			datas[i] = params[i].Ptr()
		}
		comms := mccl.CommInitAllMultipleContexts(
			gpuNum, b.driver, b.contexts, b.gpus)
		mccl.BroadcastRing(b.driver, comms, 1, datas, dataSize)
	}
}

// Run executes the benchmark.
func (b *Benchmark) Run() {
	b.init()
	b.trainer.Train()
}

// Verify runs the benchmark on the CPU and checks the result.
func (b *Benchmark) Verify() {
	panic("not implemented")
}

// SetUnifiedMemory asks the benchmark to use unified memory.
func (b *Benchmark) SetUnifiedMemory() {
	b.useUnifiedMemory = true
}
