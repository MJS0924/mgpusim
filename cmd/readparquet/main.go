package main

import (
	"flag"
	"fmt"
	"os"

	parquet "github.com/parquet-go/parquet-go"
	"github.com/sarchlab/mgpusim/v4/instrument/adapter"
)

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: readparquet <file.parquet>")
		os.Exit(1)
	}
	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	reader := parquet.NewGenericReader[adapter.ParquetSnapshot](f)
	defer reader.Close()

	buf := make([]adapter.ParquetSnapshot, 1000)
	for {
		n, err := reader.Read(buf)
		for i := 0; i < n; i++ {
			r := buf[i]
			fmt.Printf("phase=%d start=%d end=%d L2H=%d L2M=%d fetched=%d accessed=%d activeR=%d retiredWf=%d evict=%d\n",
				r.PhaseIndex, r.StartCycle, r.EndCycle,
				r.L2Hits, r.L2Misses, r.RegionFetchedBytes, r.RegionAccessedBytes,
				r.ActiveRegions, r.RetiredWavefronts, r.DirectoryEvictions)
		}
		if err != nil {
			break
		}
	}
}