package main

import (
	"fmt"

	"github.com/sarchlab/mgpusim/v4/coherence"
	"github.com/sarchlab/mgpusim/v4/instrument"
)

// m1Config holds the full configuration for one M1 measurement run.
type m1Config struct {
	workload     string
	regionSize   uint64
	seed         int64
	windowCycles uint64
	gpus         int
	outputDir    string
	maxEntries   int
	// configID / workloadID are derived from CLI input at startup.
	configID   uint16
	workloadID uint16
}

// dirCfg returns the coherence.DirectoryConfig for this run.
// When maxEntries > 0, finite LRU mode is used; otherwise infinite capacity.
func (c *m1Config) dirCfg() coherence.DirectoryConfig {
	if c.maxEntries > 0 {
		return coherence.DirectoryConfig{
			RegionSizeBytes:  c.regionSize,
			InfiniteCapacity: false,
			MaxEntries:       c.maxEntries,
		}
	}
	return coherence.DirectoryConfig{
		RegionSizeBytes:  c.regionSize,
		InfiniteCapacity: true,
	}
}

// initialPhaseID returns the initial PhaseID for this run.
func (c *m1Config) initialPhaseID() instrument.PhaseID {
	return instrument.PhaseID{
		ConfigID:   c.configID,
		WorkloadID: c.workloadID,
		Index:      0,
	}
}

// outputPath returns the parquet output file path.
// Convention: {outputDir}/{workload}_R{regionSize}_cap{maxEntries}_seed{seed}.parquet
// cap0 means infinite capacity.
func (c *m1Config) outputPath() string {
	return fmt.Sprintf("%s/%s_R%d_cap%d_seed%d.parquet",
		c.outputDir, c.workload, c.regionSize, c.maxEntries, c.seed)
}