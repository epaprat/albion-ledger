package main

import (
	"context"
	"fmt"
	"os"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
)

// runBagProbe replays the capture once with the read-only bag-move probe on and
// prints the per-event [BAGMOVE] lines (from the pipeline) plus an explicit
// end-of-run summary — including observed=0 when clean (US2, FR-005).
func runBagProbe(replay string) int {
	h, err := buildHarness(true) // enables the probe
	if err != nil {
		fmt.Fprintf(os.Stderr, "soak: %v\n", err)
		return 2
	}
	n, err := h.feed(context.Background(), capture.NewReplay(replay))
	if err != nil {
		fmt.Fprintf(os.Stderr, "soak: replay: %v\n", err)
		return 2
	}
	res := h.pipe.BagProbeResult()
	if res.Observed == 0 {
		fmt.Printf("[BAGMOVE] observed=0 reuse events — no phantom bag-move in this capture (%d declarations, %d packets)\n",
			res.Declarations, n)
	} else {
		fmt.Printf("[BAGMOVE] observed=%d reuse events over %d declarations (%d packets)\n",
			res.Observed, res.Declarations, n)
	}
	return 0
}
