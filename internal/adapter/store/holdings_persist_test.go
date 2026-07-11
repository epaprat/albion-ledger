package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/flow"
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

// 020 US2 / C3 — wallet balance round-trips; a fresh DB reports ok=false (excluded).
func TestWalletRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, _, ok, err := db.LoadWallet(ctx); err != nil || ok {
		t.Fatalf("empty DB must report ok=false, got ok=%v err=%v", ok, err)
	}
	if err := db.SaveWallet(ctx, 81_000_000, 1700); err != nil {
		t.Fatal(err)
	}
	silver, seen, ok, err := db.LoadWallet(ctx)
	if err != nil || !ok || silver != 81_000_000 || seen != 1700 {
		t.Fatalf("wallet round-trip wrong: silver=%d seen=%d ok=%v err=%v", silver, seen, ok, err)
	}
	// Newest-wins upsert (single row).
	if err := db.SaveWallet(ctx, 90_000_000, 1800); err != nil {
		t.Fatal(err)
	}
	if silver, _, _, _ := db.LoadWallet(ctx); silver != 90_000_000 {
		t.Fatalf("wallet upsert must replace, got %d", silver)
	}
}

// 020 US3 / C4 — spec board JSON round-trips; empty DB reports ok=false.
func TestSpecBoardRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, _, ok, _ := db.LoadSpecBoard(ctx); ok {
		t.Fatal("empty DB must report ok=false for spec board")
	}
	if err := db.SaveSpecBoard(ctx, `{"masteries":[{"index":1}]}`, 1700); err != nil {
		t.Fatal(err)
	}
	board, seen, ok, err := db.LoadSpecBoard(ctx)
	if err != nil || !ok || seen != 1700 || board != `{"masteries":[{"index":1}]}` {
		t.Fatalf("spec board round-trip wrong: %q seen=%d ok=%v err=%v", board, seen, ok, err)
	}
}

// 020 US4 / C5 — flow checkpoint upserts to one row, loads back, and deletes; completed
// sessions append to a capped history.
func TestFlowCheckpointAndSessionStore(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	if _, ok, _ := db.LoadFlowCheckpoint(ctx); ok {
		t.Fatal("empty DB must have no checkpoint")
	}
	cp := flow.Checkpoint{StartedMS: 1000, LastActivityMS: 2000, NetSilver: 300, EventCount: 2,
		Items: []flow.CheckpointItem{{Index: 920, Qty: 5}}}
	if err := db.SaveFlowCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}
	got, ok, err := db.LoadFlowCheckpoint(ctx)
	if err != nil || !ok || got.NetSilver != 300 || len(got.Items) != 1 || got.Items[0].Qty != 5 {
		t.Fatalf("checkpoint round-trip wrong: %+v ok=%v err=%v", got, ok, err)
	}
	// Upsert stays a single row.
	cp.NetSilver = 400
	if err := db.SaveFlowCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := db.LoadFlowCheckpoint(ctx); got.NetSilver != 400 {
		t.Fatalf("checkpoint upsert failed: %d", got.NetSilver)
	}
	if err := db.DeleteFlowCheckpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.LoadFlowCheckpoint(ctx); ok {
		t.Fatal("checkpoint must be gone after delete")
	}

	// Completed-session history.
	if err := db.SaveFlowSession(ctx, flow.CompletedFromCheckpoint(cp)); err != nil {
		t.Fatal(err)
	}
	sessions, err := db.LoadFlowSessions(ctx, 10)
	if err != nil || len(sessions) != 1 || sessions[0].NetSilver != 400 {
		t.Fatalf("session history wrong: %+v err=%v", sessions, err)
	}
}
