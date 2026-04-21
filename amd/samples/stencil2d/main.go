package main

import (
	"flag"

	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/shoc/stencil2d"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner"
)

var numRow = flag.Int("row", 2048, "The number of rows in the input matrix.")
var numCol = flag.Int("col", 2048, "The number of columns in the input matrix.")
var numIter = flag.Int("iter", 20, "The number of iterations to run.")

func main() {
	flag.Parse()

	runner := new(runner.Runner).Init()

	benchmark := stencil2d.NewBenchmark(runner.Driver())
	benchmark.NumIteration = *numIter
	benchmark.NumRows = *numRow + 2
	benchmark.NumCols = *numCol + 2

	runner.AddBenchmark(benchmark)

	runner.Run()
}
