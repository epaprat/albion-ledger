// Package valuation turns captured market data into a per-item silver value with
// provenance: live market price preferred, server estimate (EMV) fallback, then
// unknown. Each value carries its source and a staleness flag (FR-003/FR-004).
package valuation

import (
	"sync"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

type key struct {
	index   int
	quality int
}

type priced struct {
	amount int64
	asOf   int64
}

// Book is the in-memory price source: latest live market price and EMV per
// item+quality. Bounded by the number of distinct items seen.
type Book struct {
	mu   sync.RWMutex
	live map[key]priced
	emv  map[key]priced
}

// NewBook creates an empty price book.
func NewBook() *Book {
	return &Book{live: map[key]priced{}, emv: map[key]priced{}}
}

// SetMarket records a live market price for item+quality.
func (b *Book) SetMarket(index, quality int, amount, asOf int64) {
	b.mu.Lock()
	b.live[key{index, quality}] = priced{amount, asOf}
	b.mu.Unlock()
}

// SetEMV records a server-estimate (EMV) value for item+quality.
func (b *Book) SetEMV(index, quality int, amount, asOf int64) {
	b.mu.Lock()
	b.emv[key{index, quality}] = priced{amount, asOf}
	b.mu.Unlock()
}

// Valuer computes the best valuation from a Book against a staleness threshold.
type Valuer struct {
	book       *Book
	staleAfter int64
}

// NewValuer creates a Valuer; staleAfter is the freshness threshold in ms.
func NewValuer(book *Book, staleAfter int64) *Valuer {
	return &Valuer{book: book, staleAfter: staleAfter}
}

// Value returns the best valuation for item+quality at nowMS.
func (v *Valuer) Value(index, quality int, nowMS int64) model.Valuation {
	k := key{index, quality}
	v.book.mu.RLock()
	live, hasLive := v.book.live[k]
	emv, hasEMV := v.book.emv[k]
	v.book.mu.RUnlock()

	switch {
	case hasLive:
		return v.mk(live.amount, model.SourceLiveMarket, live.asOf, nowMS)
	case hasEMV:
		return v.mk(emv.amount, model.SourceServerEstimate, emv.asOf, nowMS)
	default:
		return model.Valuation{Amount: 0, Source: model.SourceUnknown, AsOf: 0, Stale: false}
	}
}

func (v *Valuer) mk(amount int64, src model.ValuationSource, asOf, nowMS int64) model.Valuation {
	return model.Valuation{
		Amount: amount,
		Source: src,
		AsOf:   asOf,
		Stale:  nowMS-asOf > v.staleAfter,
	}
}
