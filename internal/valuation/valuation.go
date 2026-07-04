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

// EMVEntry is one persisted server-estimate row (010: the book survives restarts so
// values learned from declarations keep pricing K-overview summary rows next session).
type EMVEntry struct {
	Index, Quality int
	Amount, AsOf   int64
}

// SnapshotEMV returns all recorded server estimates (for persistence).
func (b *Book) SnapshotEMV() []EMVEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]EMVEntry, 0, len(b.emv))
	for k, p := range b.emv {
		out = append(out, EMVEntry{Index: k.index, Quality: k.quality, Amount: p.amount, AsOf: p.asOf})
	}
	return out
}

// RestoreEMV loads persisted server estimates without touching newer live data.
func (b *Book) RestoreEMV(entries []EMVEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range entries {
		k := key{e.Index, e.Quality}
		if cur, ok := b.emv[k]; !ok || e.AsOf > cur.asOf {
			b.emv[k] = priced{e.Amount, e.AsOf}
		}
	}
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
	}

	// Quality-0 fallback (010): type-based summary rows (K bank overview) don't know
	// the item's quality, but the book is quality-keyed — a normal-quality miss left
	// clearly-valued items showing as unvalued (live "EMV missing" report). Any
	// recorded quality beats nothing; the LOWEST recorded quality wins so the
	// estimate stays conservative.
	if quality == 0 {
		v.book.mu.RLock()
		defer v.book.mu.RUnlock()
		for q := 1; q <= 5; q++ {
			if e, ok := v.book.live[key{index, q}]; ok {
				return v.mk(e.amount, model.SourceLiveMarket, e.asOf, nowMS)
			}
		}
		for q := 1; q <= 5; q++ {
			if e, ok := v.book.emv[key{index, q}]; ok {
				return v.mk(e.amount, model.SourceServerEstimate, e.asOf, nowMS)
			}
		}
	}
	return model.Valuation{Amount: 0, Source: model.SourceUnknown, AsOf: 0, Stale: false}
}

func (v *Valuer) mk(amount int64, src model.ValuationSource, asOf, nowMS int64) model.Valuation {
	return model.Valuation{
		Amount: amount,
		Source: src,
		AsOf:   asOf,
		Stale:  nowMS-asOf > v.staleAfter,
	}
}
