// Package wailsadapter is the Go side of the UI boundary (Principle VII): it
// turns captured data into resolved, valued view rows and exposes them to the
// frontend via bindings + events. Only DTOs cross the boundary.
package wailsadapter

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/epaprat/albion-ledger/internal/boundedmap"
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
	EventTradesChanged   = "trades:changed"
)

// Service holds the bounded live-view state and the bound methods.
type Service struct {
	cat     port.Catalog
	book    *valuation.Book
	val     port.Valuer
	emit    Emitter
	cap     int
	nowMS   func() int64
	startMS int64 // process/session start (realized-P&L window "session", 018)

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

	specUnlockedIDs []int           // latest E:155 unlocked-node set, for persistence (011)
	specEnumIDs     []int           // latest E:1 board enumeration (position→id), for persistence (012)
	uiCtx           context.Context // wails runtime ctx for native dialogs (013); set at OnStartup
	walletSilver    int64           // liquid silver balance (016); valid only when walletKnown
	walletKnown     bool
	walletLastSeen  int64

	tradeStore TradeStore                           // marketplace trade persistence (017); nil = in-memory
	trades     *boundedmap.Map[string, model.Trade] // by trade id (dedup, FR-010; bounded FIFO)

	holdingsStore HoldingsStore // holdings snapshot persistence (020); nil = in-memory
	holdingsDirty atomic.Bool   // a holdings change is owed a flush (set on the pipeline goroutine, read by the flush loop)

	flowBatch bool // coalesce flow-changed emits during a loot burst (019)
	flowDirty bool // a flow refresh is owed when the batch ends

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
		startMS:       nowMS(),
		items:         map[int]*model.LiveViewItem{},
		trades:        boundedmap.New[string, model.Trade](cap),
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

// SetSpecUnlocked stores the latest unlocked-node set for background persistence (011).
func (s *Service) SetSpecUnlocked(ids []int) {
	s.mu.Lock()
	s.specUnlockedIDs = append(s.specUnlockedIDs[:0], ids...)
	s.mu.Unlock()
}

// SpecUnlockedSnapshot returns a copy of the unlocked set for the persistence flush.
func (s *Service) SpecUnlockedSnapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.specUnlockedIDs...)
}

// SetSpecEnum stores the latest E:1 board enumeration for background persistence (012).
func (s *Service) SetSpecEnum(ids []int) {
	s.mu.Lock()
	s.specEnumIDs = append(s.specEnumIDs[:0], ids...)
	s.mu.Unlock()
}

// SpecEnumSnapshot returns a copy of the enumeration for the persistence flush.
func (s *Service) SpecEnumSnapshot() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.specEnumIDs...)
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
	s.holdingsDirty.Store(true) // a container changed → owe the persistence loop a flush (020)
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
// It runs on the status-ticker goroutine, so it broadcasts directly — bypassing the
// pipeline-only flow-batch flags — to avoid an unsynchronized cross-goroutine read of them.
func (s *Service) EmitFlowNow() { s.broadcastFlow() }

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

// BeginFlowBatch/EndFlowBatch coalesce flow-changed refreshes across a burst of ingests
// (take-all loot) into a single emit (019). The flowBatch/flowDirty flags are touched ONLY
// by the pipeline goroutine — the Ingest* path plus this Begin/End — so they need no lock.
// EmitFlowNow (status-ticker goroutine) deliberately bypasses them via broadcastFlow, so
// there is no cross-goroutine access to these flags.
func (s *Service) BeginFlowBatch() {
	s.flowBatch = true
	s.flowDirty = false
}

func (s *Service) EndFlowBatch() {
	s.flowBatch = false
	if s.flowDirty {
		s.flowDirty = false
		s.broadcastFlow()
	}
}

// emitFlow is the pipeline-goroutine flow refresh: it coalesces into an open batch when one
// is active, else broadcasts immediately.
func (s *Service) emitFlow() {
	if s.flowBatch {
		s.flowDirty = true // a refresh is owed; EndFlowBatch will broadcast it once
		return
	}
	s.broadcastFlow()
}

// broadcastFlow emits the current session summary unconditionally. It touches no batch
// state (only the immutable emitter + the internally-locked flow ledger), so it is safe to
// call from any goroutine — the status ticker's EmitFlowNow does.
func (s *Service) broadcastFlow() {
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

// SetWallet records the liquid silver balance (016). Newest-wins by arrival time: a
// source whose timestamp is older than the last applied one is ignored (the R:2 login
// seed fires on every zone change, so this also drops the redundant re-seeds). Only a
// real change re-emits.
func (s *Service) SetWallet(silver int64, ts int64) {
	s.mu.Lock()
	applied := ts >= s.walletLastSeen
	if applied {
		s.walletSilver, s.walletKnown, s.walletLastSeen = silver, true, ts
	}
	s.mu.Unlock()
	if applied {
		s.emitHoldings()
	}
}

// HoldingsSummary returns the total value + per-location seen/stale state, with the
// wallet and net worth (016) overlaid. Net worth = wallet + holdings value; when the
// wallet is unknown it equals the holdings value (the caller labels it "wallet excluded"
// — no fabricated zero, FR-003).
func (s *Service) HoldingsSummary() model.HoldingsSummary {
	sum := s.agg.Summary(s.nowMS())
	s.mu.Lock()
	silver, known, seen := s.walletSilver, s.walletKnown, s.walletLastSeen
	s.mu.Unlock()
	if known {
		sum.WalletSilver, sum.WalletKnown, sum.WalletLastSeen = silver, true, seen
		sum.NetWorth = silver + sum.TotalValue
	} else {
		sum.NetWorth = sum.TotalValue
	}
	return sum
}

// Spec returns the player's character specialization levels.
func (s *Service) Spec() model.CharacterSpec {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spec
}

// ── Marketplace trades (017) ─────────────────────────────────────────────────

// TradeStore persists captured trades + the mail-type map so both survive restarts
// (Principle VIII/XI). Low-frequency (per opened mail / trade) → writes are direct.
type TradeStore interface {
	SaveTrade(ctx context.Context, t model.Trade) error
	SaveMailInfo(ctx context.Context, id int64, typ, location string, received int64) error
}

// SaveMailInfo persists one mail's type so a later session can decode a mail the game
// client-cached and never re-listed (017). No-op without a store.
func (s *Service) SaveMailInfo(id int64, typ, location string, received int64) {
	s.mu.Lock()
	store := s.tradeStore
	s.mu.Unlock()
	if store != nil {
		if err := store.SaveMailInfo(context.Background(), id, typ, location, received); err != nil {
			log.Printf("mail-info store write failed (%d): %v", id, err)
		}
	}
}

// SetTradeStore wires the persistence sink (nil keeps the ledger purely in-memory).
func (s *Service) SetTradeStore(store TradeStore) {
	s.mu.Lock()
	s.tradeStore = store
	s.mu.Unlock()
}

// ── Holdings persistence & hydration (020) ───────────────────────────────────

// HoldingsStore persists the holdings snapshot so inventory + banks hydrate immediately
// on the next launch (stale-labelled until re-seen live).
type HoldingsStore interface {
	SaveContainer(ctx context.Context, c holdings.ContainerSnapshot) error
	LoadContainers(ctx context.Context) ([]holdings.ContainerSnapshot, error)
}

// SetHoldingsStore wires the holdings persistence sink (nil keeps holdings in-memory).
func (s *Service) SetHoldingsStore(store HoldingsStore) {
	s.mu.Lock()
	s.holdingsStore = store
	s.mu.Unlock()
}

// SeedHoldings restores persisted containers into the live aggregator at startup (020).
// They arrive stale (their original LastSeen) and never clobber a container that live data
// already claimed this session.
func (s *Service) SeedHoldings(snaps []holdings.ContainerSnapshot) {
	if len(snaps) == 0 {
		return
	}
	s.agg.SeedContainers(snaps)
	s.emitHoldings()
}

// StartHoldingsPersistence runs a debounced background flush: whenever holdings changed
// (holdingsDirty), the full bounded snapshot (≤512 containers) is written off the capture
// goroutine. A final flush runs on ctx cancel so the last state survives a clean shutdown.
func (s *Service) StartHoldingsPersistence(ctx context.Context) {
	s.mu.Lock()
	store := s.holdingsStore
	s.mu.Unlock()
	if store == nil {
		return
	}
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				s.flushHoldings(store)
				return
			case <-t.C:
				s.flushHoldings(store)
			}
		}
	}()
}

// flushHoldings persists every container if a change is pending, then clears the flag.
func (s *Service) flushHoldings(store HoldingsStore) {
	if !s.holdingsDirty.Swap(false) {
		return
	}
	for _, c := range s.agg.Snapshot() {
		if err := store.SaveContainer(context.Background(), c); err != nil {
			log.Printf("holdings store write failed (%s): %v", c.GUID, err)
			s.holdingsDirty.Store(true) // retry on the next tick
			return
		}
	}
}

// SeedTrades preloads the persisted ledger into the live view at startup (FR-011).
// LoadTrades returns newest-first, but the cache evicts oldest-inserted first — so seed in
// reverse (oldest first), else the first live trade of the session would evict the newest
// history instead of the oldest (review).
func (s *Service) SeedTrades(trades []model.Trade) {
	s.mu.Lock()
	for i := len(trades) - 1; i >= 0; i-- {
		s.trades.Put(trades[i].TradeID, trades[i])
	}
	s.mu.Unlock()
	s.emitTrades()
}

// AddTrade records one captured order-fill mail, deduped by mail id (reading the same
// mail twice must not double-count, FR-006). Only a genuinely new/changed trade persists
// and re-emits — no redundant refresh (mirrors SetWallet, 016).
func (s *Service) AddTrade(t model.Trade) {
	s.mu.Lock()
	prev, existed := s.trades.Get(t.TradeID)
	changed := !existed || prev != t
	if changed {
		s.trades.Put(t.TradeID, t) // new key evicts oldest at cap; existing refreshes in place
	}
	store := s.tradeStore
	s.mu.Unlock()
	if !changed {
		return
	}
	if store != nil {
		if err := store.SaveTrade(context.Background(), t); err != nil {
			log.Printf("trade store write failed (%s): %v", t.TradeID, err)
		}
	}
	s.emitTrades()
}

// tradeItemName resolves a mail item id to a display name. The catalog keys enchanted
// resources by the FULL uniqueName (e.g. "T5_HIDE_LEVEL2@2"), so try the whole id first;
// only fall back to the @-stripped base for items keyed by their unenchanted name. Raw id
// if neither is known.
func (s *Service) tradeItemName(itemID string) string {
	if idx, ok := s.cat.IndexOf(itemID); ok {
		if it := s.cat.Resolve(idx, 0); it.DisplayName != "" {
			return it.DisplayName
		}
	}
	if i := strings.IndexByte(itemID, '@'); i >= 0 {
		if idx, ok := s.cat.IndexOf(itemID[:i]); ok {
			if it := s.cat.Resolve(idx, 0); it.DisplayName != "" {
				return it.DisplayName
			}
		}
	}
	return itemID
}

// Trades returns the captured trades, newest first (received then trade id). The item
// name is resolved at READ time (Principle V) — only ItemID is persisted, so a seeded
// trade (restored from the store) resolves the same as a freshly captured one. An item
// index (instant sell) resolves through the catalog even without a uniqueName.
func (s *Service) Trades() []model.Trade {
	s.mu.Lock()
	out := s.trades.Values()
	s.mu.Unlock()
	for i := range out {
		out[i].ItemName = s.resolveTradeName(out[i])
		out[i].Received = normalizeTradeMs(out[i].Received) // heal rows persisted as .NET ticks
		// Fill the uniqueName (used for the item-render icon) for trades that only carry
		// the catalog index (instant sell, buy-order setup).
		if out[i].ItemID == "" && out[i].ItemIndex > 0 {
			if it := s.cat.Resolve(out[i].ItemIndex, 0); it.UniqueName != "" {
				out[i].ItemID = it.UniqueName
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Received != out[j].Received {
			return out[i].Received > out[j].Received
		}
		return out[i].TradeID > out[j].TradeID
	})
	return out
}

// resolveTradeName resolves a trade's display name: a quicksell is an aggregate batch;
// an instant sell carries the item type index; a mail carries the uniqueName; otherwise
// (instant buy without an offer cache) the item is unknown.
func (s *Service) resolveTradeName(t model.Trade) string {
	if t.Source == model.TradeSourceQuick {
		return fmt.Sprintf("Quicksell (%d items)", t.TotalAmount)
	}
	if t.ItemIndex > 0 {
		if it := s.cat.Resolve(t.ItemIndex, 0); it.DisplayName != "" && it.Known {
			return it.DisplayName
		}
	}
	if t.ItemID != "" {
		return s.tradeItemName(t.ItemID)
	}
	return "Unknown item"
}

// TradeSummary rolls up the fee/tax/net breakdown over captured trades for a time window
// (read-time, Principle V) — each component summed SEPARATELY (FR-008). Net = income −
// expense (the real wallet movement). window ∈ {"session","today","7d","all"} ("" → all).
// The window boundary is the single realizedWindowStart rule so this and the Holdings
// header can never disagree (SC-005).
func (s *Service) TradeSummary(window string) model.TradeSummary {
	start := s.realizedWindowStart(window)
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := model.TradeSummary{Scope: realizedScope(window), WindowStart: start}
	for _, t := range s.trades.Values() {
		if normalizeTradeMs(t.Received) < start {
			continue // outside the window (heal .NET ticks before comparing)
		}
		sum.Count++
		if t.Direction == model.TradeBought {
			sum.GrossExpense += t.Gross
		} else {
			sum.GrossIncome += t.Gross
		}
		sum.SalesTax += t.SalesTax
		sum.SetupFee += t.SetupFee
		sum.Net += t.Net
	}
	return sum
}

// realizedWindowStart returns the earliest received-ms included for a window (018). ONE
// rule — received >= start — shared by every realized-P&L read (SC-005).
func (s *Service) realizedWindowStart(window string) int64 {
	now := s.nowMS()
	switch window {
	case "session":
		return s.startMS
	case "today":
		return startOfLocalDayMS(now)
	case "7d":
		return now - 7*86_400_000
	default: // "all" and any unknown → everything
		return 0
	}
}

// startOfLocalDayMS returns local midnight (ms) for the day containing nowMS.
func startOfLocalDayMS(nowMS int64) int64 {
	t := time.UnixMilli(nowMS)
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).UnixMilli()
}

// realizedScope is the honest coverage label shown with a window's totals.
func realizedScope(window string) string {
	switch window {
	case "session":
		return "this session"
	case "today":
		return "today"
	case "7d":
		return "last 7 days"
	default:
		return "all time"
	}
}

// normalizeTradeMs heals a Received value that was persisted as raw .NET DateTime ticks
// (~6e17) into unix ms, so old rows and freshly-converted ones share one time base and
// render as valid dates. Values already in ms pass through.
func normalizeTradeMs(v int64) int64 {
	if v > 1e15 {
		return (v - 621355968000000000) / 10000
	}
	return v
}

func (s *Service) emitTrades() {
	if s.emit != nil {
		s.emit.Emit(EventTradesChanged, s.TradeSummary("all"))
	}
}
