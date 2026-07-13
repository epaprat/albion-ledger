package flow

import (
	"strconv"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

const hourMS = 3_600_000

func newLedger() (*Ledger, *valuation.Book) {
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	return New(val, DefaultIdleMS, 100), book
}

func item(index, quality int) model.Item { return model.Item{Index: index, Quality: quality} }

func TestSilverAccumulatesAndRate(t *testing.T) {
	l, _ := newLedger()
	// Two silver earnings 30 min apart → net 300, elapsed 30 min → 600/hr.
	l.IngestSilver("s1", 100, 1000, "mob")
	l.IngestSilver("s2", 200, 1000+30*60*1000, "mob")
	now := int64(1000 + 30*60*1000)
	s := l.Summary(now)
	if s.NetSilver != 300 {
		t.Fatalf("net silver = %d, want 300", s.NetSilver)
	}
	if !s.RateReady {
		t.Fatal("rate should be ready after 30 min")
	}
	if s.SilverPerHour != 600 {
		t.Fatalf("silver/hr = %d, want 600", s.SilverPerHour)
	}
	if s.Fame != 0 {
		t.Fatalf("fame should be 0, got %d", s.Fame)
	}
}

func TestRateNotReadyUnderOneMinute(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 500, 1000, "mob")
	s := l.Summary(1000 + 30*1000) // 30s elapsed
	if s.RateReady {
		t.Fatal("rate must not be ready under 60s")
	}
	if s.SilverPerHour != 0 {
		t.Fatalf("silver/hr must be 0 when not ready, got %d", s.SilverPerHour)
	}
}

func TestDedup(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("dup", 100, 1000, "mob")
	l.IngestSilver("dup", 100, 1000, "mob") // retry/echo
	s := l.Summary(1000 + 2*60*1000)
	if s.NetSilver != 100 {
		t.Fatalf("dedup failed: net = %d, want 100", s.NetSilver)
	}
	if s.EventCount != 1 {
		t.Fatalf("dedup failed: eventCount = %d, want 1", s.EventCount)
	}
}

func TestRingCapEviction(t *testing.T) {
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	l := New(val, DefaultIdleMS, 10) // cap 10
	for i := 0; i < 25; i++ {
		l.IngestSilver("e"+strconv.Itoa(i), 1, 1000+int64(i), "mob")
	}
	if n := len(l.List()); n != 10 {
		t.Fatalf("list length = %d, want 10 (cap)", n)
	}
	s := l.Summary(1000 + 2*60*1000)
	if s.EventCount != 25 {
		t.Fatalf("eventCount = %d, want 25 (cumulative survives eviction)", s.EventCount)
	}
	if s.NetSilver != 25 {
		t.Fatalf("net = %d, want 25 (totals survive eviction)", s.NetSilver)
	}
}

func TestIdleAutoClose(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 100, 1000, "mob")
	// 40 min later, no new earning → session idle-closed.
	now := int64(1000 + 40*60*1000)
	s := l.Summary(now)
	if s.Active {
		t.Fatal("session should be idle-closed after 40 min")
	}
	// Elapsed frozen at last activity (idle tail not counted) → ~0 here (single event).
	if s.ElapsedMS != 0 {
		t.Fatalf("elapsed = %d, want 0 (single event, idle tail excluded)", s.ElapsedMS)
	}
}

func TestNewSessionAfterIdleResets(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 1000, 1000, "mob")
	// Earning 40 min later → new session, totals reset.
	l.IngestSilver("s2", 50, 1000+40*60*1000, "mob")
	s := l.Summary(1000 + 40*60*1000)
	if s.NetSilver != 50 {
		t.Fatalf("new session net = %d, want 50 (reset)", s.NetSilver)
	}
	if s.EventCount != 1 {
		t.Fatalf("new session eventCount = %d, want 1", s.EventCount)
	}
}

func TestLootValuedAndUnvalued(t *testing.T) {
	l, book := newLedger()
	book.SetEMV(920, 0, 1000, 500) // item 920 worth 1000
	l.IngestLoot("l1", item(920, 0), 3, 1000, "chest")
	l.IngestLoot("l2", item(999, 0), 1, 1000, "chest") // no value
	s := l.Summary(1000 + 2*60*1000)
	if s.LootValue != 3000 {
		t.Fatalf("loot value = %d, want 3000 (1000×3)", s.LootValue)
	}
	if s.NetSilver != 3000 {
		t.Fatalf("net silver = %d, want 3000", s.NetSilver)
	}
	if s.UnvaluedCount != 1 {
		t.Fatalf("unvalued = %d, want 1", s.UnvaluedCount)
	}
}

func TestRevalueDeferred(t *testing.T) {
	l, book := newLedger()
	l.IngestLoot("l1", item(920, 0), 2, 1000, "chest") // unvalued at capture
	if s := l.Summary(1000 + 2*60*1000); s.NetSilver != 0 || s.UnvaluedCount != 1 {
		t.Fatalf("before revalue: net=%d unvalued=%d, want 0/1", s.NetSilver, s.UnvaluedCount)
	}
	book.SetEMV(920, 0, 500, 1500) // value arrives later
	l.RevalueItem(920, 0)
	s := l.Summary(1000 + 2*60*1000)
	if s.NetSilver != 1000 { // 500×2
		t.Fatalf("after revalue: net = %d, want 1000", s.NetSilver)
	}
	if s.LootValue != 1000 {
		t.Fatalf("after revalue: loot = %d, want 1000", s.LootValue)
	}
	if s.UnvaluedCount != 0 {
		t.Fatalf("after revalue: unvalued = %d, want 0", s.UnvaluedCount)
	}
	if n := len(l.List()); n != 1 {
		t.Fatalf("revalue must not duplicate rows: %d, want 1", n)
	}
}

// TestSoakBounded drives a large synthetic flow load and asserts the in-memory
// structures stay bounded (Principle XI / SC-004): the event ring never exceeds the
// cap and the dedup set stays within its 4×cap bound.
func TestSoakBounded(t *testing.T) {
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	const capN = 1000
	l := New(val, DefaultIdleMS, capN)
	base := int64(1000)
	for i := 0; i < 200_000; i++ {
		// keep within one session (small time steps, never exceeding idle window)
		l.IngestSilver("e"+strconv.Itoa(i), 1, base+int64(i), "mob")
	}
	if n := len(l.events); n > capN {
		t.Fatalf("event ring = %d, want ≤ %d", n, capN)
	}
	if n := len(l.order); n > capN {
		t.Fatalf("order ring = %d, want ≤ %d", n, capN)
	}
	if n := l.dedup.Len(); n > capN*4 {
		t.Fatalf("dedup set = %d, want ≤ %d", n, capN*4)
	}
	if n := len(l.List()); n != capN {
		t.Fatalf("list = %d, want %d", n, capN)
	}
}

func TestGatherValue(t *testing.T) {
	l, book := newLedger()
	book.SetEMV(700, 0, 250, 500) // resource worth 250
	l.IngestGather("g1", item(700, 0), 4, 1000, "node")
	s := l.Summary(1000 + 2*60*1000)
	if s.GatherValue != 1000 { // 250×4
		t.Fatalf("gather value = %d, want 1000", s.GatherValue)
	}
	if s.NetSilver != 1000 {
		t.Fatalf("net silver = %d, want 1000", s.NetSilver)
	}
	if s.LootValue != 0 {
		t.Fatalf("loot value must stay 0, got %d", s.LootValue)
	}
}

func TestBreakdownPerItem(t *testing.T) {
	l, book := newLedger()
	book.SetEMV(920, 0, 100, 500) // wood worth 100/ea
	book.SetEMV(837, 0, 50, 500)  // other worth 50/ea
	l.IngestGather("g1", item(920, 0), 3, 1000, "node")
	l.IngestGather("g2", item(920, 0), 2, 1100, "node") // same item again → qty 5
	l.IngestGather("g3", item(837, 0), 4, 1200, "node")
	l.IngestSilver("s1", 999, 1300, "mob") // silver must not appear in gather breakdown

	b := l.Breakdown(model.FlowGather, 1300+2*60*1000)
	if len(b) != 2 {
		t.Fatalf("gather breakdown rows = %d, want 2", len(b))
	}
	// Sorted by total value desc → 920 (5×100=500) before 837 (4×50=200).
	if b[0].Index != 920 || b[0].Qty != 5 || b[0].UnitValue != 100 || b[0].TotalValue != 500 {
		t.Fatalf("row0 = %+v, want idx920 qty5 unit100 total500", b[0])
	}
	if b[1].Index != 837 || b[1].Qty != 4 || b[1].TotalValue != 200 {
		t.Fatalf("row1 = %+v, want idx837 qty4 total200", b[1])
	}
	if n := len(l.Breakdown(model.FlowSilver, 1300)); n != 0 {
		t.Fatalf("silver breakdown must be empty, got %d", n)
	}
}

// TestLootQualityKeyedValuation guards the ADR-022 quality-0 fix for loot (007):
// equipment EMV is booked under its wire quality (1-5); a loot event carrying that
// same quality must value immediately, and a deferred value at that quality must
// back-fill via RevalueItem — neither works if loot hardcodes quality 0.
func TestLootQualityKeyedValuation(t *testing.T) {
	l, book := newLedger()
	book.SetEMV(6977, 2, 50_000, 500) // quality-2 gear worth 50k
	l.IngestLoot("lt:910", item(6977, 2), 1, 1000, "corpse")
	s := l.Summary(1000 + 2*60*1000)
	if s.LootValue != 50_000 || s.UnvaluedCount != 0 {
		t.Fatalf("quality-2 loot must value: loot=%d unvalued=%d", s.LootValue, s.UnvaluedCount)
	}

	// Deferred: quality-3 loot unvalued at capture, valued when the quality-3 EMV lands.
	l.IngestLoot("lt:911", item(6977, 3), 1, 1100, "corpse")
	if s := l.Summary(1100 + 60*1000); s.UnvaluedCount != 1 {
		t.Fatalf("quality-3 loot should be unvalued yet: %+v", s)
	}
	book.SetEMV(6977, 3, 80_000, 1200)
	if updated := l.RevalueItem(6977, 3); len(updated) != 1 {
		t.Fatalf("RevalueItem(q3) must back-fill 1 event, got %d", len(updated))
	}
	s = l.Summary(1200 + 60*1000)
	if s.LootValue != 130_000 || s.UnvaluedCount != 0 {
		t.Fatalf("after back-fill: loot=%d unvalued=%d, want 130000/0", s.LootValue, s.UnvaluedCount)
	}
}

func TestFameSeparateFromSilver(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 100, 1000, "mob")
	l.IngestFame("f1", 5000, 1000+5*60*1000) // 5 min later, same session
	now := int64(1000 + 5*60*1000)           // elapsed 5 min
	s := l.Summary(now)
	if s.Fame != 5000 {
		t.Fatalf("fame = %d, want 5000", s.Fame)
	}
	if s.NetSilver != 100 {
		t.Fatalf("fame must not touch silver: net = %d, want 100", s.NetSilver)
	}
	if s.FamePerHour != 60000 { // 5000 over 5 min → 60000/hr
		t.Fatalf("fame/hr = %d, want 60000", s.FamePerHour)
	}
}

// 020 US4 / C5 — a live session checkpoints, resumes within the idle window (totals + time
// continue), does NOT resume once expired, and promotes to a completed summary.
func TestFlowCheckpointResume(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 100, 1000, "mob")
	l.IngestSilver("s2", 200, 2000, "mob")
	cp, ok := l.Checkpoint()
	if !ok || cp.NetSilver != 300 || cp.EventCount != 2 || cp.StartedMS != 1000 {
		t.Fatalf("checkpoint wrong: %+v ok=%v", cp, ok)
	}

	// Resume into a fresh ledger within the idle window → the totals continue.
	l2, _ := newLedger()
	if !l2.RestoreCheckpoint(cp, 2000+60_000) {
		t.Fatal("must resume a checkpoint within the idle window")
	}
	if s := l2.Summary(2000 + 60_000); s.NetSilver != 300 {
		t.Fatalf("resumed net = %d, want 300", s.NetSilver)
	}
	// A new earning stacks on the resumed totals.
	l2.IngestSilver("s3", 50, 2000+120_000, "mob")
	if s := l2.Summary(2000 + 120_000); s.NetSilver != 350 {
		t.Fatalf("post-resume net = %d, want 350", s.NetSilver)
	}

	// An expired checkpoint (past the idle window) must NOT resume.
	l3, _ := newLedger()
	if l3.RestoreCheckpoint(cp, 2000+DefaultIdleMS+1) {
		t.Fatal("expired checkpoint must not resume")
	}

	// Promoting the checkpoint to history preserves its totals.
	cs := CompletedFromCheckpoint(cp)
	if cs.NetSilver != 300 || cs.StartedMS != 1000 || cs.EndedMS != 2000 {
		t.Fatalf("completed-from-checkpoint wrong: %+v", cs)
	}
}

// An idle ledger has no session to checkpoint.
func TestFlowCheckpointIdle(t *testing.T) {
	l, _ := newLedger()
	if _, ok := l.Checkpoint(); ok {
		t.Fatal("an idle ledger must not produce a checkpoint")
	}
}

// 020 US4 — when a session idle-closes and a new one starts, the closed session is queued
// for the permanent history (AFM completed-session model).
func TestFlowMidSessionPromotion(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 100, 1000, "mob") // session A
	l.IngestSilver("s2", 50, 2000, "mob")  // still A → net 150
	// An earning past the idle window starts session B and promotes A.
	l.IngestSilver("s3", 200, 2000+DefaultIdleMS+1, "mob")

	done := l.DrainCompleted()
	if len(done) != 1 || done[0].NetSilver != 150 || done[0].StartedMS != 1000 {
		t.Fatalf("closed session A must be promoted (net 150), got %+v", done)
	}
	if s := l.Summary(2000 + DefaultIdleMS + 1); s.NetSilver != 200 {
		t.Fatalf("live session B net = %d, want 200", s.NetSilver)
	}
	if d := l.DrainCompleted(); len(d) != 0 {
		t.Fatalf("second drain must be empty, got %d", len(d))
	}
}

// ── 022 earnings dashboard: EMA "now" rate + per-stream figures ──────────────

// T004: a steady earning pace makes the "now" rate converge toward the true rate (≈ the
// session average), and the per-stream figures separate correctly.
func TestNowRateConvergesToSteadyPace(t *testing.T) {
	l, _ := newLedger()
	// 100 silver every 1s for 10 minutes → true rate 100/1000ms = 360k/h.
	var ts int64 = 1000
	for i := 0; i < 600; i++ {
		l.IngestSilver("s"+strconv.Itoa(i), 100, ts, "mob")
		ts += 1000
	}
	s := l.Summary(ts)
	const want = 360_000
	if s.SilverNowPerHour < want*70/100 || s.SilverNowPerHour > want*130/100 {
		t.Fatalf("now must converge near the steady rate %d, got %d", want, s.SilverNowPerHour)
	}
	if s.SilverAvgPerHour < want*80/100 || s.SilverAvgPerHour > want*120/100 {
		t.Fatalf("avg must be near the steady rate %d, got %d", want, s.SilverAvgPerHour)
	}
}

// T004: a single large pickup lifts "now" well above the average, then decays back toward it
// without ever presenting the one-off as the sustained figure.
func TestNowRateSpikeDecays(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("base", 1000, 1000, "mob")          // small baseline
	before := l.Summary(120_000).SilverNowPerHour      // just before the spike
	l.IngestSilver("spike", 5_000_000, 120_000, "mob") // huge one-off at t=2min
	atSpike := l.Summary(121_000).SilverNowPerHour     // right after the spike
	if atSpike <= before {
		t.Fatalf("now must rise on a spike: before=%d at-spike=%d", before, atSpike)
	}
	// A 5M one-off must NOT read as a multi-hundred-million/h sustained rate: the EMA caps its
	// instantaneous contribution near value/tau (~42M/h), far below the naive avg that would
	// extrapolate 5M-over-2min to ~150M/h (FR-005 — the whole point of smoothing).
	if atSpike > 60_000_000 {
		t.Fatalf("a lone spike must not present as an implausible sustained rate, got %d", atSpike)
	}
	later := l.Summary(121_000 + 15*60_000).SilverNowPerHour // 15 min later, no new earnings
	if later >= atSpike {
		t.Fatalf("now must decay after the spike: at-spike=%d, 15min-later=%d", atSpike, later)
	}
}

// T004: with no recent earnings the "now" rate decays toward zero (idle), while totals hold.
func TestNowRateIdleDecaysToZero(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s1", 500_000, 1000, "mob")
	l.IngestSilver("s2", 500_000, 61_000, "mob") // ~1min so rateReady
	base := l.Summary(61_000).SilverNowPerHour
	idle := l.Summary(61_000 + 60*60_000).SilverNowPerHour // 1 hour idle
	if base <= 0 {
		t.Fatalf("precondition: now must be positive while earning, got %d", base)
	}
	if idle > base/100 {
		t.Fatalf("now must decay toward zero after long idle: base=%d idle=%d", base, idle)
	}
	if l.Summary(61_000+60*60_000).NetSilver != 1_000_000 {
		t.Fatal("total must hold through idle")
	}
}

// T005: per-stream totals & averages separate correctly and fame never enters silver.
func TestPerStreamSeparation(t *testing.T) {
	l, book := newLedger()
	book.SetEMV(920, 0, 10, 500)                                 // loot item worth 10
	book.SetEMV(837, 0, 20, 500)                                 // gather item worth 20
	l.IngestSilver("s", 1000, 1000, "mob")                       // silver-only 1000
	l.IngestLoot("l", model.Item{Index: 920}, 3, 1000, "corpse") // loot 30
	l.IngestGather("g", model.Item{Index: 837}, 2, 1000, "node") // gather 40
	l.IngestFame("f", 5000, 1000)                                // fame 5000
	// Read at exactly 60s active elapsed (rateReady, still within the idle window): avg /h =
	// total × 3_600_000 / 60_000 = total × 60.
	s := l.Summary(1000 + 60_000)

	if s.SilverValue != 1000 {
		t.Fatalf("silver-only total = %d, want 1000 (never loot/gather)", s.SilverValue)
	}
	if s.LootValue != 30 || s.GatherValue != 40 {
		t.Fatalf("loot/gather totals wrong: loot=%d gather=%d", s.LootValue, s.GatherValue)
	}
	if s.NetSilver != 1070 {
		t.Fatalf("combined = %d, want 1070 (silver+loot+gather, no fame)", s.NetSilver)
	}
	if s.Fame != 5000 {
		t.Fatalf("fame = %d, want 5000", s.Fame)
	}
	if s.SilverAvgPerHour != 60000 || s.LootAvgPerHour != 1800 || s.GatherAvgPerHour != 2400 {
		t.Fatalf("per-stream avg wrong (×60): s=%d l=%d g=%d", s.SilverAvgPerHour, s.LootAvgPerHour, s.GatherAvgPerHour)
	}
}

// T004: below the measuring gate (<60s active) all rates read 0; totals still show.
func TestRatesMeasuringGate(t *testing.T) {
	l, _ := newLedger()
	l.IngestSilver("s", 5000, 1000, "mob")
	s := l.Summary(1000 + 30_000) // 30s
	if s.RateReady {
		t.Fatal("rate must not be ready before 60s")
	}
	if s.SilverNowPerHour != 0 || s.SilverAvgPerHour != 0 || s.NowPerHour != 0 {
		t.Fatalf("all rates must be 0 while measuring, got now=%d avg=%d combined=%d", s.SilverNowPerHour, s.SilverAvgPerHour, s.NowPerHour)
	}
	if s.SilverValue != 5000 {
		t.Fatalf("total must show even while measuring, got %d", s.SilverValue)
	}
}
