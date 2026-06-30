package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
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
