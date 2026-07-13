package app

// Always-on memory-flat gate (feature 023, US1 / Principle XI.199). Loops the
// committed golden capture through the REAL pipeline many times, sampling retained
// heap against cumulative events processed (a deterministic progress axis, immune to
// wall-clock jitter), and asserts the growth slope stays bounded. A leak — any
// unbounded map/cache — makes the slope climb and fails this test.

import (
	"context"
	"testing"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/soak"
)

func TestSoakMemoryFlat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak in -short")
	}
	svc, parser := newSoakHarness(t)
	_ = svc
	fixture := writeGoldenFixture(t)

	const (
		loops       = 400 // passes over the fixture
		sampleEvery = 20  // sample heap every N loops
		// Budget/slope are generous: the fixture is tiny, so a correct build sits far
		// under both. They exist to catch a monotonic climb, not to pin an exact size.
		budgetBytes = int64(128) << 20 // 128 MiB peak
		maxSlope    = 4096.0           // bytes per event processed
	)

	var samples []soak.HeapSample
	events := 0
	for i := 0; i < loops; i++ {
		ch, err := capture.NewReplay(fixture).Packets(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for payload := range ch {
			parser.ReceivePacket(payload)
			events++
		}
		if i%sampleEvery == 0 {
			samples = append(samples, soak.Sample(int64(events)))
		}
	}
	samples = append(samples, soak.Sample(int64(events)))

	rep := soak.Analyze(samples, budgetBytes, maxSlope)
	if !rep.Pass {
		t.Fatalf("memory soak FAILED: %s\n  peak=%d bytes slope=%.2f bytes/event over %d events, %d samples",
			rep.FailReason, rep.PeakHeapBytes, rep.GrowthSlope, events, len(samples))
	}
	t.Logf("soak PASS: peak=%.1fMiB slope=%.2f bytes/event over %d events (%d loops, %d samples)",
		float64(rep.PeakHeapBytes)/(1<<20), rep.GrowthSlope, events, loops, len(samples))
}
