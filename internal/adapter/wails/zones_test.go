package wailsadapter

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/epaprat/albion-ledger/internal/adapter/store"
	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

// newZoneSvc builds a Service wired to a real temp SQLite store, with a fixed "now".
func newZoneSvc(t *testing.T, nowMS int64) (*Service, *store.SQLite) {
	t.Helper()
	c, err := catalog.New([]byte(cat))
	if err != nil {
		t.Fatal(err)
	}
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	db, err := store.Open(filepath.Join(t.TempDir(), "zones.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewService(c, book, val, &fakeEmitter{}, 100, func() int64 { return nowMS })
	s.SetFlowReader(db, "sesNow")
	return s, db
}

func seed(t *testing.T, db *store.SQLite, session string, events []model.FlowEvent) {
	t.Helper()
	if err := db.AppendFlowEvents(context.Background(), session, events); err != nil {
		t.Fatal(err)
	}
}

func TestZoneStatsSessionWindowAndConversion(t *testing.T) {
	now := int64(10_000_000)
	s, db := newZoneSvc(t, now)
	min := int64(60_000)

	// Current session: two zones; old session: must be excluded from "session" window.
	seed(t, db, "sesNow", []model.FlowEvent{
		{ID: "1", Kind: model.FlowSilver, TS: now - 10*min, Silver: 100, Count: 1, Zone: "Pen Gent", Valued: true},
		{ID: "2", Kind: model.FlowSilver, TS: now - 8*min, Silver: 100, Count: 1, Zone: "Pen Gent", Valued: true},
		{ID: "3", Kind: model.FlowSilver, TS: now - 5*min, Silver: 5, Count: 1, Zone: "", Valued: true}, // unlabeled
	})
	seed(t, db, "sesOld", []model.FlowEvent{
		{ID: "4", Kind: model.FlowSilver, TS: now - 9*min, Silver: 9999, Count: 1, Zone: "OldZone", Valued: true},
	})

	stats := s.ZoneStats("session")
	if len(stats) != 2 {
		t.Fatalf("session window → %d zones, want 2 (OldZone excluded)", len(stats))
	}
	for _, z := range stats {
		if z.Zone == "OldZone" {
			t.Fatal("other session leaked into 'session' window")
		}
		if z.Zone == "" {
			t.Fatal("empty zone label must render as 'Unknown zone'")
		}
	}
	pg := stats[0] // sorted silver/hr desc → Pen Gent first (2m active chain)
	if pg.Zone != "Pen Gent" || pg.NetSilver != 200 {
		t.Fatalf("top zone = %+v, want Pen Gent net 200", pg)
	}
	if pg.InsufficientData || pg.SilverPerHour == 0 {
		t.Fatalf("2m chain must have a rate: %+v", pg)
	}
	if stats[1].Zone != "Unknown zone" || !stats[1].InsufficientData {
		t.Fatalf("single unlabeled event → Unknown zone + insufficient, got %+v", stats[1])
	}
}

func TestZoneStatsWindows(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.Local).UnixMilli()
	s, db := newZoneSvc(t, now)
	day := int64(24 * 3_600_000)

	seed(t, db, "sesNow", []model.FlowEvent{
		{ID: "t1", Kind: model.FlowSilver, TS: now - 3_600_000, Silver: 10, Count: 1, Zone: "Today", Valued: true},   // today 11:00
		{ID: "t2", Kind: model.FlowSilver, TS: now - 3_500_000, Silver: 10, Count: 1, Zone: "Today", Valued: true},   // chain mate
		{ID: "y1", Kind: model.FlowSilver, TS: now - 2*day, Silver: 20, Count: 1, Zone: "TwoDaysAgo", Valued: true},  // in 7d, not today
		{ID: "o1", Kind: model.FlowSilver, TS: now - 30*day, Silver: 30, Count: 1, Zone: "LastMonth", Valued: true},  // only in all
	})

	if got := len(s.ZoneStats("today")); got != 1 {
		t.Fatalf("today → %d zones, want 1", got)
	}
	if got := len(s.ZoneStats("7d")); got != 2 {
		t.Fatalf("7d → %d zones, want 2", got)
	}
	if got := len(s.ZoneStats("all")); got != 3 {
		t.Fatalf("all → %d zones, want 3", got)
	}
	// Unknown window falls back to session (= all four rows are sesNow → 3 zones).
	if got := len(s.ZoneStats("bogus")); got != 3 {
		t.Fatalf("bogus window → %d zones, want session fallback (3)", got)
	}
}

// TestZoneStatsPerformance seeds ~10k events and asserts the full read+compute path
// stays well under the 1s budget (SC-003). Bound is generous to avoid CI flake.
func TestZoneStatsPerformance(t *testing.T) {
	now := int64(100_000_000)
	s, db := newZoneSvc(t, now)
	zones := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
	batch := make([]model.FlowEvent, 0, 500)
	n := 0
	for i := 0; i < 10_000; i++ {
		batch = append(batch, model.FlowEvent{
			ID:   fmt.Sprintf("p%d", i),
			Kind: model.FlowSilver, TS: now - int64(10_000-i)*1000, Silver: 10, Count: 1,
			Zone: zones[i%len(zones)], Valued: true,
		})
		if len(batch) == 500 {
			seed(t, db, "sesNow", batch)
			batch = batch[:0]
			n += 500
		}
	}
	start := time.Now()
	stats := s.ZoneStats("all")
	elapsed := time.Since(start)
	if len(stats) != len(zones) {
		t.Fatalf("zones = %d, want %d", len(stats), len(zones))
	}
	if elapsed > time.Second {
		t.Fatalf("ZoneStats over 10k events took %v, budget 1s (SC-003)", elapsed)
	}
	t.Logf("10k events → %v", elapsed)
}

// TestLootConsistencyAcrossViews (007 SC-004): loot ingested through the service shows
// the SAME total in the session summary, the per-zone activity breakdown, and the
// persisted flow_events rows.
func TestLootConsistencyAcrossViews(t *testing.T) {
	now := int64(50_000_000)
	s, db := newZoneSvc(t, now)
	s.SetZone("Pen Gent")
	s.book.SetEMV(1, 2, 40_000, now) // catalog item 1 (Adept's Bag), quality 2
	s.StartFlowPersistence(context.Background(), db, "sesNow")

	s.IngestLoot("lt:910", 1, 2, 1, now-2*60_000, "corpse")
	s.IngestSilver("sv:x", 500, now-60_000, "")
	s.StopFlowPersistence() // flush to the store

	sum := s.FlowSummary()
	if sum.LootValue != 40_000 {
		t.Fatalf("summary loot = %d, want 40000", sum.LootValue)
	}
	// By-zone activity breakdown carries the same loot total.
	var zoneLoot int64
	for _, z := range s.ZoneStats("session") {
		for _, a := range z.Activities {
			if a.Kind == model.FlowLoot {
				zoneLoot += a.Total
			}
		}
	}
	if zoneLoot != 40_000 {
		t.Fatalf("zone loot activity = %d, want 40000", zoneLoot)
	}
	// Persisted row carries the same value.
	rows, err := db.LoadFlowEvents(context.Background(), "sesNow", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var storedLoot int64
	for _, r := range rows {
		if r.Kind == model.FlowLoot {
			storedLoot += r.Silver
		}
	}
	if storedLoot != 40_000 {
		t.Fatalf("persisted loot = %d, want 40000", storedLoot)
	}
}

func TestZoneStatsNoReader(t *testing.T) {
	c, _ := catalog.New([]byte(cat))
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	s := NewService(c, book, val, &fakeEmitter{}, 100, func() int64 { return 1000 })
	if got := s.ZoneStats("all"); len(got) != 0 {
		t.Fatalf("no reader must return empty, got %d", len(got))
	}
}