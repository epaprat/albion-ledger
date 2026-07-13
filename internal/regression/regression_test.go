package regression

import (
	"path/filepath"
	"testing"
)

func snap(net int64, nodes int) AggregateSnapshot {
	return AggregateSnapshot{NetWorthSilver: net, TradeNet: 100, SpecNodeCount: nodes}
}

// Identical snapshots → zero diff, not regressed.
func TestDiffIdentical(t *testing.T) {
	r := Diff(snap(1000, 5), snap(1000, 5))
	if r.Regressed || len(r.Diffs) != 0 {
		t.Fatalf("identical must be clean, got %+v", r.Diffs)
	}
}

// Moved fields are each listed with baseline/current.
func TestDiffFlagsMovedFields(t *testing.T) {
	r := Diff(snap(1000, 5), snap(750, 6))
	if !r.Regressed {
		t.Fatal("moved fields must set Regressed")
	}
	if len(r.Diffs) != 2 {
		t.Fatalf("want 2 diffs, got %d: %+v", len(r.Diffs), r.Diffs)
	}
	byField := map[string]AggregateDiff{}
	for _, d := range r.Diffs {
		byField[d.Field] = d
	}
	if d := byField["NetWorthSilver"]; d.Baseline != 1000 || d.Current != 750 {
		t.Errorf("NetWorthSilver diff wrong: %+v", d)
	}
	if d := byField["SpecNodeCount"]; d.Baseline != 5 || d.Current != 6 {
		t.Errorf("SpecNodeCount diff wrong: %+v", d)
	}
}

// LoadBaseline of a missing file reports not-found without error.
func TestLoadBaselineMissing(t *testing.T) {
	_, found, err := LoadBaseline(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if found {
		t.Fatal("missing file must report found=false")
	}
}

// Establish then LoadBaseline round-trips the snapshot.
func TestEstablishRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	want := AggregateSnapshot{NetWorthSilver: 81_000_000, HoldingsTotalValue: 60_400_000, Fame: 5000, SpecNodeCount: 512}
	if err := Establish(path, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := LoadBaseline(path)
	if err != nil || !found {
		t.Fatalf("load after establish: found=%v err=%v", found, err)
	}
	if got != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
	if Diff(got, want).Regressed {
		t.Error("round-tripped snapshot should not regress against itself")
	}
}
