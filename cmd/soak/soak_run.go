package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/soak"
)

// sampleInterval is how often the heap is sampled during a soak. 1s keeps the
// forced-GC cost negligible against a minutes-to-hours run.
const sampleInterval = time.Second

// runSoak loops the capture to the target and reports the memory verdict (US1).
func runSoak(replay string, dur time.Duration, events, budgetMB int, maxSlopeMB float64, heapOut string) int {
	h, err := buildHarness(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "soak: %v\n", err)
		return 2
	}
	src := capture.NewLoopReplay(replay, events, dur)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Baseline sample BEFORE launching the feed, so the X=0 point reflects the
	// pre-replay heap — sampling it after the goroutine starts would let early
	// allocations inflate the origin and bias the growth slope low (review 023).
	start := time.Now()
	samples := []soak.HeapSample{soak.Sample(0)}

	type feedResult struct {
		n   int
		err error
	}
	done := make(chan feedResult, 1)
	go func() {
		n, err := h.feed(ctx, src)
		done <- feedResult{n, err}
	}()

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	processed := 0
loop:
	for {
		select {
		case fr := <-done:
			if fr.err != nil {
				fmt.Fprintf(os.Stderr, "soak: replay: %v\n", fr.err)
				return 2
			}
			processed = fr.n
			break loop
		case <-ticker.C:
			samples = append(samples, soak.Sample(time.Since(start).Milliseconds()))
		}
	}
	samples = append(samples, soak.Sample(time.Since(start).Milliseconds()))

	// X is elapsed ms → convert the MiB/min budget-slope to bytes/ms for Analyze.
	maxSlopeBytesPerMS := maxSlopeMB * (1 << 20) / 60_000
	rep := soak.Analyze(samples, int64(budgetMB)<<20, maxSlopeBytesPerMS)
	rep.LoopIterations = src.Iterations()

	if heapOut != "" {
		if err := writeHeapProfile(heapOut); err != nil {
			fmt.Fprintf(os.Stderr, "soak: heap-out: %v\n", err)
		}
	}

	verdict := "PASS"
	switch {
	case rep.Inconclusive:
		verdict = "INCONCLUSIVE: " + rep.FailReason
	case !rep.Pass:
		verdict = "FAIL: " + rep.FailReason
	}
	slopeMiBPerMin := rep.GrowthSlope * 60_000 / (1 << 20) // bytes/ms → MiB/min
	fmt.Printf("[SOAK] loops=%d events=%d elapsed=%s peak=%s slope=%+.2fMiB/min budget=%dMiB max-slope=%.2fMiB/min %s\n",
		rep.LoopIterations, processed, time.Since(start).Round(time.Second),
		mib(rep.PeakHeapBytes), slopeMiBPerMin,
		budgetMB, maxSlopeMB, verdict)

	switch {
	case rep.Pass:
		return 0
	case rep.Inconclusive:
		return 2 // could not verify (not a leak) — distinct from a real FAIL
	default:
		return 1
	}
}

func mib(b int64) string { return fmt.Sprintf("%.1fMiB", float64(b)/(1<<20)) }

func writeHeapProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	runtime.GC()
	return pprof.WriteHeapProfile(f)
}
