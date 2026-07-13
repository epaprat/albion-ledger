package soak

import (
	"strings"
	"testing"
)

const (
	mib      = 1 << 20
	budget   = 128 * mib
	maxSlope = 0.5 * mib // bytes per X unit
)

// flat series: heap oscillates around a steady level → PASS.
func TestAnalyzeFlatPasses(t *testing.T) {
	var s []HeapSample
	for i := 0; i < 20; i++ {
		h := int64(40*mib + (i%3)*mib) // 40–42 MiB, no trend
		s = append(s, HeapSample{X: int64(i), HeapInuseBytes: h})
	}
	r := Analyze(s, budget, maxSlope)
	if !r.Pass {
		t.Fatalf("flat series should PASS, got FAIL: %s (slope=%.0f)", r.FailReason, r.GrowthSlope)
	}
	if r.PeakHeapBytes != 42*mib {
		t.Errorf("peak = %d, want %d", r.PeakHeapBytes, int64(42*mib))
	}
}

// monotonic climb → FAIL naming the slope breach.
func TestAnalyzeRisingFails(t *testing.T) {
	var s []HeapSample
	for i := 0; i < 20; i++ {
		h := int64(40*mib) + int64(i)*10*mib // +10 MiB per X unit
		s = append(s, HeapSample{X: int64(i), HeapInuseBytes: h})
	}
	r := Analyze(s, 10*budget, maxSlope) // budget high so only slope trips
	if r.Pass {
		t.Fatal("rising series should FAIL")
	}
	if !strings.Contains(r.FailReason, "slope") {
		t.Errorf("FailReason should name slope, got %q", r.FailReason)
	}
}

// one GC dip in an otherwise flat series must NOT flip the verdict.
func TestAnalyzeSingleDipStaysPass(t *testing.T) {
	var s []HeapSample
	for i := 0; i < 20; i++ {
		h := int64(50 * mib)
		if i == 10 {
			h = 20 * mib // a lone collection dip
		}
		s = append(s, HeapSample{X: int64(i), HeapInuseBytes: h})
	}
	r := Analyze(s, budget, maxSlope)
	if !r.Pass {
		t.Fatalf("single dip should stay PASS, got %s", r.FailReason)
	}
}

// over budget → FAIL naming the peak, even with zero slope.
func TestAnalyzeOverBudgetFails(t *testing.T) {
	var s []HeapSample
	for i := 0; i < 10; i++ {
		s = append(s, HeapSample{X: int64(i), HeapInuseBytes: 200 * mib})
	}
	r := Analyze(s, budget, maxSlope)
	if r.Pass {
		t.Fatal("over-budget series should FAIL")
	}
	if !strings.Contains(r.FailReason, "budget") {
		t.Errorf("FailReason should name budget, got %q", r.FailReason)
	}
}

// fewer than two samples is inconclusive, never a false PASS.
func TestAnalyzeInsufficientSamples(t *testing.T) {
	r := Analyze([]HeapSample{{HeapInuseBytes: 1 * mib}}, budget, maxSlope)
	if r.Pass {
		t.Fatal("one sample must not PASS")
	}
	if !strings.Contains(r.FailReason, "inconclusive") {
		t.Errorf("want inconclusive, got %q", r.FailReason)
	}
}

// enough samples but all at one X coordinate → inconclusive (no growth axis).
func TestAnalyzeNoXSpreadInconclusive(t *testing.T) {
	var s []HeapSample
	for i := 0; i < 6; i++ {
		s = append(s, HeapSample{X: 0, HeapInuseBytes: int64(10+i) * mib})
	}
	r := Analyze(s, budget, maxSlope)
	if r.Pass {
		t.Fatal("no X spread must not PASS")
	}
	if !strings.Contains(r.FailReason, "inconclusive") {
		t.Errorf("want inconclusive, got %q", r.FailReason)
	}
}

// too few samples (below the slope minimum) → inconclusive, never a false PASS
// (guards the fast sub-second run that yields only endpoint samples).
func TestAnalyzeTooFewSamplesInconclusive(t *testing.T) {
	s := []HeapSample{
		{X: 0, HeapInuseBytes: 10 * mib},
		{X: 5, HeapInuseBytes: 90 * mib}, // near-coincident → untrustworthy slope
	}
	r := Analyze(s, budget, maxSlope)
	if r.Pass {
		t.Fatal("2 samples must not PASS a slope verdict")
	}
	if !strings.Contains(r.FailReason, "inconclusive") {
		t.Errorf("want inconclusive, got %q", r.FailReason)
	}
}
