package drift

import (
	"strings"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func cov(pairs map[model.Category]int) []model.CategoryCoverage {
	var out []model.CategoryCoverage
	for c, n := range pairs {
		out = append(out, model.CategoryCoverage{Category: c, ObservedCount: n})
	}
	return out
}

func TestSilentCategoryAlerts(t *testing.T) {
	w := New()
	// Establish baseline: loot + silver active.
	w.Observe(cov(map[model.Category]int{model.CatLoot: 5, model.CatSilver: 3}), 0.5)
	if w.Alert() != "" {
		t.Fatalf("healthy baseline should not alert: %q", w.Alert())
	}
	// Loot goes silent while silver still active → drift.
	w.Observe(cov(map[model.Category]int{model.CatLoot: 0, model.CatSilver: 4}), 0.5)
	if a := w.Alert(); a == "" || !strings.Contains(a, string(model.CatLoot)) {
		t.Fatalf("expected loot-silent alert, got %q", a)
	}
}

func TestUnhandledSpikeAlerts(t *testing.T) {
	w := New()
	w.Observe(cov(map[model.Category]int{model.CatLoot: 1}), 0.998)
	if a := w.Alert(); !strings.Contains(strings.ToLower(a), "unhandled") {
		t.Fatalf("expected unhandled-spike alert, got %q", a)
	}
}

func TestHealthyNoAlert(t *testing.T) {
	w := New()
	w.Observe(cov(map[model.Category]int{model.CatLoot: 2, model.CatSilver: 2}), 0.6)
	w.Observe(cov(map[model.Category]int{model.CatLoot: 3, model.CatSilver: 3}), 0.6)
	if w.Alert() != "" {
		t.Fatalf("healthy should not alert: %q", w.Alert())
	}
}
