// Command albion-ledger is the desktop app (Wails): it captures the game stream
// and shows captured items resolved to names with an estimated value, live.
// Live capture needs the pcap build tag: wails build -tags pcap (run with sudo).
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/epaprat/albion-ledger/data"
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/adapter/store"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/loot"
	"github.com/epaprat/albion-ledger/internal/photon"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

//go:embed all:frontend/dist
var assets embed.FS

const emvEventCode = 466

func nowMS() int64 { return time.Now().UnixMilli() }

// defaultStorePath is the local ledger DB location — OS config dir when available
// (e.g. ~/Library/Application Support/albion-ledger on macOS), else ./captures.
func defaultStorePath() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "albion-ledger", "ledger.db")
	}
	return filepath.Join("captures", "ledger.db")
}

// wailsEmitter bridges the service's Emitter to the Wails event runtime.
type wailsEmitter struct{ ctx context.Context }

func (e *wailsEmitter) Emit(event string, datav ...interface{}) {
	if e.ctx != nil {
		runtime.EventsEmit(e.ctx, event, datav...)
	}
}

func main() {
	iface := flag.String("iface", "", "capture interface (empty = auto)")
	replay := flag.String("replay", "", "replay a recorded pcap instead of live capture")
	catalogPath := flag.String("catalog", "", "override item catalog file (data/items.json format)")
	codesPath := flag.String("codes", "", "override code map file (data/codes.json format)")
	debugFlowFlag := flag.Bool("debugflow", false, "log flow (silver/loot/gather/fame) attribution to stderr")
	flag.Parse()
	debugFlow = *debugFlowFlag

	cat, err := catalog.New(data.ItemsJSON)
	if err != nil {
		log.Fatal(err)
	}
	if *catalogPath != "" {
		if err := cat.Reload(*catalogPath); err != nil {
			log.Printf("catalog override failed, using bundled: %v", err)
		}
	}
	reg, err := codes.New(data.CodesJSON)
	if err != nil {
		log.Fatal(err)
	}
	if l, err := locations.New(data.ClustersJSON); err != nil {
		log.Printf("cluster map unavailable, zones show raw ids: %v", err)
	} else {
		locs = l
	}
	if *codesPath != "" {
		_ = reg.Reload(*codesPath)
	}

	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	emitter := &wailsEmitter{}
	svc := wailsadapter.NewService(cat, book, val, emitter, 5000, nowMS)

	clf := probe.New(reg)

	// Local-first store (Principle VIII): earnings events are persisted to SQLite as
	// they arrive; the in-memory ledger stays bounded (Principle XI). Best-effort — if
	// the store can't open, the app still runs (in-memory only).
	var flowStore *store.SQLite
	sessionID := fmt.Sprintf("app-%d", nowMS())
	if db, err := store.Open(defaultStorePath()); err != nil {
		log.Printf("local store unavailable, running in-memory only: %v", err)
	} else {
		flowStore = db
	}
	srcKind := model.SourceLive
	if *replay != "" {
		srcKind = model.SourceReplay
	}

	app := wails.Run(&options.App{
		Title:  "Albion Ledger",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			emitter.ctx = ctx
			if flowStore != nil {
				if err := flowStore.StartSession(ctx, model.CaptureSession{
					ID: sessionID, StartedAt: nowMS(), SourceKind: srcKind, Interface: *iface,
				}); err != nil {
					log.Printf("store StartSession: %v", err)
				}
				svc.StartFlowPersistence(ctx, flowStore, sessionID)
				svc.SetFlowReader(flowStore, sessionID) // zone analytics read side (006)
			}
			go runCapture(ctx, *iface, *replay, clf, svc)
		},
		OnShutdown: func(context.Context) {
			if flowStore != nil {
				svc.StopFlowPersistence() // drain the final batch before the DB closes
				_ = flowStore.EndSession(context.Background(), sessionID, nowMS(), model.SessionTotals{})
				_ = flowStore.Close()
			}
		},
		Bind: []interface{}{svc},
	})
	if app != nil {
		log.Fatal(app)
	}
}

// runCapture drives capture → Photon parse → classify → service ingest, and
// periodically pushes capture status to the UI.
func runCapture(ctx context.Context, iface, replay string, clf *probe.Classifier, svc *wailsadapter.Service) {
	var src port.PacketSource
	var err error
	if replay != "" {
		src = capture.NewReplay(replay)
	} else {
		src, err = capture.NewLive(iface)
		if err != nil {
			svc.SetStatus(model.CaptureStatusView{Capturing: false, DriftAlert: "capture unavailable: " + err.Error()})
			return
		}
	}

	parser := photon.NewPhotonParser(
		func(opByte byte, params map[byte]interface{}) {
			// The player's own OUTGOING operation requests (passively observed). The op
			// code lives in param 253 like responses; loot correlation needs the item-move
			// requests (feature 007).
			code := codeFrom(params, 253, int(opByte))
			if debugFlow {
				if _, ok := params[0]; ok && (code == 30 || code == 39) {
					log.Printf("[flow] request: code=%d keys=%v", code, paramKeys(params))
				}
			}
			ingest(clf, svc, probe.KindRequest, code, params)
		},
		func(opByte byte, _ int16, _ string, params map[byte]interface{}) {
			// The operation code lives in param 253 for responses (opByte is the raw
			// Photon opcode); own-state Join carries 253=2. Without this, op-2 responses
			// (masteries + login inventory) never classify.
			rc := codeFrom(params, 253, int(opByte))
			// Self-identity (own objId + name) rides on EVERY Join op-2 — the login Join
			// AND every zone-change Join (where userObjectId changes per map). Read it here,
			// independent of the key-55 (bag) guard that gates own-state/character_spec, so
			// self stays fresh across zones and is set even when the app starts mid-session.
			// Require the REAL response op code (key 253 == 2): the raw-opByte fallback can
			// collide on unrelated/partially-decoded responses, and a foreign message whose
			// key 0 parses as an int would silently corrupt selfObjID and all attribution.
			if fromKey, ok := capture.IntParam(params, 253); ok && fromKey == 2 {
				updateSelf(svc, params)
			}
			ingest(clf, svc, probe.KindResponse, rc, params)
		},
		func(evByte byte, params map[byte]interface{}) {
			code := codeFrom(params, 252, int(evByte))
			registerNewItem(svc, code, params) // declare object ids + feed EMV before containers reference them
			ingest(clf, svc, probe.KindEvent, code, params)
		},
	)
	parser.OnEncrypted = func() {}

	// Status ticker. Every 30th tick it also re-broadcasts the flow summary so the
	// session's idle auto-close and live elapsed/rate stay visible between earnings.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		ticks := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st := src.Status()
				svc.SetStatus(model.CaptureStatusView{
					Capturing: st.Capturing, Interface: st.Interface,
					GameServer: st.GameServer, EncryptedRate: st.EncryptedRate,
					Decoded: st.Decoded,
				})
				if ticks++; ticks%30 == 0 {
					svc.EmitFlowNow()
				}
			}
		}
	}()

	ch, err := src.Packets(ctx)
	if err != nil {
		svc.SetStatus(model.CaptureStatusView{Capturing: false, DriftAlert: err.Error()})
		return
	}
	for payload := range ch {
		parser.ReceivePacket(payload)
	}
}


// ingest routes a classified message into the views (market value + holdings + spec).
func ingest(clf *probe.Classifier, svc *wailsadapter.Service, kind probe.Kind, code int, params map[byte]interface{}) {
	cl, ok := clf.Classify(kind, code, params)
	if !ok {
		return
	}
	switch cl.Category {
	case model.CatItemValueEMV:
		if idx, quality, value, ok := extractEMV(params); ok {
			svc.IngestEMV(idx, quality, value, nowMS())
		}
	case model.CatInventory: // AttachItemContainer — key-3 slots are in-world object ids,
		// resolved to item type+quality via the object registry (New*Item declarations).
		if cGUID, ownerGUID, objIDs, ok := capture.ContainerItems(params); ok {
			svc.IngestContainer(cGUID, ownerGUID, resolveObjects(objIDs))
		}
		// Loot correlation also needs this container: source link + SLOT-INDEXED map
		// (empties preserved), and attaching may resolve moves that arrived early.
		if guid, srcObjID, slots, ok := capture.ContainerSlots(params); ok {
			emitLootHits(svc, lootTracker.AttachContainer(guid, srcObjID, slots, nowMS()))
		}
	case model.CatBank: // BankVaultInfo — declares the bank tabs (owner GUIDs + names)
		if owners, tabNames, ok := capture.BankVault(params); ok {
			svc.IngestBankVault(owners, tabNames)
		}
	case model.CatCharacterSpec: // own-state (op-2 with key 55): the login bag (key 55) + equipped (key 52)
		// NOTE: key 55 is the bag and key 52 the worn set — neither is masteries (the
		// earlier reading was wrong); the real spec source is TBD, so Spec stays empty.
		// Self-identity is handled in the response callback (updateSelf) for EVERY op-2,
		// not here, so it is not gated by the key-55 guard (login-only) — see runCapture.
		if objIDs, ok := capture.OwnInventory(params); ok {
			ingestSelf(svc, selfBagGUID, "Bag", objIDs)
		}
		if objIDs, ok := capture.OwnEquipped(params); ok {
			ingestSelf(svc, selfEquipGUID, "Equipped", objIDs)
		}
	case model.CatCurrentLocation: // notification event 163 — "you entered <city>"
		if city, ok := capture.CurrentCity(params); ok {
			svc.SetCurrentCity(city)
			svc.SetZone(city) // readable city name overrides the raw cluster id for flow zone
		}
	case model.CatInventoryPut: // event 26 — item added/moved into a container (live)
		if objID, cGUID, ok := capture.PutItem(params); ok {
			if ref, ok := resolveObj(objID); ok {
				svc.IngestPutItem(cGUID, objID, ref)
			}
		}
	case model.CatInventoryDelete: // event 27 — item removed from a container (live)
		if objID, ok := capture.DeleteItem(params); ok {
			svc.IngestDeleteItem(objID)
		}
	case model.CatSilver: // TakeSilver (62) — own net silver (US1)
		if target, obj, net, ok := capture.SilverEvent(params); ok {
			if debugFlow {
				log.Printf("[flow] silver evt: obj=%d target=%d net=%d self=%d match=%v", obj, target, net, selfObjID, isSelfObj(obj))
			}
			// key0 (obj) is the RECEIVING player (self); key2 is the mob/target entity.
			// Live-verified 2026-07-01: obj==self across a session, target varies per mob.
			if isSelfObj(obj) && net != 0 {
				// Per-event nonce: identical yields are common (same mob type, AoE kills in
				// one wire tick) and key 1 (timestamp) is not guaranteed — without the seq,
				// equal-net pickups collapse into one id and undercount. Wire-level
				// retransmits are already deduped by the Photon reliable layer.
				ts, _ := capture.IntParam(params, 1)
				id := fmt.Sprintf("sv:%d:%d:%d", ts, net, nextFlowSeq())
				svc.IngestSilver(id, net, nowMS(), "")
			}
		}
	case model.CatLoot: // OtherGrabbedLoot (279) — own looted items (US2); 98 has no items
		if debugFlow {
			log.Printf("[flow] loot category: code=%d keys=%v", code, paramKeys(params))
		}
		if code != 279 { // NewLoot(98) shares the category but has a different key layout —
			return // feeding it to LootGrab could fabricate a phantom loot event.
		}
		if looter, isSilver, itemID, amount, ok := capture.LootGrab(params); ok {
			if debugFlow {
				log.Printf("[flow] loot evt: looter=%q item=%d amt=%d isSilver=%v self=%q match=%v", looter, itemID, amount, isSilver, selfName, isSelfName(looter))
			}
			if !isSilver && isSelfName(looter) {
				// Per-event nonce: two identical stacks from one container are distinct
				// grabs — without the seq they collapse into one id (same fix as harvest).
				container, _ := capture.IntParam(params, 0)
				id := fmt.Sprintf("lt:%d:%d:%d:%d", container, itemID, amount, nextFlowSeq())
				// OtherGrabbedLoot carries no quality → normal (0); resources are unquality anyway.
				svc.IngestLoot(id, itemID, 0, amount, nowMS(), "")
			}
		}
	case model.CatGatherFishing: // HarvestFinished (61) + RewardGranted (267) — own gathers (US3)
		switch code {
		case 61:
			if gatherer, itemID, amount, ok := capture.HarvestEvent(params); ok && isSelfObj(gatherer) {
				// Each harvest tick on the SAME node is a distinct gain (a node yields
				// several charges), so the dedup id must be per-tick unique — keying by
				// node+item collapsed multiple charges into one (~3× undercount). Photon
				// transport already drops re-delivered packets, so a monotonic seq is safe.
				node, _ := capture.IntParam(params, 3)
				id := fmt.Sprintf("hv:%d:%d:%d", node, itemID, nextFlowSeq())
				if debugFlow {
					std, _ := capture.IntParam(params, 5)
					bonus, _ := capture.IntParam(params, 6)
					prem, _ := capture.IntParam(params, 7)
					log.Printf("[flow] harvest: node=%d item=%d std=%d bonus=%d prem=%d total=%d", node, itemID, std, bonus, prem, amount)
				}
				svc.IngestGather(id, itemID, 0, amount, nowMS(), "")
			}
		case 267:
			if itemID, qty, ok := capture.RewardEvent(params); ok {
				id := fmt.Sprintf("rw:%d:%d:%d", itemID, qty, nextFlowSeq())
				if debugFlow {
					log.Printf("[flow] reward: item=%d qty=%d", itemID, qty)
				}
				svc.IngestGather(id, itemID, 0, qty, nowMS(), "")
			}
			// code 38 is unverified (see codes.json / tasks T031) — intentionally ignored.
		}
	case model.CatLootSource: // NewLoot(98) / NewLootChest(393) / LootChestOpened(395)
		if objID, name, ok := capture.LootSource(params, code); ok {
			lootTracker.RegisterSource(objID, name, nowMS())
			if debugFlow {
				log.Printf("[flow] loot source: code=%d obj=%d name=%q", code, objID, name)
			}
		}
	case model.CatLootMove: // own item-move requests (op-30 single, op-39 take-all)
		switch code {
		case 30:
			if guid, slot, ok := capture.MoveItem(params); ok {
				emitLootHits(svc, lootTracker.ResolveMove(guid, slot, nowMS()))
			}
		case 39:
			if guid, ids, ok := capture.MoveGivenItems(params); ok {
				emitLootHits(svc, lootTracker.ResolveMoveGiven(guid, ids, nowMS()))
			}
		}
	case model.CatFame: // UpdateFame (82 event) — fame is inherently own (US4)
		if fame, ok := capture.FameEvent(params); ok && fame > 0 {
			// Per-event nonce: key 1 (running total) is unverified — if absent, every
			// id would be "fm:0" and all fame after the first tick would be dropped.
			total, _ := capture.IntParam(params, 1)
			id := fmt.Sprintf("fm:%d:%d", total, nextFlowSeq())
			svc.IngestFame(id, fame, nowMS())
		}
	}
}

// self identity for own-earning attribution (005). Set from the Join own-state
// response; touched only on the single capture goroutine, so no lock needed.
var (
	selfObjID int
	selfName  string
	debugFlow bool  // -debugflow: log flow attribution to stderr
	flowSeq   int64 // monotonic nonce for flow events lacking a natural unique wire id
	locs      *locations.Locations // cluster-id → zone-name resolver (nil = raw ids)
)

// zoneName resolves a raw cluster id to a readable zone name when the map is loaded.
func zoneName(clusterID string) string {
	if locs != nil {
		return locs.Resolve(clusterID)
	}
	return clusterID
}

// nextFlowSeq returns a per-event unique nonce (capture runs on one goroutine, so a
// plain increment is race-free) for building dedup ids of events like harvest ticks
// that legitimately repeat with identical field values.
func nextFlowSeq() int64 { flowSeq++; return flowSeq }

// updateSelf refreshes the local player's own object id + name from a Join op-2
// response (key 0 objId, key 2 name). Called for every op-2 so self stays correct
// across zone changes (objId changes per map) and is set even mid-session. Requires
// BOTH fields — a partial match (e.g. a stray int at key 0 of a non-Join op-2 variant)
// must never overwrite a good identity with garbage.
func updateSelf(svc *wailsadapter.Service, params map[byte]interface{}) {
	objID, name, ok := capture.SelfIdentity(params)
	if !ok || objID <= 0 || name == "" {
		return
	}
	selfObjID = objID
	selfName = name
	svc.SetSelf(objID, name)
	// The Join also carries the current location/cluster (key 8) — stamp it as the zone
	// so flow events know where they happened (per-zone analytics, 006). Open-world zones
	// only surface here (event 163 covers cities); raw cluster id is fine, named later.
	if zone, zok := params[8].(string); zok && zone != "" {
		svc.SetZone(zoneName(zone)) // raw cluster id → readable zone name
	}
	if debugFlow {
		log.Printf("[flow] self set: objID=%d name=%q (op-2)", selfObjID, selfName)
	}
}

// isSelfObj reports whether an object id is the local player. Until self is known
// (selfObjID==0) it returns false so we never count another player's earnings.
func isSelfObj(objID int) bool { return selfObjID != 0 && objID == selfObjID }

// isSelfName reports whether a looter name is the local player (case-sensitive as
// the wire delivers it). Empty self name → false (don't count until known).
func isSelfName(name string) bool { return selfName != "" && name == selfName }

// lootTracker correlates own move requests with loot sources (feature 007). Touched
// only on the capture goroutine (its own mutex guards the internals anyway).
var lootTracker = loot.New()

// pendingLootResolve holds loot hits whose New*Item declaration hasn't arrived yet
// (itemObjID → source label); drained in registerNewItem like pendingInv. Bounded.
var pendingLootResolve = map[int]string{}

const pendingLootResolveCap = 256

// emitLootHits turns tracker hits into flow loot events: the item identity, quality
// and stack count come from the object registry (New*Item declaration) — quality-keyed
// valuation works (closes the ADR-022 quality-0 gap for loot). Undeclared objects wait
// in pendingLootResolve until their declaration arrives.
func emitLootHits(svc *wailsadapter.Service, hits []loot.Hit) {
	for _, h := range hits {
		if debugFlow {
			log.Printf("[flow] loot hit: itemObj=%d source=%q", h.ItemObjID, h.Source)
		}
		if ref, ok := resolveObj(h.ItemObjID); ok {
			svc.IngestLoot(fmt.Sprintf("lt:%d", h.ItemObjID), ref.Index, ref.Quality, ref.Count, nowMS(), h.Source)
			continue
		}
		objMu.Lock()
		if len(pendingLootResolve) < pendingLootResolveCap {
			pendingLootResolve[h.ItemObjID] = h.Source
		} else if debugFlow {
			log.Printf("[flow] loot hit dropped (pending resolve full): itemObj=%d", h.ItemObjID)
		}
		objMu.Unlock()
	}
}

// paramKeys returns the sorted parameter keys of a message (debug aid).
func paramKeys(params map[byte]interface{}) []int {
	ks := make([]int, 0, len(params))
	for k := range params {
		ks = append(ks, int(k))
	}
	sort.Ints(ks)
	return ks
}

// objReg maps in-world object ids to their item type+quality. Container slots
// (AttachItemContainer key 3) reference object ids, so New*Item events (codes 30-37,
// which declare objectId→itemType+quality) must be captured first to resolve a container.
var (
	objMu    sync.Mutex
	objReg   = map[int]holdings.ItemRef{}
	objOrder []int
)

const objRegCap = 50_000

// registerNewItem records objectId → {itemIndex, quality, count} from a New*Item
// event and feeds the item's server EstimatedMarketValue into valuation. Field map
// (reference client NewItem): key 1 = itemIndex, key 2 = quantity, key 4 = EMV
// (scaled ×10000), key 6 = quality, key 7 = durability.
func registerNewItem(svc *wailsadapter.Service, code int, params map[byte]interface{}) {
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
	objMu.Lock()
	if _, exists := objReg[objID]; !exists {
		if len(objReg) >= objRegCap && len(objOrder) > 0 {
			delete(objReg, objOrder[0])
			objOrder = objOrder[1:]
		}
		objOrder = append(objOrder, objID)
	}
	ref := holdings.ItemRef{Index: idx, Quality: quality, Count: count}
	objReg[objID] = ref
	pendGUID := pendingInv[objID] // own-state slot awaiting its declaration ("" = none)
	delete(pendingInv, objID)
	lootSrc, lootPending := pendingLootResolve[objID] // loot hit awaiting this declaration
	delete(pendingLootResolve, objID)
	objMu.Unlock()

	// An own-state bag/equipped object declared after the fact: place it into its
	// self-container now that it resolves.
	if pendGUID != "" {
		svc.IngestPutItem(pendGUID, objID, ref)
	}
	// A loot pickup whose declaration arrived after the move: emit it now (007).
	if lootPending {
		svc.IngestLoot(fmt.Sprintf("lt:%d", objID), ref.Index, ref.Quality, ref.Count, nowMS(), lootSrc)
	}

	// The item's own EstimatedMarketValue (key 4, a scalar int64) is the value the game
	// shows when you open it — feed it to valuation so held items are valued without a
	// market capture.
	if emv, ok := capture.IntParam(params, 4); ok && emv > 0 {
		svc.IngestEMV(idx, quality, int64(emv)/emvScale, nowMS())
	}
}

// Fixed container ids for the player's own bag + equipped sets, which arrive in
// own-state slot arrays without a wire GUID. They group under the inventory city as
// separate tabs.
const (
	selfBagGUID   = "self-bag"
	selfEquipGUID = "self-equipped"
)

// pendingInv maps an own-state object id (bag/equipped) not yet declared by a New*Item
// to the self-container it belongs to; it is placed there when its declaration arrives.
// Reset per own-state, so it is naturally small (bag + equipped); pendingInvCap is a
// hard backstop against unbounded growth (Principle XI).
var pendingInv = map[int]string{}

const pendingInvCap = 1024

// ingestSelf sets one own-state self-container (bag or equipped) from its slot object
// ids: already-declared objects are placed now, the rest queue in pendingInv keyed to
// this container. Re-runs replace the container wholesale (own-state is a full list).
func ingestSelf(svc *wailsadapter.Service, guid, tab string, objIDs []int) {
	slots := resolveObjects(objIDs)
	if debugFlow {
		idxs := make([]int, len(slots))
		for i, s := range slots {
			idxs[i] = s.Ref.Index
		}
		log.Printf("[self] %s objIDs=%v resolvedItemIdx=%v (%d/%d resolved)", tab, objIDs, idxs, len(slots), len(objIDs))
	}
	svc.IngestSelfContainer(guid, tab, slots)
	resolved := make(map[int]bool, len(slots))
	for _, s := range slots {
		resolved[s.ObjID] = true
	}
	objMu.Lock()
	for id, g := range pendingInv { // clear this container's stale pending entries
		if g == guid {
			delete(pendingInv, id)
		}
	}
	for _, id := range objIDs {
		if !resolved[id] && len(pendingInv) < pendingInvCap {
			pendingInv[id] = guid
		}
	}
	objMu.Unlock()
}

// resolveObjects maps container object ids to slot items (objId + ref), skipping
// unresolved ones. The objId is kept so incremental moves can target the item.
func resolveObjects(objIDs []int) []holdings.SlotItem {
	objMu.Lock()
	defer objMu.Unlock()
	slots := make([]holdings.SlotItem, 0, len(objIDs))
	for _, id := range objIDs {
		if r, ok := objReg[id]; ok {
			slots = append(slots, holdings.SlotItem{ObjID: id, Ref: r})
		}
	}
	return slots
}

// resolveObj returns the ref for a single object id (for incremental Put).
func resolveObj(objID int) (holdings.ItemRef, bool) {
	objMu.Lock()
	defer objMu.Unlock()
	r, ok := objReg[objID]
	return r, ok
}

// emvScale: the server EMV is stored scaled by 10000 (silver = raw / 10000).
const emvScale = 10000

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
