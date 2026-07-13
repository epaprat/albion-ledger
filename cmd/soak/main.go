// Command soak is the headless self-verification harness for feature 023. It runs
// the real capture pipeline over a recorded pcap — no live game, no webview, no
// elevated privileges — and emits a named verdict for one of three checks:
//
//	soak -mode soak     -replay f.pcap   # memory-flat gate (Principle XI)
//	soak -mode bagprobe -replay f.pcap   # phantom bag-move probe (US2)
//	soak -mode regress  -replay f.pcap   # aggregate regression vs baseline (US3)
//
// Exit codes: 0 PASS/established, 1 FAIL (verdict breached), 2 usage/IO error.
// See specs/023-soak-selfcheck/contracts/ for the exact output formats.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	mode := flag.String("mode", "", "check to run: soak | bagprobe | regress")
	replay := flag.String("replay", "", "pcap to replay (looped for -mode soak)")
	duration := flag.Duration("duration", 5*time.Minute, "soak: target wall-clock (looped)")
	events := flag.Int("events", 0, "soak: target processed-event count (alt to -duration)")
	budgetMB := flag.Int("budget-mb", 256, "soak: peak HeapInuse budget in MiB")
	maxSlopeMB := flag.Float64("max-slope", 0.5, "soak: max growth slope in MiB/min")
	heapOut := flag.String("heap-out", "", "soak: optional path to write a pprof heap profile")
	baseline := flag.String("baseline", "internal/app/testdata/regression_baseline.json", "regress: baseline json path (shared with the go-test gate)")
	update := flag.Bool("update", false, "regress: (re)establish the baseline from this run")
	flag.Parse()

	if *replay == "" {
		fmt.Fprintln(os.Stderr, "soak: -replay is required")
		os.Exit(2)
	}
	switch *mode {
	case "soak":
		os.Exit(runSoak(*replay, *duration, *events, *budgetMB, *maxSlopeMB, *heapOut))
	case "bagprobe":
		os.Exit(runBagProbe(*replay))
	case "regress":
		os.Exit(runRegress(*replay, *baseline, *update))
	default:
		fmt.Fprintf(os.Stderr, "soak: unknown -mode %q (want soak|bagprobe|regress)\n", *mode)
		os.Exit(2)
	}
}
