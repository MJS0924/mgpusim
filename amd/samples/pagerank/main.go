package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sarchlab/mgpusim/v4/amd/benchmarks/heteromark/pagerank"
	"github.com/sarchlab/mgpusim/v4/amd/samples/runner"
)

var numNode = flag.Int("node", 32768, "The number of nodes")
var sparsity = flag.Float64("sparsity", 0.2, "The sparsity of the graph")
var maxIterations = flag.Int("iterations", 16, "The number of iterations")
var randSeed = flag.Int64("rand-seed", 42, "Random seed for graph generation (use same seed across REC/HMG/SuperDirectory for fair comparison)")

func main() {
	flag.Parse()

	runner := new(runner.Runner).Init()

	benchmark := pagerank.NewBenchmark(runner.Driver())
	benchmark.NumNodes = uint32(*numNode)

	if *sparsity > 1 {
		*sparsity = 1
	}
	numConn := int(float64(*numNode**numNode) * *sparsity)
	if numConn < *numNode {
		numConn = *numNode
	}
	fmt.Fprintf(os.Stderr,
		"Number node %d, number connection %d\n", *numNode, numConn)

	benchmark.NumConnections = uint32(numConn)
	benchmark.MaxIterations = uint32(*maxIterations)
	benchmark.RandSeed = *randSeed

	runner.AddBenchmark(benchmark)

	runner.Run()
}
