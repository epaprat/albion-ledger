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
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/epaprat/albion-ledger/data"
	"github.com/epaprat/albion-ledger/internal/adapter/aodp"
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/adapter/store"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/app"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/photon"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

//go:embed all:frontend/dist
var assets embed.FS

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
	noExternal := flag.Bool("noexternal", false, "disable the community price feed (AODP) — no outbound HTTP")
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
	var locs *locations.Locations // nil = zones show raw cluster ids
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

	pipe := app.New(svc, probe.New(reg), locs, nowMS, *debugFlowFlag)

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

	runErr := wails.Run(&options.App{
		Title:  "Albion Ledger",
		Width:  1100,
		Height: 720,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			emitter.ctx = ctx
			// Pre-create the player's virtual containers (pinned, marked not-yet-
			// observed) so live puts bridged to them land even before the first
			// own-state snapshot, without faking a fresh empty inventory in the UI.
			svc.EnsureSelfContainer(app.SelfBagGUID, "Bag")
			svc.EnsureSelfContainer(app.SelfEquipGUID, "Equipped")
			if flowStore != nil {
				// Persisted EMV book (010): values learned in earlier sessions keep
				// pricing K-overview summary rows now; the book flushes on shutdown.
				if entries, err := flowStore.LoadEMVBook(ctx); err == nil {
					book.RestoreEMV(entries)
				} else {
					log.Printf("store LoadEMVBook: %v", err)
				}
				if err := flowStore.StartSession(ctx, model.CaptureSession{
					ID: sessionID, StartedAt: nowMS(), SourceKind: srcKind, Interface: *iface,
				}); err != nil {
					log.Printf("store StartSession: %v", err)
				}
				svc.StartFlowPersistence(ctx, flowStore, sessionID)
				svc.SetFlowReader(flowStore, sessionID) // zone analytics read side (006)
			}
			go runCapture(ctx, *iface, *replay, pipe, svc)
			// Community price base layer (AODP, 010): fills valuation gaps for held
			// items shortly after startup, then hourly. In-game observations always
			// override; network failures degrade silently.
			go func() {
				if *noExternal {
					return // explicit opt-out: zero outbound HTTP (Principle V transparency)
				}
				client := aodp.New("")
				t := time.NewTimer(20 * time.Second)
				defer t.Stop()
				refresh := func() {
					if n := svc.RefreshExternalPrices(ctx, client); n > 0 {
						log.Printf("aodp: %d prices fetched", n)
					}
					// Periodic durability: a crash must not discard the session's learned
					// prices — the upsert is idempotent and newest-wins (010 review).
					if flowStore != nil {
						if err := flowStore.SaveEMVBook(ctx, book.SnapshotEMV()); err != nil {
							log.Printf("store SaveEMVBook (periodic): %v", err)
						}
					}
				}
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						refresh()
						t.Reset(time.Hour)
					case <-svc.ExternalRefreshSignal():
						// New vault rows just landed (K overview): give the burst a
						// moment to finish, then fill the gaps — don't wait the hour.
						select {
						case <-ctx.Done():
							return
						case <-time.After(3 * time.Second):
						}
						refresh()
						t.Reset(time.Hour)
					}
				}
			}()
		},
		OnShutdown: func(context.Context) {
			if flowStore != nil {
				svc.StopFlowPersistence() // drain the final batch before the DB closes
				if err := flowStore.SaveEMVBook(context.Background(), book.SnapshotEMV()); err != nil {
					log.Printf("store SaveEMVBook: %v", err)
				}
				_ = flowStore.EndSession(context.Background(), sessionID, nowMS(), model.SessionTotals{})
				_ = flowStore.Close()
			}
		},
		Bind: []interface{}{svc},
	})
	if runErr != nil {
		log.Fatal(runErr)
	}
}

// runCapture drives capture → Photon parse → pipeline dispatch, and periodically
// pushes capture status to the UI. All classification/handler glue lives in
// internal/app (feature 009); this function is setup only.
func runCapture(ctx context.Context, iface, replay string, pipe *app.Pipeline, svc *wailsadapter.Service) {
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

	parser := photon.NewPhotonParser(pipe.OnRequest, pipe.OnResponse, pipe.OnEvent)
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
