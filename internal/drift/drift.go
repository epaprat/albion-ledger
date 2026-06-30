// Package drift detects when capture coverage degrades — a normally-active
// category goes silent or the unhandled-code rate spikes — the signal that a
// game patch shifted codes (FR-012). It never blocks capture; it only reports.
package drift

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// unhandledSpike is the rate above which nearly everything is unclassified —
// a strong hint that codes moved.
const unhandledSpike = 0.99

// Watcher tracks an ever-active baseline and flags regressions.
type Watcher struct {
	mu       sync.Mutex
	baseline map[model.Category]bool // categories seen active at least once
	alert    string
}

// New creates a Watcher.
func New() *Watcher {
	return &Watcher{baseline: map[model.Category]bool{}}
}

// Observe records the latest coverage snapshot + unhandled rate and recomputes
// the alert.
func (w *Watcher) Observe(coverage []model.CategoryCoverage, unhandledRate float64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	current := map[model.Category]int{}
	anyActive := false
	for _, c := range coverage {
		current[c.Category] = c.ObservedCount
		if c.ObservedCount > 0 {
			w.baseline[c.Category] = true
			anyActive = true
		}
	}

	var silent []string
	if anyActive {
		for cat := range w.baseline {
			if current[cat] == 0 {
				silent = append(silent, string(cat))
			}
		}
	}
	sort.Strings(silent)

	var msgs []string
	if len(silent) > 0 {
		msgs = append(msgs, fmt.Sprintf("categories went silent (possible patch): %s", strings.Join(silent, ", ")))
	}
	if unhandledRate >= unhandledSpike {
		msgs = append(msgs, fmt.Sprintf("unhandled rate spiked to %.1f%% — codes may have shifted", unhandledRate*100))
	}
	w.alert = strings.Join(msgs, "; ")
}

// Alert returns the current drift message, or "" when healthy.
func (w *Watcher) Alert() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alert
}
