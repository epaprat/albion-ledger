// Package wailsadapter is the Go side of the UI boundary (Principle VII): it
// turns captured data into resolved, valued view rows and exposes them to the
// frontend via bindings + events. Only DTOs cross the boundary.
package wailsadapter

import (
	"context"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/flow"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/valuation"
	"github.com/epaprat/albion-ledger/internal/zonestats"
)

// Emitter abstracts the Wails event runtime so the service is testable without it.
type Emitter interface {
	Emit(event string, data ...interface{})
}

// Events emitted to the frontend.
const (
	EventItemUpdated     = "item:updated"
	EventStatusChanged   = "status:changed"
	EventDriftAlert      = "drift:alert"
	EventHoldingsChanged = "holdings:changed"
	EventSpecChanged     = "spec:changed"
	EventFlowChanged     = "flow:changed"
)

// Service holds the bounded live-view state and the bound methods.
type Service struct {
	cat   port.Catalog
	book  *valuation.Book
	val   port.Valuer
	emit  Emitter
	cap   int
	nowMS func() int64

	agg  *holdings.Aggregator
	flow *flow.Ledger
	spec model.CharacterSpec

	flowReader  FlowReader           // zone-analytics read side (006); nil = no store
	sessionID   string               // active capture session id (window "session")
	flowCh      chan model.FlowEvent // bounded buffer to the background writer (Principle XI)
	flowStop    chan struct{}        // closed by StopFlowPersistence to trigger the final flush
	flowDone    chan struct{}        // closed by the writer after its final flush completes
	flowStopped bool                 // guards double-close of flowStop
	flowDropped atomic.Int64         // events dropped on a full buffer (observability, VIII)

	externalNudge chan struct{} // buffered(1) — K content signals the external price loop (010)

	mu     sync.Mutex
	items  map[int]*model.LiveViewItem // by item index
	order  []int                       // insertion order for FIFO cap eviction
	status model.CaptureStatusView
}

// NewService creates the view service. cap bounds the live list (Principle XI).
func NewService(cat port.Catalog, book *valuation.Book, val port.Valuer, emit Emitter, cap int, nowMS func() int64) *Service {
	if cap <= 0 {
		cap = 5000
	}
	return &Service{
		cat: cat, book: book, val: val, emit: emit, cap: cap, nowMS: nowMS,
		items:         map[int]*model.LiveViewItem{},
		agg:           holdings.New(cat, val, model.DefaultStaleAfterMS),
		flow:          flow.New(val, flow.DefaultIdleMS, flow.DefaultCap),
		externalNudge: make(chan struct{}, 1),
	}
}

// IngestEMV records an item's estimated value and refreshes its view row. A newly
// known value also back-fills any flow loot/gather rows that were unvalued (FR-009).
func (s *Service) IngestEMV(index, quality int, value, asOf int64) {
	s.book.SetEMV(index, quality, value, asOf)
	s.upsert(index, quality, asOf)
	// Emit only when something was actually revalued: EMV traffic is high-frequency
	// (event 466 arrays + every New*Item), and an unconditional emit would drive a
	// permanent flow-refresh loop in the webview with zero flow activity (Principle XI).
	revalued := s.flow.RevalueItem(index, quality)
	for _, e := range revalued {
		ev := e
		s.persist(&ev)
	}
	if len(revalued) > 0 {
		s.emitFlow()
	}
}

// ExternalRefreshSignal exposes the per-Service nudge channel to the price loop:
// new potentially-unvalued rows arrived (K overview content), run soon instead of
// waiting out the hourly timer (startup-race left fresh rows unpriced for an hour —
// live-seen 2026-07-05). Buffered(1): repeated nudges collapse. Per-instance field,
// not a package global — cross-Service nudging leaked tokens between tests (review).
func (s *Service) ExternalRefreshSignal() <-chan struct{} { return s.externalNudge }

func (s *Service) nudgeExternal() {
	select {
	case s.externalNudge <- struct{}{}:
	default:
	}
}

// RefreshExternalPrices fills valuation gaps from a community price feed (010):
// only items currently HELD and still unvalued are queried — the base layer under
// every in-game price source. Failures degrade silently (no network dependency).
func (s *Service) RefreshExternalPrices(ctx context.Context, fetch port.PriceFetcher) int {
	now := s.nowMS()
	missing := map[string]bool{}
	for _, r := range s.agg.List() {
		if r.Valuation.Source == model.SourceUnknown && r.Item.UniqueName != "" {
			missing[r.Item.UniqueName] = true
		}
	}
	if len(missing) == 0 {
		return 0
	}
	names := make([]string, 0, len(missing))
	for n := range missing {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic batches
	prices, err := fetch.Fetch(ctx, names)
	if err != nil && len(prices) == 0 {
		return 0
	}
	for _, p := range prices {
		if idx, ok := s.cat.IndexOf(p.UniqueName); ok && p.Silver > 0 {
			s.book.SetExternal(idx, p.Quality, p.Silver, now)
		}
	}
	if len(prices) > 0 {
		s.emitHoldings()
	}
	return len(prices)
}

// IngestMarketPrice records a market price identified by uniqueName (order feeds
// carry names, not indexes — 010). The price doubles as a persisted server-estimate
// so resources learned from a market browse keep pricing bank summaries next session.
func (s *Service) IngestMarketPrice(uniqueName string, quality int, silver int64) {
	idx, ok := s.cat.IndexOf(uniqueName)
	if !ok {
		return
	}
	now := s.nowMS()
	s.book.SetMarket(idx, quality, silver, now)
	s.book.SetEMV(idx, quality, silver, now)
	s.upsert(idx, quality, now)
}

// IngestMarket records a live market price and refreshes its view row.
func (s *Service) IngestMarket(index, quality int, price, asOf int64) {
	s.book.SetMarket(index, quality, price, asOf)
	s.upsert(index, quality, asOf)
}

func (s *Service) upsert(index, quality int, asOf int64) {
	now := s.nowMS()
	item := s.cat.Resolve(index, quality)
	val := s.val.Value(index, quality, now)

	s.mu.Lock()
	row, ok := s.items[index]
	if !ok {
		row = &model.LiveViewItem{}
		s.items[index] = row
		s.order = append(s.order, index)
		s.evict()
	}
	row.Item = item
	row.Valuation = val
	row.LastSeen = asOf
	row.Count++
	snapshot := *row
	s.mu.Unlock()

	if s.emit != nil {
		s.emit.Emit(EventItemUpdated, snapshot)
	}
}

func (s *Service) evict() {
	for len(s.items) > s.cap && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.items, oldest)
	}
}

// SetStatus updates and broadcasts the capture status (FR-006 / drift FR-012).
func (s *Service) SetStatus(st model.CaptureStatusView) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	if s.emit != nil {
		s.emit.Emit(EventStatusChanged, st)
		if st.DriftAlert != "" {
			s.emit.Emit(EventDriftAlert, st.DriftAlert)
		}
	}
}

// ── Holdings ingestion ───────────────────────────────────────────────────────

// IngestContainer replaces a container's held items (full snapshot) and broadcasts.
func (s *Service) IngestContainer(containerGUID, ownerGUID string, slots []holdings.SlotItem) {
	s.agg.SetContainer(containerGUID, ownerGUID, slots, s.nowMS())
	s.emitHoldings()
}

// IngestSelfContainer replaces a player-owned bag/equipped container (from own-state)
// under the inventory group with the given tab, and broadcasts.
func (s *Service) IngestSelfContainer(containerGUID, tab string, slots []holdings.SlotItem) {
	s.agg.SetSelfContainer(containerGUID, tab, slots, s.nowMS())
	s.emitHoldings()
}

// IngestPutItem incrementally adds/moves one item into a container (live update).
// applied=false means the destination is untracked — the caller decides the fallback
// (typically dropping the item from view so it can't linger stale in its old spot).
func (s *Service) IngestPutItem(containerGUID string, objID int, ref holdings.ItemRef) (applied bool) {
	applied = s.agg.PutItem(containerGUID, objID, ref, s.nowMS())
	if applied {
		s.emitHoldings()
	}
	return applied
}

// EnsureSelfContainer pre-creates a pinned, not-yet-observed player container (008).
func (s *Service) EnsureSelfContainer(containerGUID, tab string) {
	s.agg.EnsureSelfContainer(containerGUID, tab)
}

// IngestDeleteItem incrementally removes one item by object id (live update).
func (s *Service) IngestDeleteItem(objID int) {
	s.agg.DeleteItem(objID, s.nowMS())
	s.emitHoldings()
}

// SetCurrentCity records the player's current city display name (US3).
func (s *Service) SetCurrentCity(city string) {
	s.agg.SetCurrentCity(city)
	s.emitHoldings()
}

// IngestVaultSummaryTab replaces one bank tab from a K bank-overview content
// summary (city-tagged, real tab name, type-based rows — feature 010).
func (s *Service) IngestVaultSummaryTab(tabGUID, city, tabName string, rows []holdings.ItemRef) {
	s.agg.SetVaultSummaryTab(tabGUID, city, tabName, rows, s.nowMS())
	s.emitHoldings()
	s.nudgeExternal() // fresh summary rows may need the external base layer soon
}

// IngestCityVaultValues replaces the per-city vault totals from the K overview (010).
func (s *Service) IngestCityVaultValues(values map[string]int64) {
	s.agg.SetCityVaultValues(values, s.nowMS())
	s.emitHoldings()
}

// IngestBankVault records bank tab owners + names (from BankVaultInfo).
func (s *Service) IngestBankVault(owners, tabNames []string) { s.agg.SetBankVault(owners, tabNames) }

// IngestEquipment replaces the equipped set and broadcasts holdings.
func (s *Service) IngestEquipment(items []holdings.ItemRef) {
	s.agg.SetEquipped(items, s.nowMS())
	s.emitHoldings()
}

// SetSpec replaces the character Destiny Board and broadcasts it (011). The handler
// resolves node names/categories; the service just stores and emits.
func (s *Service) SetSpec(spec model.CharacterSpec) {
	s.mu.Lock()
	s.spec = spec
	snap := s.spec
	s.mu.Unlock()
	if s.emit != nil {
		s.emit.Emit(EventSpecChanged, snap)
	}
}

func (s *Service) emitHoldings() {
	if s.emit != nil {
		s.emit.Emit(EventHoldingsChanged, s.agg.Summary(s.nowMS()))
	}
}

// ── Flow (earnings) ingestion ────────────────────────────────────────────────

// SetSelf records the local player's own object id + name (Join own-state), so the
// flow ledger can attribute own earnings (silver/harvest by id, loot by name).
func (s *Service) SetSelf(objID int, name string) { s.flow.SetSelf(objID, name) }

// SetZone records the current zone/cluster label for stamping flow events (006).
func (s *Service) SetZone(zone string) { s.flow.SetZone(zone) }

// EmitFlowNow re-broadcasts the current session summary. The capture status ticker
// calls this periodically so idle auto-close and the live elapsed/rate stay observable
// between earnings — the summary is otherwise only pushed on ingest, which would leave
// the UI frozen on "Active" after the last earning.
func (s *Service) EmitFlowNow() { s.emitFlow() }

// IngestSilver records a net-silver earning and broadcasts the flow summary.
// A deduped (nil) event changes nothing, so it neither persists nor emits.
func (s *Service) IngestSilver(id string, net int64, ts int64, source string) {
	if e := s.flow.IngestSilver(id, net, ts, source); e != nil {
		s.persist(e)
		s.emitFlow()
	}
}

// IngestLoot records a looted item (valued via the valuer) and broadcasts.
func (s *Service) IngestLoot(id string, index, quality, count int, ts int64, source string) {
	if e := s.flow.IngestLoot(id, s.cat.Resolve(index, quality), count, ts, source); e != nil {
		s.persist(e)
		s.emitFlow()
	}
}

// IngestGather records a gathered/reward item and broadcasts.
func (s *Service) IngestGather(id string, index, quality, count int, ts int64, source string) {
	if e := s.flow.IngestGather(id, s.cat.Resolve(index, quality), count, ts, source); e != nil {
		s.persist(e)
		s.emitFlow()
	}
}

// IngestFame records fame gained (separate metric) and broadcasts.
func (s *Service) IngestFame(id string, fame int64, ts int64) {
	if e := s.flow.IngestFame(id, fame, ts); e != nil {
		s.persist(e)
		s.emitFlow()
	}
}

func (s *Service) emitFlow() {
	if s.emit != nil {
		s.emit.Emit(EventFlowChanged, s.flow.Summary(s.nowMS()))
	}
}

// FlowStore is the local-first persistence sink for earnings events (Principle VIII):
// every flow event is written to the durable store, the in-memory ledger stays bounded.
type FlowStore interface {
	AppendFlowEvents(ctx context.Context, sessionID string, batch []model.FlowEvent) error
}

// FlowReader is the zone-analytics read side (006): windowed, ordered, bounded reads
// of the persisted flow history.
type FlowReader interface {
	LoadFlowEvents(ctx context.Context, sessionID string, sinceMS int64, limit int) ([]zonestats.StoredEvent, error)
}

// SetFlowReader wires the analytics read side (kept separate from the writer so a
// read-only build or a failed store degrades independently).
func (s *Service) SetFlowReader(r FlowReader, sessionID string) {
	s.mu.Lock()
	s.flowReader = r
	s.sessionID = sessionID
	s.mu.Unlock()
}

// ZoneStats returns per-zone earning rollups for a time window: "session" (current
// capture session), "today" (local midnight), "7d", or "all". Unknown windows fall
// back to "session" (safe default). Sorted by silver/hr desc; zone label never empty.
func (s *Service) ZoneStats(window string) []model.ZoneStatView {
	s.mu.Lock()
	reader, sessionID := s.flowReader, s.sessionID
	s.mu.Unlock()
	if reader == nil {
		return []model.ZoneStatView{}
	}

	now := s.nowMS()
	sessionFilter := ""
	var since int64
	switch window {
	case "today":
		t := time.UnixMilli(now)
		y, m, d := t.Local().Date()
		since = time.Date(y, m, d, 0, 0, 0, 0, time.Local).UnixMilli()
	case "7d":
		since = now - 7*24*3_600_000
	case "all":
		since = 0
	default: // "session" and anything unknown
		sessionFilter = sessionID
	}

	events, err := reader.LoadFlowEvents(context.Background(), sessionFilter, since, 0)
	if err != nil {
		log.Printf("zone stats load failed: %v", err)
		return []model.ZoneStatView{}
	}
	// Normalize generated-instance labels BEFORE grouping so every corrupted-dungeon/
	// mists/hellgate run (each a unique per-instance id in the store — kept raw there
	// on purpose) rolls up into ONE analytics row per instance type. Memoized per call:
	// a 500k-row window has only ~hundreds of distinct labels.
	friendly := make(map[string]string, 64)
	for i := range events {
		z := events[i].Zone
		f, ok := friendly[z]
		if !ok {
			f = locations.Friendly(z)
			friendly[z] = f
		}
		events[i].Zone = f
	}
	stats := zonestats.Compute(events)

	out := make([]model.ZoneStatView, 0, len(stats))
	for _, z := range stats {
		name := z.Zone
		if name == "" {
			name = "Unknown zone"
		}
		acts := make([]model.ZoneActivityStatView, 0, len(z.Activities))
		for _, a := range z.Activities {
			acts = append(acts, model.ZoneActivityStatView{
				Kind: a.Kind, Total: a.Total, PerHour: a.PerHour, EventCount: a.EventCount,
			})
		}
		out = append(out, model.ZoneStatView{
			Zone: name, ActiveMS: z.ActiveMS,
			NetSilver: z.NetSilver, SilverPerHour: z.SilverPerHour,
			GatherValue: z.GatherValue, GatherPerHour: z.GatherPerHour,
			Fame: z.Fame, FamePerHour: z.FamePerHour,
			EventCount: z.EventCount, InsufficientData: z.InsufficientData,
			Activities: acts,
		})
	}
	return out
}

// StartFlowPersistence wires a store + session id and launches a single background
// writer that batches flow events to the store (size- or time-triggered). Writes use
// context.Background() so the final flush still lands after the app context is
// cancelled; call StopFlowPersistence from shutdown to drain before closing the store.
// Safe to skip entirely — with no store the ledger is purely in-memory.
func (s *Service) StartFlowPersistence(ctx context.Context, store FlowStore, sessionID string) {
	if store == nil {
		return
	}
	s.mu.Lock()
	s.flowCh = make(chan model.FlowEvent, 1024)
	s.flowStop = make(chan struct{})
	s.flowDone = make(chan struct{})
	ch, stop, done := s.flowCh, s.flowStop, s.flowDone
	s.mu.Unlock()

	go func() {
		defer close(done)
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		buf := make([]model.FlowEvent, 0, 64)
		flush := func() {
			if len(buf) == 0 {
				return
			}
			// A failed local write must be visible, never silent (Principles VII/VIII).
			// The upsert is idempotent, so the buffered batch is retried on the next flush.
			if err := store.AppendFlowEvents(context.Background(), sessionID, buf); err != nil {
				log.Printf("flow store write failed (%d events, will retry): %v", len(buf), err)
				return
			}
			buf = buf[:0]
		}
		drainAndExit := func() {
			for {
				select {
				case e := <-ch:
					buf = append(buf, e)
				default:
					flush()
					return
				}
			}
		}
		for {
			select {
			case <-ctx.Done():
				drainAndExit()
				return
			case <-stop:
				drainAndExit()
				return
			case e := <-ch:
				buf = append(buf, e)
				if len(buf) >= 64 {
					flush()
				}
				// A persistently failing store must not balloon the retry buffer
				// (Principle XI): keep the newest 4096 and count the sacrifice.
				if len(buf) > 4096 {
					drop := len(buf) - 4096
					buf = append(buf[:0], buf[drop:]...)
					log.Printf("flow store still failing — dropped %d oldest buffered events", drop)
				}
			case <-t.C:
				flush()
			}
		}
	}()
}

// StopFlowPersistence triggers the writer's final drain+flush and waits (bounded) for
// it, so the caller can close the store without racing an in-flight write. Safe to
// call when persistence was never started, and safe to call more than once.
func (s *Service) StopFlowPersistence() {
	s.mu.Lock()
	if s.flowStop != nil && !s.flowStopped {
		s.flowStopped = true
		close(s.flowStop)
	}
	done := s.flowDone
	s.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		log.Printf("flow store writer did not drain within 2s; closing anyway")
	}
}

// persist enqueues a newly-stored event for the background writer (non-blocking; a
// full buffer drops rather than stalling capture — bounded, Principle XI). Drops are
// counted and logged so durable-history loss is never silent (Principle VIII).
func (s *Service) persist(e *model.FlowEvent) {
	if e == nil || s.flowCh == nil {
		return
	}
	select {
	case s.flowCh <- *e:
	default:
		if n := s.flowDropped.Add(1); n == 1 || n%100 == 0 {
			log.Printf("flow persist buffer full — %d events dropped so far", n)
		}
	}
}

// ListFlow returns the current flow events, newest first (bounded), as UI DTOs.
func (s *Service) ListFlow() []model.FlowEventView {
	events := s.flow.List()
	out := make([]model.FlowEventView, 0, len(events))
	for _, e := range events {
		out = append(out, model.FlowEventView{
			Kind: e.Kind, TS: e.TS,
			ItemDisplayName: e.Item.DisplayName, UniqueName: e.Item.UniqueName,
			Tier: e.Item.Tier, Enchant: e.Item.Enchant, Quality: e.Item.Quality,
			Count: e.Count, Silver: e.Silver, Fame: e.Fame, Valued: e.Valued, Source: e.Source,
			Zone: locations.Friendly(e.Zone), // display-time: raw instance ids stay in the store
		})
	}
	return out
}

// FlowSummary returns the current activity-session rollup.
func (s *Service) FlowSummary() model.SessionSummary { return s.flow.Summary(s.nowMS()) }

// FlowBreakdown returns the per-item session totals for a loot/gather kind
// ("loot"|"gather"): quantity + per-item EMV + stack (total) value, resolved to item
// names, valued at the current price (AFM-style gathering/loot summary).
func (s *Service) FlowBreakdown(kind string) []model.FlowItemStatView {
	stats := s.flow.Breakdown(model.FlowKind(kind), s.nowMS())
	out := make([]model.FlowItemStatView, 0, len(stats))
	for _, st := range stats {
		it := s.cat.Resolve(st.Index, st.Quality)
		out = append(out, model.FlowItemStatView{
			Kind: st.Kind, ItemDisplayName: it.DisplayName, UniqueName: it.UniqueName,
			Tier: it.Tier, Enchant: it.Enchant, Quality: st.Quality, Qty: st.Qty,
			UnitValue: st.UnitValue, TotalValue: st.TotalValue, Valued: st.Valued, LastSeen: st.LastSeen,
		})
	}
	return out
}

// ── Bound methods (called from the frontend) ─────────────────────────────────

// ListItems returns the current live view rows (most recently seen first).
func (s *Service) ListItems() []model.LiveViewItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.LiveViewItem, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if row, ok := s.items[s.order[i]]; ok {
			out = append(out, *row)
		}
	}
	return out
}

// Status returns the current capture status.
func (s *Service) Status() model.CaptureStatusView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ListHoldings returns the player's held items (inventory/bank/equipped).
func (s *Service) ListHoldings() []model.HoldingItem { return s.agg.List() }

// HoldingsSummary returns the total value + per-location seen/stale state.
func (s *Service) HoldingsSummary() model.HoldingsSummary { return s.agg.Summary(s.nowMS()) }

// Spec returns the player's character specialization levels.
func (s *Service) Spec() model.CharacterSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spec
}
