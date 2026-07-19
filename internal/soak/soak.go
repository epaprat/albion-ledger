// Package soak is the pure heap-growth analyzer for the long-run stability gate
// (feature 023, Principle XI). It turns a series of heap samples taken over a
// replayed capture into a PASS/FAIL verdict: memory is "bounded" when the peak
// stays under a budget AND the least-squares growth slope stays under a threshold.
//
// The slope is judged over ALL samples, never two endpoints, so a single
// garbage-collection dip or spike cannot flip the verdict. Measuring live heap
// (post-GC HeapInuse) rather than OS RSS keeps the verdict portable and isolates
// the real failure mode — an unbounded structure that only ever grows.
package soak

import "fmt"

// HeapSample is one measurement of live heap, taken after a forced GC so it
// reflects retained (not yet collected) memory. X is the progress axis the slope
// is regressed against — elapsed ms for a wall-clock soak, or cumulative events
// processed for a deterministic event-driven gate. The unit is the caller's choice;
// the slope and threshold are simply "bytes per one X unit" (research.md Decision 1).
type HeapSample struct {
	X              int64 // progress coordinate (ms elapsed, or events processed)
	HeapInuseBytes int64 // runtime.MemStats.HeapInuse after GC
}

// minSlopeSamples is the fewest samples from which a growth slope is trusted. A
// sub-second run that yields only the two endpoint samples cannot establish a trend
// — reporting it as inconclusive prevents a spurious PASS (or FAIL) from a slope fit
// to two near-coincident points. The peak-vs-budget check needs no such minimum.
const minSlopeSamples = 5

// SoakReport is the outcome of one long replay (data-model.md).
type SoakReport struct {
	Samples        []HeapSample
	PeakHeapBytes  int64
	GrowthSlope    float64 // bytes per one X unit (least-squares over all samples)
	BudgetBytes    int64
	MaxSlope       float64 // bytes per one X unit
	LoopIterations int
	Pass           bool
	Inconclusive   bool   // could not render a slope verdict (too few samples / no X spread)
	FailReason     string // non-empty when !Pass (names the breach or the inconclusive reason)
}

// Analyze computes peak + growth slope over the samples and renders the verdict.
// budgetBytes is the peak HeapInuse budget; maxSlope is the max acceptable growth
// in bytes per X unit (same X unit as the samples). A run with fewer than two
// samples, or with all samples at one X coordinate, cannot establish a slope and is
// reported as inconclusive (Pass=false, FailReason names it) rather than a false PASS.
func Analyze(samples []HeapSample, budgetBytes int64, maxSlope float64) SoakReport {
	r := SoakReport{
		Samples:     append([]HeapSample(nil), samples...),
		BudgetBytes: budgetBytes,
		MaxSlope:    maxSlope,
	}
	if len(samples) < 2 {
		r.Inconclusive = true
		r.FailReason = "inconclusive: fewer than 2 heap samples"
		if len(samples) == 1 {
			r.PeakHeapBytes = samples[0].HeapInuseBytes
		}
		return r
	}

	var peak int64
	for _, s := range samples {
		if s.HeapInuseBytes > peak {
			peak = s.HeapInuseBytes
		}
	}
	r.PeakHeapBytes = peak

	// Peak-vs-budget is valid at any sample count and takes precedence.
	if peak > budgetBytes {
		r.FailReason = fmt.Sprintf("peak %d bytes exceeds budget %d bytes", peak, budgetBytes)
		return r
	}
	if len(samples) < minSlopeSamples {
		r.Inconclusive = true
		r.FailReason = fmt.Sprintf("inconclusive: %d samples, need >= %d to judge growth (run longer / more loops)",
			len(samples), minSlopeSamples)
		return r
	}
	slope, ok := slopePerX(samples)
	if !ok {
		r.Inconclusive = true
		r.FailReason = "inconclusive: all samples share one progress coordinate (no growth axis)"
		return r
	}
	r.GrowthSlope = slope

	switch {
	case slope > maxSlope:
		r.FailReason = fmt.Sprintf("slope %.2f bytes/unit exceeds max-slope %.2f bytes/unit (unbounded growth)",
			slope, maxSlope)
	default:
		r.Pass = true
	}
	return r
}

// slopePerX is the least-squares slope of HeapInuse against the X axis, in bytes
// per X unit. Using every sample (not the endpoints) makes the estimate robust to a
// lone GC-timing outlier. ok=false when there is no X spread to regress against.
func slopePerX(samples []HeapSample) (slope float64, ok bool) {
	n := float64(len(samples))
	var sumX, sumY, sumXY, sumXX float64
	for _, s := range samples {
		x := float64(s.X)
		y := float64(s.HeapInuseBytes)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 { // all samples at the same X → no growth axis
		return 0, false
	}
	return (n*sumXY - sumX*sumY) / denom, true
}
