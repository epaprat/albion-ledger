package probe

import "github.com/epaprat/albion-ledger/internal/domain/model"

// Thresholds for the confidence verdict (research D6 defaults, config-overridable).
type Thresholds struct {
	CompletenessHigh float64 // default 0.8
	CompletenessLow  float64 // default 0.5
	MinSamplesHigh   int     // default 5
}

// DefaultThresholds returns the documented defaults (research D6).
func DefaultThresholds() Thresholds {
	return Thresholds{CompletenessHigh: 0.8, CompletenessLow: 0.5, MinSamplesHigh: 5}
}

// Coverage accumulates observations per category and produces the rollup.
type Coverage struct {
	th     Thresholds
	count  map[model.Category]int
	sumPct map[model.Category]float64
}

// NewCoverage creates a Coverage accumulator with the given thresholds.
func NewCoverage(th Thresholds) *Coverage {
	return &Coverage{
		th:     th,
		count:  make(map[model.Category]int),
		sumPct: make(map[model.Category]float64),
	}
}

// Add records one classified observation.
func (c *Coverage) Add(cat model.Category, fieldsPresent, fieldsExpected int) {
	c.count[cat]++
	if fieldsExpected > 0 {
		c.sumPct[cat] += float64(fieldsPresent) / float64(fieldsExpected)
	}
}

// Rollup produces a CategoryCoverage row for EVERY target category (never-seen
// ones included as not_observed), with the verdict per the thresholds.
func (c *Coverage) Rollup(sessionID string, encryptedRate float64) []model.CategoryCoverage {
	rows := make([]model.CategoryCoverage, 0, len(model.AllCategories))
	for _, cat := range model.AllCategories {
		n := c.count[cat]
		completeness := 0.0
		if n > 0 {
			completeness = c.sumPct[cat] / float64(n)
		}
		rows = append(rows, model.CategoryCoverage{
			SessionID:     sessionID,
			Category:      cat,
			ObservedCount: n,
			Completeness:  completeness,
			EncryptedRate: encryptedRate,
			Verdict:       c.verdict(n, completeness),
		})
	}
	return rows
}

// verdict maps (count, completeness) to a confidence verdict (research D6).
func (c *Coverage) verdict(count int, completeness float64) model.Verdict {
	switch {
	case count == 0:
		return model.VerdictNotObserved
	case completeness < c.th.CompletenessLow:
		return model.VerdictLow
	case completeness < c.th.CompletenessHigh:
		return model.VerdictMedium
	case count < c.th.MinSamplesHigh:
		// Good fields but too few samples to be confident.
		return model.VerdictMedium
	default:
		return model.VerdictHigh
	}
}
