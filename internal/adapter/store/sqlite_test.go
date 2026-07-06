package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	sess := model.CaptureSession{ID: "s1", StartedAt: 1000, SourceKind: model.SourceReplay, Interface: "f.pcap"}
	if err := db.StartSession(ctx, sess); err != nil {
		t.Fatalf("start: %v", err)
	}

	obs := []model.Observation{
		{SessionID: "s1", TS: 1001, Category: model.CatLoot, MessageCode: 98, FieldsPresent: 2, FieldsExpected: 2},
		{SessionID: "s1", TS: 1002, Category: model.CatSilver, MessageCode: 62, FieldsPresent: 3, FieldsExpected: 3},
	}
	if err := db.AppendObservations(ctx, obs); err != nil {
		t.Fatalf("append: %v", err)
	}

	cov := []model.CategoryCoverage{
		{SessionID: "s1", Category: model.CatLoot, ObservedCount: 1, Completeness: 1, Verdict: model.VerdictMedium},
	}
	// Upsert twice — must be idempotent (no duplicate, value updated).
	if err := db.UpsertCoverage(ctx, cov); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	cov[0].ObservedCount = 5
	cov[0].Verdict = model.VerdictHigh
	if err := db.UpsertCoverage(ctx, cov); err != nil {
		t.Fatalf("upsert2: %v", err)
	}

	got, err := db.LoadCoverage(ctx, "s1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("coverage rows = %d, want 1 (idempotent)", len(got))
	}
	if got[0].ObservedCount != 5 || got[0].Verdict != model.VerdictHigh {
		t.Fatalf("upsert did not update: %+v", got[0])
	}

	if err := db.EndSession(ctx, "s1", 2000, model.SessionTotals{DecodedCount: 10}); err != nil {
		t.Fatalf("end: %v", err)
	}

	note := model.ReconciliationNote{SessionID: "s1", Category: model.CatBank, Result: "pass", Notes: "matched", CreatedAt: 1500}
	if err := db.AddReconciliation(ctx, note); err != nil {
		t.Fatalf("recon: %v", err)
	}
	notes, err := db.LoadReconciliations(ctx, "s1")
	if err != nil || len(notes) != 1 || notes[0].Result != "pass" {
		t.Fatalf("recon load: %v notes=%v", err, notes)
	}
}

// TestMigrateOldFlowEventsTable reproduces the live 2026-07-02 failure: a DB whose
// flow_events predates the zone column. Open must ALTER it in, after which writes and
// zone reads work (old rows read back with zone "").
func TestMigrateOldFlowEventsTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "old.db")

	// Build the pre-zone table exactly as the old schema did, with one legacy row.
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE flow_events (
		session_id TEXT, event_id TEXT, kind TEXT, ts INTEGER,
		item_index INTEGER, quality INTEGER, count INTEGER,
		silver INTEGER, fame INTEGER, valued INTEGER, source TEXT,
		PRIMARY KEY (session_id, event_id));
		INSERT INTO flow_events VALUES ('old','e1','silver',100,0,0,1,42,0,1,'');`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("open must migrate, got: %v", err)
	}
	defer db.Close()

	// New-schema write succeeds against the migrated table.
	if err := db.AppendFlowEvents(ctx, "new", []model.FlowEvent{
		{ID: "e2", Kind: model.FlowGather, TS: 200, Silver: 10, Count: 1, Zone: "Pen Gent", Valued: true},
	}); err != nil {
		t.Fatalf("write after migration: %v", err)
	}
	rows, err := db.LoadFlowEvents(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("read after migration: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (legacy + new)", len(rows))
	}
	if rows[0].Zone != "" || rows[0].Silver != 42 {
		t.Fatalf("legacy row = %+v, want zone \"\" silver 42", rows[0])
	}
	if rows[1].Zone != "Pen Gent" {
		t.Fatalf("new row zone = %q, want Pen Gent", rows[1].Zone)
	}
}

func TestLoadFlowEvents(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "load.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	batchA := []model.FlowEvent{
		{ID: "a1", Kind: model.FlowSilver, TS: 100, Silver: 10, Count: 1, Zone: "Z1", Valued: true},
		{ID: "a2", Kind: model.FlowFame, TS: 300, Fame: 50, Count: 1, Zone: "Z1", Valued: true},
	}
	batchB := []model.FlowEvent{
		{ID: "b1", Kind: model.FlowGather, TS: 200, Silver: 20, Count: 2, Zone: "Z2", Valued: true},
	}
	if err := db.AppendFlowEvents(ctx, "sA", batchA); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendFlowEvents(ctx, "sB", batchB); err != nil {
		t.Fatal(err)
	}

	// All sessions, since 0 → 3 rows, ts ASC, fields round-tripped.
	all, err := db.LoadFlowEvents(ctx, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].TS != 100 || all[1].TS != 200 || all[2].TS != 300 {
		t.Fatalf("all = %+v, want 3 rows ts ASC", all)
	}
	if all[1].SessionID != "sB" || all[1].Kind != model.FlowGather || all[1].Zone != "Z2" || all[1].Silver != 20 {
		t.Fatalf("row fields wrong: %+v", all[1])
	}

	// Session filter.
	onlyA, err := db.LoadFlowEvents(ctx, "sA", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyA) != 2 {
		t.Fatalf("session filter → %d rows, want 2", len(onlyA))
	}

	// since filter.
	since, err := db.LoadFlowEvents(ctx, "", 150, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(since) != 2 || since[0].TS != 200 {
		t.Fatalf("since filter → %+v, want ts 200,300", since)
	}

	// Limit keeps the NEWEST rows (then re-sorted ASC).
	lim, err := db.LoadFlowEvents(ctx, "", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lim) != 2 || lim[0].TS != 200 || lim[1].TS != 300 {
		t.Fatalf("limit → %+v, want newest two (200,300) ASC", lim)
	}
}

func TestAppendFlowEventsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "flow.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.StartSession(ctx, model.CaptureSession{ID: "s1", StartedAt: 1000, SourceKind: model.SourceReplay}); err != nil {
		t.Fatalf("start: %v", err)
	}
	batch := []model.FlowEvent{
		{ID: "sv:1:47", Kind: model.FlowSilver, TS: 1001, Count: 1, Silver: 47, Valued: true, Source: "mob"},
		{ID: "lt:9:920:2", Kind: model.FlowLoot, TS: 1002, Item: model.Item{Index: 920}, Count: 2, Valued: false},
	}
	if err := db.AppendFlowEvents(ctx, "s1", batch); err != nil {
		t.Fatalf("append: %v", err)
	}
	// Re-append the loot event now valued → upsert, not duplicate (FR-008 / at-least-once).
	batch[1].Silver = 1000
	batch[1].Valued = true
	if err := db.AppendFlowEvents(ctx, "s1", batch[1:]); err != nil {
		t.Fatalf("re-append: %v", err)
	}

	var rows int
	var lootSilver int64
	var lootValued int
	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM flow_events WHERE session_id='s1'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, `SELECT silver, valued FROM flow_events WHERE event_id='lt:9:920:2'`).Scan(&lootSilver, &lootValued); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("flow rows = %d, want 2 (idempotent upsert, no dup)", rows)
	}
	if lootSilver != 1000 || lootValued != 1 {
		t.Fatalf("loot upsert not applied: silver=%d valued=%d, want 1000/1", lootSilver, lootValued)
	}
}

// 010: the EMV book survives restarts — save, reload, newest-asOf wins on conflict.
func TestEMVBookRoundTrip(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "emv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.SaveEMVBook(ctx, []valuation.EMVEntry{
		{Index: 920, Quality: 1, Amount: 42883, AsOf: 1000},
		{Index: 837, Quality: 4, Amount: 13500, AsOf: 2000},
	}); err != nil {
		t.Fatal(err)
	}
	// Older write must not clobber newer; newer must win.
	if err := db.SaveEMVBook(ctx, []valuation.EMVEntry{
		{Index: 920, Quality: 1, Amount: 1, AsOf: 500},     // stale — ignored
		{Index: 837, Quality: 4, Amount: 14000, AsOf: 3000}, // newer — wins
	}); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadEMVBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[[2]int]valuation.EMVEntry{}
	for _, e := range got {
		byKey[[2]int{e.Index, e.Quality}] = e
	}
	if e := byKey[[2]int{920, 1}]; e.Amount != 42883 {
		t.Fatalf("stale write clobbered: %+v", e)
	}
	if e := byKey[[2]int{837, 4}]; e.Amount != 14000 {
		t.Fatalf("newer write lost: %+v", e)
	}
}

func TestSpecUnlockedRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "spec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SaveSpecUnlocked(ctx, []int{22, 96, 172}); err != nil {
		t.Fatal(err)
	}
	// REPLACE semantics: a second save wholly replaces the set.
	if err := db.SaveSpecUnlocked(ctx, []int{96, 97}); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadSpecUnlocked(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set := map[int]bool{}
	for _, id := range got {
		set[id] = true
	}
	if len(got) != 2 || !set[96] || !set[97] || set[22] {
		t.Fatalf("round-trip/replace wrong: %v", got)
	}
}

func TestSpecEnumRoundTrip(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "enum.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SaveSpecEnum(ctx, []int{6, 22, 99, 150}); err != nil {
		t.Fatal(err)
	}
	// REPLACE: a fresh cold login re-sends the order.
	if err := db.SaveSpecEnum(ctx, []int{7, 8, 9}); err != nil {
		t.Fatal(err)
	}
	got, err := db.LoadSpecEnum(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != 7 || got[2] != 9 {
		t.Fatalf("enum round-trip/order wrong: %v", got)
	}
}
