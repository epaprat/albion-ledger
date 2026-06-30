package probe

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func verdictFor(t *testing.T, count int, completeness float64) model.Verdict {
	t.Helper()
	cov := NewCoverage(DefaultThresholds())
	// Encode the desired completeness via fields present/expected = completeness.
	for i := 0; i < count; i++ {
		cov.Add(model.CatSilver, int(completeness*100), 100)
	}
	for _, r := range cov.Rollup("s", 0) {
		if r.Category == model.CatSilver {
			return r.Verdict
		}
	}
	return ""
}

func TestVerdictMapping(t *testing.T) {
	cases := []struct {
		count int
		comp  float64
		want  model.Verdict
	}{
		{0, 0, model.VerdictNotObserved},
		{3, 0.30, model.VerdictLow},
		{3, 0.60, model.VerdictMedium},
		{3, 0.90, model.VerdictMedium}, // good fields, too few samples
		{10, 0.90, model.VerdictHigh},
		{10, 0.79, model.VerdictMedium}, // just under high threshold
	}
	for _, tc := range cases {
		if got := verdictFor(t, tc.count, tc.comp); got != tc.want {
			t.Errorf("count=%d comp=%.2f → %s, want %s", tc.count, tc.comp, got, tc.want)
		}
	}
}

func TestRollupIncludesEveryCategory(t *testing.T) {
	cov := NewCoverage(DefaultThresholds())
	cov.Add(model.CatLoot, 2, 2)
	rows := cov.Rollup("s", 0.1)
	if len(rows) != len(model.AllCategories) {
		t.Fatalf("rollup rows = %d, want %d (every category)", len(rows), len(model.AllCategories))
	}
	seen := map[model.Category]bool{}
	for _, r := range rows {
		seen[r.Category] = true
		if r.Category != model.CatLoot && r.Verdict != model.VerdictNotObserved {
			t.Errorf("unseen category %s should be not_observed, got %s", r.Category, r.Verdict)
		}
	}
	for _, c := range model.AllCategories {
		if !seen[c] {
			t.Errorf("category %s missing from rollup", c)
		}
	}
}
