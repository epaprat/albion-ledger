package app

// Shared headless harness for the feature-023 soak + regression gates (T005). Builds
// the REAL Service + Pipeline over the bundled data catalogs with a nil emitter, and
// a parser wired to the pipeline — the same wiring cmd/soak uses, but in-package so
// the gates can read the aggregate getters directly.

import (
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/data"
	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	wailsadapter "github.com/epaprat/albion-ledger/internal/adapter/wails"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/photon"
	"github.com/epaprat/albion-ledger/internal/specnames"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

// writeGoldenFixture generates the deterministic privacy-safe golden capture into a
// temp file (the real .pcap is gitignored, so it is regenerated per run — the same
// source the committed regression baseline is established from).
func writeGoldenFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "golden.pcap")
	if err := capture.WriteSyntheticFixture(path); err != nil {
		t.Fatalf("write golden fixture: %v", err)
	}
	return path
}

func newSoakHarness(t *testing.T) (*wailsadapter.Service, *photon.PhotonParser) {
	t.Helper()
	cat, err := catalog.New(data.ItemsJSON)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := codes.New(data.CodesJSON)
	if err != nil {
		t.Fatal(err)
	}
	var locs *locations.Locations
	if l, err := locations.New(data.ClustersJSON); err == nil {
		locs = l
	}
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	svc := wailsadapter.NewService(cat, book, val, nil, 5000, nowMS)
	specCat, err := specnames.New(data.SpecNodesJSON)
	if err != nil {
		t.Fatal(err)
	}
	p := New(svc, probe.New(reg), locs, specCat, nowMS, false)
	svc.EnsureSelfContainer(SelfBagGUID, "Bag")
	svc.EnsureSelfContainer(SelfEquipGUID, "Equipped")

	parser := photon.NewPhotonParser(p.OnRequest, p.OnResponse, p.OnEvent)
	parser.OnEncrypted = func() {}
	return svc, parser
}
