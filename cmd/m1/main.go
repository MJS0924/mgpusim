// Package main is the M1 measurement driver.
//
// It runs a specified workload under a given directory config (region size)
// and writes phase-level snapshots to a parquet file.
//
// Kernel boundary: not available without driver modification (see
// TODO_PHASE2.md). Phase C uses window-only boundaries.
//
// Usage:
//
//	go run ./cmd/m1 -workload=simpleconvolution -region-size=64 \
//	  -timing -gpus 1 -disable-rtm -output-dir=results/m1/raw
package main

import (
	"flag"
	"log"
	"math/rand"
)

var (
	workloadFlag     = flag.String("workload", "simpleconvolution", "Workload name")
	regionSizeFlag   = flag.Uint64("region-size", 64, "Region size in bytes (64/256/1024/4096/16384)")
	seedFlag         = flag.Int64("seed", 42, "Random seed")
	windowCyclesFlag = flag.Uint64("window-cycles", 100000, "Phase window width in GPU cycles")
	outputDirFlag    = flag.String("output-dir", "results/m1/raw", "Output directory for parquet files")
	configIDFlag     = flag.Uint("config-id", 0, "Config ID embedded in snapshot rows")
	workloadIDFlag   = flag.Uint("workload-id", 0, "Workload ID embedded in snapshot rows")
)

func main() {
	flag.Parse()
	rand.Seed(*seedFlag)

	cfg := &m1Config{
		workload:     *workloadFlag,
		regionSize:   *regionSizeFlag,
		seed:         *seedFlag,
		windowCycles: *windowCyclesFlag,
		outputDir:    *outputDirFlag,
		configID:     uint16(*configIDFlag),
		workloadID:   uint16(*workloadIDFlag),
	}

	log.Printf("[m1] starting: workload=%s regionSize=%d seed=%d windowCycles=%d",
		cfg.workload, cfg.regionSize, cfg.seed, cfg.windowCycles)

	if err := runM1(cfg); err != nil {
		log.Fatalf("[m1] FATAL: %v", err)
	}
}