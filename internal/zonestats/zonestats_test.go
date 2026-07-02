package zonestats

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

const minMS = 60 * 1000

// ev builds a StoredEvent with the fields the compute cares about.
func ev(session, zone string, kind model.FlowKind, ts, silver, fame int64) StoredEvent {
	return StoredEvent{
		FlowEvent: model.FlowEvent{Kind: kind, TS: ts, Silver: silver, Fame: fame, Count: 1, Zone: zone},
		SessionID: session,
	}
}

func zoneOf(t *testing.T, stats []ZoneStat, name string) ZoneStat {
	t.Helper()
	for _, s := range stats {
		if s.Zone == name {
			return s
		}
	}
	t.Fatalf("zone %q not in result: %+v", name, stats)
	return ZoneStat{}
}

func TestGapCappedActiveTime(t *testing.T) {
	// Events at 0, 2m, 22m in one session: gaps 2m (counted) + 20m (capped at 5m),
	// plus the 30s tail → 2m + 5m + 30s = 450_000 ms.
	stats := Compute([]StoredEvent{
		ev("s1", "Pen Gent", model.FlowSilver, 0, 100, 0),
		ev("s1", "Pen Gent", model.FlowSilver, 2*minMS, 100, 0),
		ev("s1", "Pen Gent", model.FlowSilver, 22*minMS, 100, 0),
	})
	z := zoneOf(t, stats, "Pen Gent")
	want := int64(2*minMS + GapCapMS + TailMS)
	if z.ActiveMS != want {
		t.Fatalf("active = %d, want %d (2m + 5m cap + 30s tail)", z.ActiveMS, want)
	}
	if z.InsufficientData {
		t.Fatal("7.5m active must not be insufficient")
	}
}

func TestSingleEventZoneIsInsufficient(t *testing.T) {
	stats := Compute([]StoredEvent{ev("s1", "Thetford", model.FlowSilver, 1000, 500, 0)})
	z := zoneOf(t, stats, "Thetford")
	if z.ActiveMS != TailMS {
		t.Fatalf("single event active = %d, want tail %d", z.ActiveMS, TailMS)
	}
	if !z.InsufficientData {
		t.Fatal("30s active must be insufficient")
	}
	if z.SilverPerHour != 0 || z.FamePerHour != 0 {
		t.Fatalf("insufficient zone must have zero rates, got %d/%d", z.SilverPerHour, z.FamePerHour)
	}
	if z.NetSilver != 500 {
		t.Fatalf("totals still counted: net = %d, want 500", z.NetSilver)
	}
}

func TestSessionBoundaryBreaksChain(t *testing.T) {
	// Same zone, two sessions 1h apart: the cross-session hour must NOT count.
	// s1: single event (tail 30s). s2: events 0..3m apart (3m + tail 30s).
	base := int64(1_000_000)
	stats := Compute([]StoredEvent{
		ev("s1", "Mists", model.FlowSilver, base, 100, 0),
		ev("s2", "Mists", model.FlowSilver, base+60*minMS, 100, 0),
		ev("s2", "Mists", model.FlowSilver, base+63*minMS, 100, 0),
	})
	z := zoneOf(t, stats, "Mists")
	want := int64(TailMS + 3*minMS + TailMS) // s1 tail + s2 chain
	if z.ActiveMS != want {
		t.Fatalf("active = %d, want %d (cross-session gap excluded)", z.ActiveMS, want)
	}
}

func TestRates(t *testing.T) {
	// 300 silver over exactly 6m (5.5m gaps capped? no — 5.5m > 5m cap) — use clean case:
	// events at 0 and 5m30s? gap 5.5m > cap → counted 5m; +tail 30s = 5.5m... keep simpler:
	// events at 0, 3m, 6m → gaps 3m+3m + tail 30s = 390000ms active; net 300.
	stats := Compute([]StoredEvent{
		ev("s1", "Z", model.FlowSilver, 0, 100, 0),
		ev("s1", "Z", model.FlowSilver, 3*minMS, 100, 0),
		ev("s1", "Z", model.FlowGather, 6*minMS, 100, 0),
	})
	z := zoneOf(t, stats, "Z")
	active := int64(6*minMS + TailMS)
	if z.ActiveMS != active {
		t.Fatalf("active = %d, want %d", z.ActiveMS, active)
	}
	wantSPH := int64(300) * 3_600_000 / active
	if z.SilverPerHour != wantSPH {
		t.Fatalf("silver/h = %d, want %d", z.SilverPerHour, wantSPH)
	}
	wantGPH := int64(100) * 3_600_000 / active
	if z.GatherPerHour != wantGPH {
		t.Fatalf("gather/h = %d, want %d", z.GatherPerHour, wantGPH)
	}
}

func TestFameNeverInSilver(t *testing.T) {
	stats := Compute([]StoredEvent{
		ev("s1", "Z", model.FlowSilver, 0, 100, 0),
		ev("s1", "Z", model.FlowFame, 2*minMS, 0, 5000),
	})
	z := zoneOf(t, stats, "Z")
	if z.NetSilver != 100 {
		t.Fatalf("fame leaked into silver: net = %d, want 100", z.NetSilver)
	}
	if z.Fame != 5000 {
		t.Fatalf("fame = %d, want 5000", z.Fame)
	}
}

func TestUnknownZoneGrouped(t *testing.T) {
	// zone "" must appear as its own group, never dropped.
	stats := Compute([]StoredEvent{
		ev("s1", "", model.FlowSilver, 0, 100, 0),
		ev("s1", "Z", model.FlowSilver, 0, 100, 0),
	})
	if len(stats) != 2 {
		t.Fatalf("want 2 groups (incl. unlabeled), got %d", len(stats))
	}
	zoneOf(t, stats, "") // must exist
}

func TestSortBySilverPerHourDesc(t *testing.T) {
	// Rich zone: 1000 silver / ~4.5m; poor zone: 100 silver / same shape.
	mk := func(zone string, silver int64) []StoredEvent {
		return []StoredEvent{
			ev("s1", zone, model.FlowSilver, 0, silver/2, 0),
			ev("s1", zone, model.FlowSilver, 4*minMS, silver/2, 0),
		}
	}
	stats := Compute(append(mk("Poor", 100), mk("Rich", 1000)...))
	if stats[0].Zone != "Rich" || stats[1].Zone != "Poor" {
		t.Fatalf("sort wrong: %s, %s", stats[0].Zone, stats[1].Zone)
	}
}

func TestActivityBreakdownConsistency(t *testing.T) {
	stats := Compute([]StoredEvent{
		ev("s1", "Z", model.FlowSilver, 0, 200, 0),
		ev("s1", "Z", model.FlowGather, 1*minMS, 300, 0),
		ev("s1", "Z", model.FlowFame, 2*minMS, 0, 900),
	})
	z := zoneOf(t, stats, "Z")
	var silverSum int64
	var fameRow *ActivityStat
	for i, a := range z.Activities {
		if a.Kind == model.FlowFame {
			fameRow = &z.Activities[i]
			continue
		}
		silverSum += a.Total
	}
	if silverSum != z.NetSilver {
		t.Fatalf("Σ activities(kind≠fame) = %d, want NetSilver %d (contract rule 4)", silverSum, z.NetSilver)
	}
	if fameRow == nil || fameRow.Total != 900 {
		t.Fatalf("fame activity row missing/wrong: %+v", z.Activities)
	}
	// Shared denominator: each activity's PerHour = Total scaled by the SAME activeMS.
	for _, a := range z.Activities {
		want := a.Total * 3_600_000 / z.ActiveMS
		if a.PerHour != want {
			t.Fatalf("activity %s perHour = %d, want %d (zone denominator)", a.Kind, a.PerHour, want)
		}
	}
}
