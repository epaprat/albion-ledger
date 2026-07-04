// Package app owns the capture-time pipeline: parser callbacks → classification →
// per-category handlers → the UI/persistence sink. Before feature 009 this glue lived
// as package-level globals and a monolithic switch in cmd/albion-ledger — every
// feature grew that file and nothing was testable without the entrypoint
// (ADR-022/024/025, Constitution Principle II).
//
// Concurrency model (unchanged from pre-009): the whole Pipeline runs on the single
// capture goroutine. objMu exists only because resolveObjects/registerNewItem also
// serialize the object registry against the pending queues — the same discipline the
// globals had.
package app

import (
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/loot"
	"github.com/epaprat/albion-ledger/internal/pending"
)

// Fixed container ids for the player's own bag + equipped sets, which arrive in
// own-state slot arrays without a wire GUID. They group under the inventory city as
// separate tabs.
const (
	SelfBagGUID   = "self-bag"
	SelfEquipGUID = "self-equipped"
)

// emvScale: the server EMV is stored scaled by 10000 (silver = raw / 10000).
const emvScale = 10000

const objRegCap = 50_000

// maxBagSlots bounds the live bag slot map. The largest real bag is well under 128
// slots; E:26 (key 1) and op-30 destination (key 3) slots come off the wire with no
// upper bound of their own, and an unbounded pad loop here would let a single
// hostile/corrupt packet allocate gigabytes (Principle IV/XI).
const maxBagSlots = 256

// Sink is the narrow consumer-side view of the UI/persistence service — only the
// methods the pipeline actually calls. The wails adapter satisfies it; handlers stay
// testable against any implementation (Principle I: app depends on the interface).
type Sink interface {
	SetSelf(objID int, name string)
	SetZone(zone string)
	SetCurrentCity(city string)
	IngestEMV(index, quality int, value, asOf int64)
	IngestContainer(containerGUID, ownerGUID string, slots []holdings.SlotItem)
	IngestSelfContainer(containerGUID, tab string, slots []holdings.SlotItem)
	IngestPutItem(containerGUID string, objID int, ref holdings.ItemRef) bool
	IngestDeleteItem(objID int)
	IngestBankVault(owners, tabNames []string)
	IngestVaultSummaryTab(tabGUID, city, tabName string, rows []holdings.ItemRef)
	IngestCityVaultValues(values map[string]int64)
	IngestMarketPrice(uniqueName string, quality int, silver int64)
	IngestSilver(id string, net int64, ts int64, source string)
	IngestLoot(id string, index, quality, count int, ts int64, source string)
	IngestGather(id string, index, quality, count int, ts int64, source string)
	IngestFame(id string, fame int64, ts int64)
}

// Pipeline holds every piece of capture-time state that used to be a package global
// in cmd/albion-ledger. One instance per capture session, driven by the parser
// callbacks (OnRequest/OnResponse/OnEvent).
type Pipeline struct {
	sink  Sink
	clf   *probe.Classifier
	locs  *locations.Locations // cluster-id → zone-name resolver (nil = raw ids)
	nowMS func() int64
	debug bool // -debugflow: log flow attribution to stderr

	// self identity for own-earning attribution (005); set on every Join op-2.
	selfObjID int
	selfName  string
	flowSeq   int64 // monotonic nonce for flow events lacking a natural unique wire id

	// lootTracker correlates own move requests with loot sources (007).
	lootTracker *loot.Tracker

	// objReg maps in-world object ids to their item type+quality (New*Item, codes
	// 30-37); container slots reference object ids. objMu also serializes the pending
	// queues against the registry (see package comment).
	objMu    sync.Mutex
	objReg   map[int]holdings.ItemRef
	objOrder []int

	// pending queues (internal/pending: cap+TTL+counted drops; loss logs stay here).
	pendingInv             *pending.Map[string] // own-state slot → its self container (no TTL)
	pendingLootResolve     *pending.Map[string] // loot hit → source name
	pendingPuts            *pending.Map[string] // container put → target container
	lastPendingLootDropLog int64
	lastPendingPutDropLog  int64

	// Holdings freshness glue (008): own-container GUID bridge + live bag slot map.
	selfContainerGUIDs map[string]string
	bagSlots           []int

	// K bank-overview bridges (010): vault guid → city, tab guid → (city, name).
	// vaultCity is REBUILT per R:516 (full list); tabMeta upserts, capped.
	vaultCity map[string]string
	tabMeta   map[string]tabInfo
}

// New wires a Pipeline. locs may be nil (zones stay raw cluster ids).
func New(sink Sink, clf *probe.Classifier, locs *locations.Locations, nowMS func() int64, debug bool) *Pipeline {
	return &Pipeline{
		sink:               sink,
		clf:                clf,
		locs:               locs,
		nowMS:              nowMS,
		debug:              debug,
		lootTracker:        loot.New(),
		objReg:             map[int]holdings.ItemRef{},
		pendingInv:         pending.New[string](1024, 0),
		pendingLootResolve: pending.New[string](256, 10_000),
		pendingPuts:        pending.New[string](256, 10_000),
		selfContainerGUIDs: map[string]string{},
		vaultCity:          map[string]string{},
		tabMeta:            map[string]tabInfo{},
	}
}

// ── Parser callbacks ─────────────────────────────────────────────────────────

// OnRequest handles the player's own OUTGOING operation requests (passively
// observed). The op code lives in param 253; loot correlation needs the item-move
// requests (007). The REAL key-253 code is required — the raw-opByte fallback can
// collide on partially-decoded requests and feed bogus guids into the loot tracker.
func (p *Pipeline) OnRequest(_ byte, params map[byte]interface{}) {
	code, ok := capture.IntParam(params, 253)
	if !ok {
		return
	}
	if p.debug && (code == 30 || code == 39) {
		log.Printf("[flow] request: code=%d keys=%v", code, paramKeys(params))
	}
	p.dispatch(probe.KindRequest, code, params)
}

// OnResponse handles operation responses. The op code lives in param 253 (opByte is
// the raw Photon opcode); own-state Join carries 253=2. Self-identity rides on EVERY
// Join op-2 (login AND zone changes, where the player's object id changes per map),
// so it is read here as a FIXED pre-dispatch step — not a registry handler — and is
// not gated by the key-55 own-state guard (login-only). See the ordering contract in
// dispatch.go.
func (p *Pipeline) OnResponse(opByte byte, _ int16, _ string, params map[byte]interface{}) {
	rc := codeFrom(params, 253, int(opByte))
	if fromKey, ok := capture.IntParam(params, 253); ok && fromKey == 2 {
		p.updateSelf(params)
	}
	p.dispatch(probe.KindResponse, rc, params)
}

// OnEvent handles server events. Declarations (New*Item 30-37) are registered BEFORE
// dispatch so containers referencing the new object ids resolve — ordering contract.
func (p *Pipeline) OnEvent(evByte byte, params map[byte]interface{}) {
	code := codeFrom(params, 252, int(evByte))
	p.registerNewItem(code, params)
	p.dispatch(probe.KindEvent, code, params)
}

// ── Self identity + zone (fixed pre-dispatch step) ───────────────────────────

// updateSelf refreshes the local player's own object id + name from a Join op-2
// response (key 0 objId, key 2 name). Requires BOTH fields — a partial match (e.g. a
// stray int at key 0 of a non-Join op-2 variant) must never overwrite a good identity.
func (p *Pipeline) updateSelf(params map[byte]interface{}) {
	objID, name, ok := capture.SelfIdentity(params)
	if !ok || objID <= 0 || name == "" {
		return
	}
	p.selfObjID = objID
	p.selfName = name
	p.sink.SetSelf(objID, name)
	// The Join also carries the current location/cluster (key 8) — stamp it as the zone
	// so flow events know where they happened (per-zone analytics, 006). Open-world zones
	// only surface here (event 163 covers cities); raw cluster id is fine, named later.
	if zone, zok := params[8].(string); zok && zone != "" {
		name := p.zoneName(zone)
		p.sink.SetZone(name)
		// Mid-session starts miss the city-entry notification (event 163), leaving
		// physically-opened bank tabs city-less ("Bank" ghost group, live-seen
		// 2026-07-05). The Join cluster already names the place: a city or bank
		// cluster feeds the current city too, normalized to the bare city name.
		if city := bankCityDisplay(name); city != name || isKnownCity(name) {
			p.sink.SetCurrentCity(bankCityDisplay(name))
		}
	}
	// Own-container GUID bridge (008): bag (key 54, confirmed) + equipped candidate
	// (key 51). Entries update INDEPENDENTLY — a Join variant carrying only one of the
	// keys must not skip (or wipe) the other's bridge. Each virtual id keeps exactly
	// ONE wire guid: when a key's guid changes (character switch, hostile stream), the
	// stale reverse mapping is pruned, so the map is hard-bounded at 2 entries and an
	// old character's bag guid can't keep bridging to self-bag (XI, 009 review).
	if bagGUID, eqGUID, _ := capture.SelfContainers(params); bagGUID != "" || eqGUID != "" {
		if bagGUID != "" {
			p.setSelfContainerGUID(bagGUID, SelfBagGUID)
		}
		if eqGUID != "" {
			p.setSelfContainerGUID(eqGUID, SelfEquipGUID)
		}
		if p.debug {
			log.Printf("[hold] self containers: bag=%s equipped-candidate=%s", bagGUID, eqGUID)
		}
	}
	if p.debug {
		log.Printf("[flow] self set: objID=%d name=%q (op-2)", p.selfObjID, p.selfName)
	}
}

// isSelfObj reports whether an object id is the local player. Until self is known
// (selfObjID==0) it returns false so we never count another player's earnings.
func (p *Pipeline) isSelfObj(objID int) bool { return p.selfObjID != 0 && objID == p.selfObjID }

// zoneName resolves a raw cluster id to a readable zone name when the map is loaded.
func (p *Pipeline) zoneName(clusterID string) string {
	if p.locs != nil {
		return p.locs.Resolve(clusterID)
	}
	return clusterID
}

// nextFlowSeq returns a per-event unique nonce (capture runs on one goroutine, so a
// plain increment is race-free) for building dedup ids of events like harvest ticks
// that legitimately repeat with identical field values.
func (p *Pipeline) nextFlowSeq() int64 { p.flowSeq++; return p.flowSeq }

// ── Object registry + declaration drains ─────────────────────────────────────

// registerNewItem records objectId → {itemIndex, quality, count} from a New*Item
// event and feeds the item's server EstimatedMarketValue into valuation. Field map
// (reference client NewItem): key 1 = itemIndex, key 2 = quantity, key 4 = EMV
// (scaled ×10000), key 6 = quality, key 7 = durability.
func (p *Pipeline) registerNewItem(code int, params map[byte]interface{}) {
	if code < 30 || code > 37 { // NewEquipmentItem..NewEquipmentItemLegendarySoul
		return
	}
	objID, ok1 := capture.IntParam(params, 0)
	idx, ok2 := capture.IntParam(params, 1)
	if !ok1 || !ok2 {
		return
	}
	count := 1
	if c, ok := capture.IntParam(params, 2); ok && c > 0 {
		count = c
	}
	quality, _ := capture.IntParam(params, 6)
	if quality < 0 || quality > 5 { // furniture etc. put non-quality data here
		quality = 0
	}
	p.objMu.Lock()
	if _, exists := p.objReg[objID]; !exists {
		if len(p.objReg) >= objRegCap && len(p.objOrder) > 0 {
			delete(p.objReg, p.objOrder[0])
			p.objOrder = p.objOrder[1:]
		}
		p.objOrder = append(p.objOrder, objID)
	}
	ref := holdings.ItemRef{Index: idx, Quality: quality, Count: count}
	p.objReg[objID] = ref
	now := p.nowMS()
	pendGUID, invPending := p.pendingInv.Take(objID, now)        // own-state slot awaiting this declaration
	source, lootPending := p.pendingLootResolve.Take(objID, now) // loot hit awaiting it (TTL-guarded:
	target, putPending := p.pendingPuts.Take(objID, now)         // ids are reused across zones — a stale
	p.objMu.Unlock()                                             // drain would fabricate phantom events)

	// An own-state bag/equipped object declared after the fact: place it into its
	// self-container now that it resolves.
	if invPending {
		p.sink.IngestPutItem(pendGUID, objID, ref)
	}
	// A loot pickup whose declaration arrived after the move: emit it now (007).
	if lootPending {
		p.ingestLootObj(objID, ref, source)
	}
	// A holdings put whose declaration arrived late (008): place it now. An untracked
	// target still drops the item from its old spot (the put was authoritative — it left).
	if putPending {
		if !p.sink.IngestPutItem(target, objID, ref) {
			p.sink.IngestDeleteItem(objID)
			p.bagSlotClear(objID)
		}
	}

	// The item's own EstimatedMarketValue (key 4, a scalar int64) is the value the game
	// shows when you open it — feed it to valuation so held items are valued without a
	// market capture.
	if emv, ok := capture.IntParam(params, 4); ok && emv > 0 {
		p.sink.IngestEMV(idx, quality, int64(emv)/emvScale, p.nowMS())
	}
}

// resolveObjects maps container object ids to slot items (objId + ref), skipping
// unresolved ones. The objId is kept so incremental moves can target the item.
func (p *Pipeline) resolveObjects(objIDs []int) []holdings.SlotItem {
	p.objMu.Lock()
	defer p.objMu.Unlock()
	slots := make([]holdings.SlotItem, 0, len(objIDs))
	for _, id := range objIDs {
		if r, ok := p.objReg[id]; ok {
			slots = append(slots, holdings.SlotItem{ObjID: id, Ref: r})
		}
	}
	return slots
}

// resolveObj returns the ref for a single object id (for incremental Put).
func (p *Pipeline) resolveObj(objID int) (holdings.ItemRef, bool) {
	p.objMu.Lock()
	defer p.objMu.Unlock()
	r, ok := p.objReg[objID]
	return r, ok
}

// ── Holdings freshness glue (008) ────────────────────────────────────────────

// virtualContainer maps a wire container GUID to its holdings virtual id ("self-bag"
// / "self-equipped") when it is one of the player's own containers.
func (p *Pipeline) virtualContainer(guid string) (string, bool) {
	v, ok := p.selfContainerGUIDs[guid]
	return v, ok
}

// isSelfBag reports whether a wire GUID is the player's own bag — the ONE predicate
// behind the loot-suppression gate (a bag move is never a loot pickup, but the
// unconfirmed-candidate equipped guid must never suppress loot resolution).
func (p *Pipeline) isSelfBag(guid string) bool {
	v, ok := p.selfContainerGUIDs[guid]
	return ok && v == SelfBagGUID
}

// setSelfContainerGUID binds one wire guid to a virtual container id, pruning any
// previous guid bound to the same virtual id — the map never exceeds one entry per
// virtual container regardless of what the wire sends (XI).
func (p *Pipeline) setSelfContainerGUID(guid, virtual string) {
	for g, v := range p.selfContainerGUIDs {
		if v == virtual && g != guid {
			delete(p.selfContainerGUIDs, g)
		}
	}
	p.selfContainerGUIDs[guid] = virtual
}

func (p *Pipeline) bagSlotItem(slot int) (int, bool) {
	if slot < 0 || slot >= len(p.bagSlots) || p.bagSlots[slot] <= 0 {
		return 0, false
	}
	return p.bagSlots[slot], true
}

func (p *Pipeline) bagSlotSet(slot, objID int) {
	if slot < 0 || slot >= maxBagSlots {
		return
	}
	for slot >= len(p.bagSlots) { // bag grew (bigger bag equipped) — pad up to maxBagSlots
		p.bagSlots = append(p.bagSlots, 0)
	}
	p.bagSlots[slot] = objID
}

func (p *Pipeline) bagSlotClear(objID int) {
	for i, v := range p.bagSlots {
		if v == objID {
			p.bagSlots[i] = 0
			return
		}
	}
}

// logDrops emits the rate-limited (1/min) counted-loss line — losses must be
// observable without a debug flag (FR-004), shared by both logging consumers so the
// guard can never drift between copies (009 review). NOTE: the cumulative-counter
// guard re-logs a long-stable count once per minute — pre-009 behavior, preserved
// verbatim; a since-last-report delta is recorded 010 polish.
func (p *Pipeline) logDrops(dropped int, lastLogMS *int64, nowMS int64, format string) {
	if dropped > 0 && nowMS-*lastLogMS > 60_000 {
		*lastLogMS = nowMS
		log.Printf(format, dropped)
	}
}

func (p *Pipeline) queuePendingPut(objID int, target string) {
	now := p.nowMS()
	p.objMu.Lock()
	p.pendingPuts.Queue(objID, target, now)
	dropped := p.pendingPuts.Dropped()
	p.objMu.Unlock()
	p.logDrops(dropped, &p.lastPendingPutDropLog, now, "holdings: %d pending puts dropped so far (declaration never arrived)")
}

// clearSelfPendingPuts drops pending puts targeting the player's own containers —
// a fresh own-state snapshot (which carries BOTH bag and equipped) is authoritative
// and must not be overridden by a late drain (008 US3, symmetric for equipped).
func (p *Pipeline) clearSelfPendingPuts() {
	p.objMu.Lock()
	p.pendingPuts.Clear(func(target string) bool {
		return target == SelfBagGUID || target == SelfEquipGUID
	})
	p.objMu.Unlock()
}

// applyMoveToHoldings applies a single-item move request (op-30) to the holdings
// view: resolve the source slot to an item object, then relocate or drop it.
func (p *Pipeline) applyMoveToHoldings(srcGUID string, srcSlot int, dstGUID string, dstSlot int, hasDst bool) {
	var itemObj int
	if v, bridged := p.virtualContainer(srcGUID); bridged && v == SelfBagGUID {
		if id, ok := p.bagSlotItem(srcSlot); ok {
			itemObj = id
			p.bagSlots[srcSlot] = 0
		}
	} else if id, ok := p.lootTracker.SlotItem(srcGUID, srcSlot); ok {
		itemObj = id
	}
	if itemObj == 0 {
		return // source unknown/empty — nothing to apply (snapshot reconciles later)
	}
	p.applyMovedObject(itemObj, srcGUID, dstGUID, dstSlot, hasDst)
}

// applyMovedObject relocates a known item object: the destination is tried against
// holdings itself (bridged virtual id or any container holdings has seen — bank tabs
// stay known there long after the loot tracker's 10-minute TTL would have swept them);
// an untracked destination (market, sale, never-opened bank) drops the item from view
// (008 contract rules 3-4). The bag slot map tracks bag-side changes.
func (p *Pipeline) applyMovedObject(itemObj int, srcGUID, dstGUID string, dstSlot int, hasDst bool) {
	if v, bridged := p.virtualContainer(srcGUID); bridged && v == SelfBagGUID {
		p.bagSlotClear(itemObj)
	}
	target := ""
	if hasDst {
		if v, bridged := p.virtualContainer(dstGUID); bridged {
			target = v
			if v == SelfBagGUID {
				p.bagSlotSet(dstSlot, itemObj)
			}
		} else {
			target = dstGUID // let holdings decide — it knows every snapshotted container
		}
	}
	if target == "" {
		p.sink.IngestDeleteItem(itemObj) // leaves every tracked view; reappears via snapshots
		if p.debug {
			log.Printf("[hold] move → no dst, dropped from view: obj=%d", itemObj)
		}
		return
	}
	if ref, ok := p.resolveObj(itemObj); ok {
		if !p.sink.IngestPutItem(target, itemObj, ref) {
			p.sink.IngestDeleteItem(itemObj)
			p.bagSlotClear(itemObj)
			if p.debug {
				log.Printf("[hold] move → untracked dst, dropped from view: obj=%d", itemObj)
			}
			return
		}
	} else {
		p.queuePendingPut(itemObj, target)
	}
	if p.debug {
		log.Printf("[hold] move applied: obj=%d → %s", itemObj, target)
	}
}

// ── Loot flow emission (007) ─────────────────────────────────────────────────

// ingestLootObj emits one loot flow event from a resolved object registry entry —
// the single place the loot dedup id ("lt:<itemObjID>") and argument order live, so
// the fast path and the late-declaration path can never drift.
func (p *Pipeline) ingestLootObj(itemObjID int, ref holdings.ItemRef, source string) {
	p.sink.IngestLoot(fmt.Sprintf("lt:%d", itemObjID), ref.Index, ref.Quality, ref.Count, p.nowMS(), source)
}

// emitLootHits turns tracker hits into flow loot events: the item identity, quality
// and stack count come from the object registry (New*Item declaration) — quality-keyed
// valuation works (closes the ADR-022 quality-0 gap for loot). Undeclared objects wait
// in pendingLootResolve until their declaration arrives or the TTL expires.
func (p *Pipeline) emitLootHits(hits []loot.Hit) {
	for _, h := range hits {
		if p.debug {
			log.Printf("[flow] loot hit: itemObj=%d source=%q", h.ItemObjID, h.Source)
		}
		if ref, ok := p.resolveObj(h.ItemObjID); ok {
			p.ingestLootObj(h.ItemObjID, ref, h.Source)
			continue
		}
		now := p.nowMS()
		p.objMu.Lock()
		p.pendingLootResolve.Queue(h.ItemObjID, h.Source, now)
		dropped := p.pendingLootResolve.Dropped()
		p.objMu.Unlock()
		p.logDrops(dropped, &p.lastPendingLootDropLog, now, "loot: %d pending pickups dropped so far (declaration never arrived)")
	}
}

// ── Own-state self containers ────────────────────────────────────────────────

// ingestSelf sets one own-state self-container (bag or equipped) from its slot object
// ids: already-declared objects are placed now, the rest queue in pendingInv keyed to
// this container. Re-runs replace the container wholesale (own-state is a full list).
func (p *Pipeline) ingestSelf(guid, tab string, objIDs []int) {
	slots := p.resolveObjects(objIDs)
	if p.debug {
		idxs := make([]int, len(slots))
		for i, s := range slots {
			idxs[i] = s.Ref.Index
		}
		log.Printf("[self] %s objIDs=%v resolvedItemIdx=%v (%d/%d resolved)", tab, objIDs, idxs, len(slots), len(objIDs))
	}
	p.sink.IngestSelfContainer(guid, tab, slots)
	resolved := make(map[int]bool, len(slots))
	for _, s := range slots {
		resolved[s.ObjID] = true
	}
	now := p.nowMS()
	p.objMu.Lock()
	p.pendingInv.Clear(func(g string) bool { return g == guid }) // stale entries for this container
	for _, id := range objIDs {
		if !resolved[id] {
			p.pendingInv.Queue(id, guid, now)
		}
	}
	p.objMu.Unlock()
}

// ── Small helpers ────────────────────────────────────────────────────────────

// paramKeys returns the sorted parameter keys of a message (debug aid).
func paramKeys(params map[byte]interface{}) []int {
	ks := make([]int, 0, len(params))
	for k := range params {
		ks = append(ks, int(k))
	}
	sort.Ints(ks)
	return ks
}

// extractEMV pulls (index, quality, silver value) from the two EMV layouts.
func extractEMV(params map[byte]interface{}) (index, quality int, value int64, ok bool) {
	if id, okId := firstInt(params[0]); okId {
		if v, okV := firstInt64(params[1]); okV {
			return id, 0, v / emvScale, true
		}
	}
	if id, okId := firstInt(params[2]); okId {
		v, _ := firstInt64(params[4])
		q, _ := firstInt(params[3])
		return id, q, v / emvScale, true
	}
	return 0, 0, 0, false
}

func codeFrom(params map[byte]interface{}, key byte, fallback int) int {
	if v, ok := params[key]; ok {
		if n, ok := firstInt(v); ok {
			return n
		}
		switch n := v.(type) {
		case int16:
			return int(n)
		case int32:
			return int(n)
		}
	}
	return fallback
}

func firstInt(v interface{}) (int, bool) {
	switch a := v.(type) {
	case []int16:
		if len(a) > 0 {
			return int(a[0]), true
		}
	case []int32:
		if len(a) > 0 {
			return int(a[0]), true
		}
	case []byte:
		if len(a) > 0 {
			return int(a[0]), true
		}
	case int16:
		return int(a), true
	case int32:
		return int(a), true
	}
	return 0, false
}

func firstInt64(v interface{}) (int64, bool) {
	switch a := v.(type) {
	case []int32:
		if len(a) > 0 {
			return int64(a[0]), true
		}
	case []int64:
		if len(a) > 0 {
			return a[0], true
		}
	}
	return 0, false
}
