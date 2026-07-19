package main

// Headless harness (feature 023): build the REAL Service + Pipeline exactly as
// main() does, minus the Wails webview and the SQLite store, and drive it from a
// PacketSource. The Service (with a nil emitter) already satisfies the app.Sink
// port and exposes the aggregate getters, so replayed packets produce real
// holdings/flow/trade/spec state offline — no mock of the pipeline, the pipeline
// itself (plan.md structure decision).

import (
	"context"
	"fmt"
	"time"

	"github.com/epaprat/albion-ledger/data"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/app"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/photon"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/specnames"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

func nowMS() int64 { return time.Now().UnixMilli() }

// harness bundles the headless service + pipeline + parser for one replay run.
type harness struct {
	svc    *wailsadapter.Service
	pipe   *app.Pipeline
	parser *photon.PhotonParser
}

// buildHarness wires the app like main() minus store/webview. When bagProbe is
// true the pipeline's read-only bag-move probe is enabled (US2).
func buildHarness(bagProbe bool) (*harness, error) {
	cat, err := catalog.New(data.ItemsJSON)
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	reg, err := codes.New(data.CodesJSON)
	if err != nil {
		return nil, fmt.Errorf("codes: %w", err)
	}
	var locs *locations.Locations
	if l, err := locations.New(data.ClustersJSON); err == nil {
		locs = l
	}
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	svc := wailsadapter.NewService(cat, book, val, nil, 5000, nowMS) // nil emitter = headless

	specCat, err := specnames.New(data.SpecNodesJSON)
	if err != nil {
		return nil, fmt.Errorf("specnames: %w", err)
	}
	pipe := app.New(svc, probe.New(reg), locs, specCat, nowMS, false)
	svc.EnsureSelfContainer(app.SelfBagGUID, "Bag")
	svc.EnsureSelfContainer(app.SelfEquipGUID, "Equipped")
	if bagProbe {
		pipe.EnableBagProbe() // US2: read-only object-id reuse probe
	}

	parser := photon.NewPhotonParser(pipe.OnRequest, pipe.OnResponse, pipe.OnEvent)
	parser.OnEncrypted = func() {}
	return &harness{svc: svc, pipe: pipe, parser: parser}, nil
}

// feed drains a PacketSource through the parser into the pipeline to completion (or
// ctx cancel). Returns the number of payloads processed. Mirrors main.runCapture's
// packet loop without the status ticker.
func (h *harness) feed(ctx context.Context, src port.PacketSource) (int, error) {
	ch, err := src.Packets(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for payload := range ch {
		h.parser.ReceivePacket(payload)
		n++
	}
	return n, nil
}
