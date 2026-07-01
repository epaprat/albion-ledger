// Command albion-ledger is the desktop app (Wails): it captures the game stream
// and shows captured items resolved to names with an estimated value, live.
// Live capture needs the pcap build tag: wails build -tags pcap (run with sudo).
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/epaprat/albion-ledger/data"
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/photon"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

//go:embed all:frontend/dist
var assets embed.FS

const emvEventCode = 466

func nowMS() int64 { return time.Now().UnixMilli() }

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
	flag.Parse()

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
	if *codesPath != "" {
		_ = reg.Reload(*codesPath)
	}

	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	emitter := &wailsEmitter{}
	svc := wailsadapter.NewService(cat, book, val, emitter, 5000, nowMS)

	clf := probe.New(reg)

	app := wails.Run(&options.App{
		Title:  "Albion Ledger",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			emitter.ctx = ctx
			go runCapture(ctx, *iface, *replay, clf, svc)
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
		nil,
		func(opByte byte, _ int16, _ string, params map[byte]interface{}) {
			// The operation code lives in param 253 for responses (opByte is the raw
			// Photon opcode); own-state Join carries 253=2. Without this, op-2 responses
			// (masteries + login inventory) never classify.
			ingest(clf, svc, probe.KindResponse, codeFrom(params, 253, int(opByte)), params)
		},
		func(evByte byte, params map[byte]interface{}) {
			code := codeFrom(params, 252, int(evByte))
			registerNewItem(svc, code, params) // declare object ids + feed EMV before containers reference them
			ingest(clf, svc, probe.KindEvent, code, params)
		},
	)
	parser.OnEncrypted = func() {}

	// Status ticker.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st := src.Status()
				svc.SetStatus(model.CaptureStatusView{
					Capturing: st.Capturing, Interface: st.Interface,
					GameServer: st.GameServer, EncryptedRate: st.EncryptedRate,
				})
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
	case model.CatBank: // BankVaultInfo — declares the bank tabs (owner GUIDs + names)
		if owners, tabNames, ok := capture.BankVault(params); ok {
			svc.IngestBankVault(owners, tabNames)
		}
	// NewEquipmentItem(30) is an equipment-item DECLARATION (handled by the object
	// registry), not the player's worn loadout — so it does not populate "equipped".
	// The real equipped source is the equipment container; identifying it is future work.
	case model.CatCharacterSpec: // own-state (op-2): masteries + the login inventory baseline
		if levels, ok := capture.MasteryLevels(params); ok {
			svc.SetSpec(levels)
		}
		if objIDs, ok := capture.OwnInventory(params); ok {
			ingestSelfInventory(svc, objIDs)
		}
	case model.CatCurrentLocation: // notification event 163 — "you entered <city>"
		if city, ok := capture.CurrentCity(params); ok {
			svc.SetCurrentCity(city)
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
	}
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
	wasPending := pendingInv[objID] // login-inventory slot awaiting its declaration
	delete(pendingInv, objID)
	objMu.Unlock()

	// A login-inventory object (from own-state key 52) declared after the fact: place
	// it into the inventory now that it resolves.
	if wasPending {
		svc.IngestPutItem(selfInvGUID, objID, ref)
	}

	// The item's own EstimatedMarketValue (key 4, a scalar int64) is the value the game
	// shows when you open it — feed it to valuation so held items are valued without a
	// market capture.
	if emv, ok := capture.IntParam(params, 4); ok && emv > 0 {
		svc.IngestEMV(idx, quality, int64(emv)/emvScale, nowMS())
	}
}

// selfInvGUID/selfInvOwner identify the player's own inventory container. Its baseline
// comes from own-state key 52 (Join), which carries no container GUID, so we use a
// fixed one; selfInvOwner is deliberately not a bank owner so it groups as inventory.
const selfInvGUID = "self-inventory"
const selfInvOwner = "self"

// pendingInv holds inventory object ids seen in own-state but not yet declared by a
// New*Item; each is placed into the inventory when its declaration arrives.
var pendingInv = map[int]bool{}

// ingestSelfInventory sets the player inventory from own-state slot object ids: the
// already-declared ones are placed now, the rest are queued in pendingInv.
func ingestSelfInventory(svc *wailsadapter.Service, objIDs []int) {
	slots := resolveObjects(objIDs)
	svc.IngestContainer(selfInvGUID, selfInvOwner, slots)
	resolved := make(map[int]bool, len(slots))
	for _, s := range slots {
		resolved[s.ObjID] = true
	}
	objMu.Lock()
	for k := range pendingInv {
		delete(pendingInv, k)
	}
	for _, id := range objIDs {
		if !resolved[id] {
			pendingInv[id] = true
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
