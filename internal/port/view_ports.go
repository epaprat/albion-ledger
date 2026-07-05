package port

import (
	"context"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// Catalog resolves a numeric item index to an item identity (FR-001/FR-007).
type Catalog interface {
	// Resolve never errors: an unknown index yields a safe placeholder
	// (Known=false, DisplayName "Unknown item #N").
	Resolve(index int, quality int) model.Item
	// IndexOf resolves a uniqueName to its catalog index (market order feeds
	// identify items by name — feature 010); ok=false for unknown names.
	IndexOf(uniqueName string) (int, bool)
	// Reload swaps in a new catalog file at runtime (FR-012); a malformed file
	// is rejected and the previous catalog is kept.
	Reload(path string) error
}

// Valuer produces the best valuation for an item+quality from captured data (FR-003/FR-004).
type Valuer interface {
	Value(index int, quality int, nowMS int64) model.Valuation
}

// CodeRegistry is the data-driven code→category map (FR-012).
type CodeRegistry interface {
	// Lookup returns the category and optional guard key for a (kind, code).
	Lookup(kind string, code int) (category model.Category, guardKey int, hasGuard bool, ok bool)
	Reload(path string) error
}

// DriftWatcher flags when a normally-active category goes silent or the
// unhandled rate spikes — the signal that a patch shifted codes (FR-012).
type DriftWatcher interface {
	Observe(coverage []model.CategoryCoverage, unhandledRate float64)
	Alert() string // "" when healthy
}

// ExternalPrice is one community-feed price point (silver, plain units).
type ExternalPrice struct {
	UniqueName string
	Quality    int
	Silver     int64
}

// PriceFetcher pulls community prices for catalog uniqueNames (AODP adapter, 010).
// Implementations must be network-failure-tolerant; callers treat errors as "no data".
type PriceFetcher interface {
	Fetch(ctx context.Context, names []string) ([]ExternalPrice, error)
}
