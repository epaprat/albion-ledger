// Command albion-ledger is the desktop app (Wails): it captures the game stream
// and shows captured items resolved to names with an estimated value, live.
// Live capture needs the pcap build tag: wails build -tags pcap (run with sudo).
package main

import (
	"context"
	"embed"
	"flag"
	"log"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/epaprat/albion-ledger/data"
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/cities"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
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

	cityTable = cities.New(data.CitiesJSON)

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
			ingest(clf, svc, probe.KindResponse, int(opByte), params)
		},
		func(evByte byte, params map[byte]interface{}) {
			code := codeFrom(params, 252, int(evByte))
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
	case model.CatInventory: // AttachItemContainer — key-3 slots are item indices (FR-001)
		if cGUID, ownerGUID, slots, ok := capture.ContainerItems(params); ok {
			svc.IngestContainerSlots(cGUID, ownerGUID, slots)
		}
	case model.CatBank: // BankVaultInfo — declares the bank tabs (owner GUIDs + names)
		if owners, tabNames, ok := capture.BankVault(params); ok {
			svc.IngestBankVault(owners, tabNames)
		}
	// NewEquipmentItem(30) is an equipment-item DECLARATION (handled by the object
	// registry), not the player's worn loadout — so it does not populate "equipped".
	// The real equipped source is the equipment container; identifying it is future work.
	case model.CatCharacterSpec: // own-state masteries
		if levels, ok := capture.MasteryLevels(params); ok {
			svc.SetSpec(levels)
		}
	case model.CatCurrentLocation: // Join response key 8 — the player's own current city
		if id, ok := capture.CurrentCity(params); ok {
			svc.SetCurrentCity(cityTable.Name(id))
		}
	}
}

// cityTable maps location ids → city display names (loaded from data/cities.json).
var cityTable *cities.Table

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
