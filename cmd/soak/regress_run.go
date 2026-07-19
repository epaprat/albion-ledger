package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/regression"
)

// runRegress replays the golden capture, snapshots the headline aggregates, and
// either establishes the baseline (-update / first run) or diffs against it (US3).
func runRegress(replay, baselinePath string, update bool) int {
	h, err := buildHarness(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "soak: %v\n", err)
		return 2
	}
	if _, err := h.feed(context.Background(), capture.NewReplay(replay)); err != nil {
		fmt.Fprintf(os.Stderr, "soak: replay: %v\n", err)
		return 2
	}
	cur := regression.Snapshot(h.svc)
	golden := filepath.Base(replay)

	if update {
		if err := regression.Establish(baselinePath, cur); err != nil {
			fmt.Fprintf(os.Stderr, "soak: establish: %v\n", err)
			return 2
		}
		fmt.Printf("[REGRESS] golden=%s ESTABLISHED baseline (9 aggregates) — -update\n", golden)
		return 0
	}

	base, found, err := regression.LoadBaseline(baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "soak: baseline: %v\n", err)
		return 2
	}
	if !found {
		if err := regression.Establish(baselinePath, cur); err != nil {
			fmt.Fprintf(os.Stderr, "soak: establish: %v\n", err)
			return 2
		}
		fmt.Printf("[REGRESS] golden=%s ESTABLISHED baseline (9 aggregates) — first run\n", golden)
		return 0
	}

	res := regression.Diff(base, cur)
	if res.Regressed {
		parts := make([]string, 0, len(res.Diffs))
		for _, d := range res.Diffs {
			parts = append(parts, fmt.Sprintf("%s %d->%d", d.Field, d.Baseline, d.Current))
		}
		fmt.Printf("[REGRESS] golden=%s aggregates=9 diffs=%d REGRESSED: %s\n",
			golden, len(res.Diffs), strings.Join(parts, ", "))
		return 1
	}
	fmt.Printf("[REGRESS] golden=%s aggregates=9 diffs=0 PASS\n", golden)
	return 0
}
