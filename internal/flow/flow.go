// Package flow aggregates the player's own earnings (silver / loot / gather / fame)
// into a deduped, valued, bounded activity ledger with an AFM-style activity session
// (lazy start on first earning, idle auto-close, live rate). Self-attribution and
// item resolution happen in the capture/main layer; the ledger receives only the
// player's own, already-resolved events. Bounded per Constitution Principle XI.
package flow

import (
	"sort"
	"sync"

	"github.com/epaprat/albion-ledger/internal/boundedmap"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/port"
)

const (
	// DefaultIdleMS auto-closes a session after this long with no earning (idle time
	// is never counted into the rate). Mirrors AFM's 30-min inactivity timeout.
	DefaultIdleMS = 30 * 60 * 1000
	// DefaultCap bounds the in-memory event ring (history lives in the store).
	DefaultCap = 10_000
	// rateMinMS is the minimum active elapsed before a per-hour rate is shown (SC-006).
	rateMinMS = 60 * 1000
)

// ik keys unvalued events by item identity for deferred revaluation (FR-009).
type ik struct{ index, quality int }

// ikk keys the per-item session aggregate (loot/gather breakdown) by kind + identity.
type ikk struct {
	kind           model.FlowKind
	index, quality int
}

// itemStat is the running session aggregate for one loot/gather item.
type itemStat struct {
	qty      int
	lastSeen int64
}

// ItemStat is one row of the per-item breakdown (loot/gather): total quantity this
// session with the current unit + stack value (AFM-style gathering summary).
type ItemStat struct {
	Kind       model.FlowKind
	Index      int
	Quality    int
	Qty        int
	UnitValue  int64 // per-item EMV (current)
	TotalValue int64 // stack EMV = UnitValue × Qty
	Valued     bool
	LastSeen   int64
}

// Ledger is the live earnings state for the current activity session.
type Ledger struct {
	val       port.Valuer
	idleMS    int64
	maxEvents int

	mu        sync.Mutex
	selfObjID int
	selfName  string
	zone      string // current zone/cluster label, stamped onto each event (006 analytics)

	events         map[string]*model.FlowEvent       // by id (bounded ring)
	order          []string                          // event ids, oldest first
	dedup          *boundedmap.Map[string, struct{}] // seen ids (persists past ring eviction, FR-008; bounded)
	unvaluedByItem map[ik][]string                   // item identity -> live unvalued event ids (FR-009)
	itemAgg        map[ikk]*itemStat                 // per-item session totals (loot/gather breakdown)

	startedMS      int64
	lastActivityMS int64
	active         bool
	netSilver      int64
	lootValue      int64
	gatherValue    int64
	fame           int64
	unvaluedCount  int
	eventCount     int
}

// New creates a Ledger. idleMS<=0 → DefaultIdleMS; maxEvents<=0 → DefaultCap.
func New(val port.Valuer, idleMS int64, maxEvents int) *Ledger {
	if idleMS <= 0 {
		idleMS = DefaultIdleMS
	}
	if maxEvents <= 0 {
		maxEvents = DefaultCap
	}
	return &Ledger{
		val: val, idleMS: idleMS, maxEvents: maxEvents,
		events:         map[string]*model.FlowEvent{},
		dedup:          boundedmap.New[string, struct{}](maxEvents * 4),
		unvaluedByItem: map[ik][]string{},
		itemAgg:        map[ikk]*itemStat{},
	}
}

// SetSelf records the local player's object id + name (from the Join own-state
// response). Empty/zero values are ignored so a later partial update never clears it.
func (l *Ledger) SetSelf(objID int, name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if objID > 0 {
		l.selfObjID = objID
	}
	if name != "" {
		l.selfName = name
	}
}

// SetZone records the current zone/cluster label; subsequent events are stamped with
// it so per-zone rate analytics (feature 006) can group by where earnings happened.
func (l *Ledger) SetZone(zone string) {
	if zone == "" {
		return
	}
	l.mu.Lock()
	l.zone = zone
	l.mu.Unlock()
}

// IngestSilver records a net-silver earning (already tax-adjusted, in whole silver).
// Returns the stored event, or nil if it was a duplicate (so callers persist once).
func (l *Ledger) IngestSilver(id string, net int64, ts int64, source string) *model.FlowEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.touch(ts)
	if l.dup(id) {
		return nil
	}
	l.netSilver += net
	e := &model.FlowEvent{ID: id, Kind: model.FlowSilver, TS: ts, Count: 1, Silver: net, Valued: true, Source: source, Zone: l.zone}
	l.push(e)
	return e
}

// IngestLoot records a looted item, valued via the valuer (unvalued → counted but 0).
func (l *Ledger) IngestLoot(id string, item model.Item, count int, ts int64, source string) *model.FlowEvent {
	return l.ingestItem(id, model.FlowLoot, item, count, ts, source)
}

// IngestGather records a gathered/reward item, valued like loot.
func (l *Ledger) IngestGather(id string, item model.Item, count int, ts int64, source string) *model.FlowEvent {
	return l.ingestItem(id, model.FlowGather, item, count, ts, source)
}

func (l *Ledger) ingestItem(id string, kind model.FlowKind, item model.Item, count int, ts int64, source string) *model.FlowEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.touch(ts)
	if l.dup(id) {
		return nil
	}
	if count < 1 {
		count = 1
	}
	v := l.val.Value(item.Index, item.Quality, ts)
	valued := v.Source != model.SourceUnknown
	var silver int64
	if valued {
		silver = v.Amount * int64(count)
		if kind == model.FlowLoot {
			l.lootValue += silver
		} else {
			l.gatherValue += silver
		}
		l.netSilver += silver
	} else {
		l.unvaluedCount++
		key := ik{item.Index, item.Quality}
		l.unvaluedByItem[key] = append(l.unvaluedByItem[key], id)
	}
	// Per-item session aggregate for the loot/gather breakdown (AFM-style).
	ak := ikk{kind, item.Index, item.Quality}
	st := l.itemAgg[ak]
	if st == nil {
		st = &itemStat{}
		l.itemAgg[ak] = st
	}
	st.qty += count
	st.lastSeen = ts

	e := &model.FlowEvent{ID: id, Kind: kind, TS: ts, Item: item, Count: count, Silver: silver, Valued: valued, Source: source, Zone: l.zone}
	l.push(e)
	return e
}

// Breakdown returns the per-item session totals for a loot/gather kind, valued at the
// CURRENT unit price (so stack value reflects the latest EMV), sorted by total value
// desc. Silver/fame have no item breakdown → empty.
func (l *Ledger) Breakdown(kind model.FlowKind, now int64) []ItemStat {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ItemStat, 0)
	for k, st := range l.itemAgg {
		if k.kind != kind {
			continue
		}
		v := l.val.Value(k.index, k.quality, now)
		unit := v.Amount
		valued := v.Source != model.SourceUnknown
		out = append(out, ItemStat{
			Kind: k.kind, Index: k.index, Quality: k.quality, Qty: st.qty,
			UnitValue: unit, TotalValue: unit * int64(st.qty), Valued: valued, LastSeen: st.lastSeen,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalValue != out[j].TotalValue {
			return out[i].TotalValue > out[j].TotalValue
		}
		return out[i].Qty > out[j].Qty
	})
	return out
}

// IngestFame records fame gained (separate metric; never mixed into silver, SC-005).
func (l *Ledger) IngestFame(id string, fame int64, ts int64) *model.FlowEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.touch(ts)
	if l.dup(id) {
		return nil
	}
	l.fame += fame
	e := &model.FlowEvent{ID: id, Kind: model.FlowFame, TS: ts, Count: 1, Fame: fame, Valued: true, Zone: l.zone}
	l.push(e)
	return e
}

// RevalueItem back-fills value for loot/gather events that were unvalued when
// captured, now that a value is known (FR-009). Totals are corrected; events already
// evicted from the ring are skipped. No duplication. Returns the updated events so the
// caller can re-persist them (idempotent upsert).
func (l *Ledger) RevalueItem(index, quality int) []model.FlowEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := ik{index, quality}
	ids := l.unvaluedByItem[key]
	if len(ids) == 0 {
		return nil
	}
	v := l.val.Value(index, quality, l.lastActivityMS)
	if v.Source == model.SourceUnknown {
		return nil
	}
	var updated []model.FlowEvent
	for _, id := range ids {
		e, ok := l.events[id]
		if !ok {
			continue // evicted from ring; can't fix its row
		}
		silver := v.Amount * int64(e.Count)
		e.Silver = silver
		e.Valued = true
		l.netSilver += silver
		if e.Kind == model.FlowLoot {
			l.lootValue += silver
		} else {
			l.gatherValue += silver
		}
		if l.unvaluedCount > 0 {
			l.unvaluedCount--
		}
		updated = append(updated, *e)
	}
	delete(l.unvaluedByItem, key)
	return updated
}

// Summary returns the derived session rollup as of now (evaluates idle auto-close).
func (l *Ledger) Summary(now int64) model.SessionSummary {
	l.mu.Lock()
	defer l.mu.Unlock()
	active := l.active && l.startedMS > 0 && now-l.lastActivityMS <= l.idleMS
	var elapsed int64
	if l.startedMS > 0 {
		if active {
			elapsed = now - l.startedMS
		} else {
			elapsed = l.lastActivityMS - l.startedMS // idle tail not counted
		}
	}
	if elapsed < 0 {
		elapsed = 0
	}
	rateReady := elapsed >= rateMinMS
	var sph, fph int64
	if rateReady {
		sph = l.netSilver * 3_600_000 / elapsed
		fph = l.fame * 3_600_000 / elapsed
	}
	return model.SessionSummary{
		SelfKnown: l.selfObjID > 0,
		Active:    active, StartedMS: l.startedMS, ElapsedMS: elapsed,
		NetSilver: l.netSilver, SilverPerHour: sph,
		LootValue: l.lootValue, GatherValue: l.gatherValue,
		Fame: l.fame, FamePerHour: fph, RateReady: rateReady,
		UnvaluedCount: l.unvaluedCount, EventCount: l.eventCount,
	}
}

// List returns the visible events, newest first (bounded).
func (l *Ledger) List() []model.FlowEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]model.FlowEvent, 0, len(l.order))
	for i := len(l.order) - 1; i >= 0; i-- {
		if e, ok := l.events[l.order[i]]; ok {
			out = append(out, *e)
		}
	}
	return out
}

// touch advances the activity session: starts a fresh one on the first earning or
// after an idle gap longer than idleMS (a new session resets in-memory totals).
func (l *Ledger) touch(ts int64) {
	if !l.active || ts-l.lastActivityMS > l.idleMS {
		l.startSession(ts)
	}
	if ts > l.lastActivityMS {
		l.lastActivityMS = ts
	}
}

func (l *Ledger) startSession(ts int64) {
	l.active = true
	l.startedMS = ts
	l.lastActivityMS = ts
	l.events = map[string]*model.FlowEvent{}
	l.order = nil
	l.dedup = boundedmap.New[string, struct{}](l.maxEvents * 4)
	l.unvaluedByItem = map[ik][]string{}
	l.itemAgg = map[ikk]*itemStat{}
	l.netSilver, l.lootValue, l.gatherValue, l.fame = 0, 0, 0, 0
	l.unvaluedCount, l.eventCount = 0, 0
}

// dup returns true if id was already seen this session; otherwise records it. The
// dedup set persists past ring eviction so a re-sent event never double-counts (FR-008).
func (l *Ledger) dup(id string) bool {
	if _, ok := l.dedup.Get(id); ok {
		return true
	}
	l.dedup.Put(id, struct{}{}) // bounded at maxEvents*4, oldest-evicted
	return false
}

// push appends an event to the ring, evicting the oldest past the cap. Eviction does
// not alter session cumulative totals (history is the store's job, Principle XI).
func (l *Ledger) push(e *model.FlowEvent) {
	l.events[e.ID] = e
	l.order = append(l.order, e.ID)
	l.eventCount++
	for len(l.order) > l.maxEvents {
		old := l.order[0]
		l.order = l.order[1:]
		if ev, ok := l.events[old]; ok {
			if !ev.Valued && (ev.Kind == model.FlowLoot || ev.Kind == model.FlowGather) {
				l.removeUnvaluedRef(ik{ev.Item.Index, ev.Item.Quality}, old)
			}
			delete(l.events, old)
		}
	}
}

func (l *Ledger) removeUnvaluedRef(key ik, id string) {
	ids := l.unvaluedByItem[key]
	for i, x := range ids {
		if x == id {
			l.unvaluedByItem[key] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(l.unvaluedByItem[key]) == 0 {
		delete(l.unvaluedByItem, key)
	}
}
