package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/holdings"
)

// 020 US1 / C1 — a holdings container round-trips through the store, and re-saving the same
// stable id REPLACES it (no duplicate row) — the replace-on-new persistence contract.
func TestHoldingsContainerRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	c := holdings.ContainerSnapshot{
		GUID: "vault:Lymhurst:Bank", Location: model.LocBank, City: "Lymhurst", Tab: "Bank",
		LastSeen: 12345, Pinned: false,
		Items: []model.HoldingItem{{ObjID: 1, Item: model.Item{Index: 920}, Count: 3, LastSeen: 12345}},
	}
	if err := db.SaveContainer(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadContainers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GUID != "vault:Lymhurst:Bank" || got[0].City != "Lymhurst" ||
		got[0].LastSeen != 12345 || len(got[0].Items) != 1 || got[0].Items[0].Count != 3 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Re-save the SAME id with new items → replace, not a second row.
	c.Items = []model.HoldingItem{{ObjID: 2, Item: model.Item{Index: 837}, Count: 1}}
	if err := db.SaveContainer(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, _ = db.LoadContainers(ctx)
	if len(got) != 1 || len(got[0].Items) != 1 || got[0].Items[0].Item.Index != 837 {
		t.Fatalf("replace-on-new failed: %+v", got)
	}
}

// 020 US1 — corrupt items_json is skipped, never crashing hydration (FR-008).
func TestHoldingsContainerCorruptSkipped(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	// One good row, one hand-inserted corrupt row.
	if err := db.SaveContainer(ctx, holdings.ContainerSnapshot{GUID: "good", LastSeen: 1,
		Items: []model.HoldingItem{{ObjID: 1, Item: model.Item{Index: 920}, Count: 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.ExecContext(ctx, `INSERT INTO holdings_containers (container_id, location, city, tab, items_json, last_seen, pinned) VALUES ('bad','','','','{not json',1,0)`); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadContainers(ctx)
	if err != nil {
		t.Fatalf("corrupt row must not fail the whole load: %v", err)
	}
	if len(got) != 1 || got[0].GUID != "good" {
		t.Fatalf("only the good container should survive, got %+v", got)
	}
}
