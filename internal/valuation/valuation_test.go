package valuation

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func TestPrefersLiveMarket(t *testing.T) {
	b := NewBook()
	b.SetMarket(1, 2, 500, 1000)
	b.SetEMV(1, 2, 400, 1000)
	v := NewValuer(b, model.DefaultStaleAfterMS)
	got := v.Value(1, 2, 1000)
	if got.Source != model.SourceLiveMarket || got.Amount != 500 {
		t.Fatalf("want live_market 500, got %+v", got)
	}
}

func TestFallsBackToEMV(t *testing.T) {
	b := NewBook()
	b.SetEMV(1, 2, 400, 1000)
	got := NewValuer(b, model.DefaultStaleAfterMS).Value(1, 2, 1000)
	if got.Source != model.SourceServerEstimate || got.Amount != 400 {
		t.Fatalf("want server_estimate 400, got %+v", got)
	}
}

func TestUnknownWhenNoData(t *testing.T) {
	got := NewValuer(NewBook(), model.DefaultStaleAfterMS).Value(9, 0, 1000)
	if got.Source != model.SourceUnknown || got.Amount != 0 {
		t.Fatalf("want unknown 0, got %+v", got)
	}
}

func TestStaleFlag(t *testing.T) {
	b := NewBook()
	b.SetMarket(1, 0, 500, 0)
	now := model.DefaultStaleAfterMS + 1
	got := NewValuer(b, model.DefaultStaleAfterMS).Value(1, 0, now)
	if !got.Stale {
		t.Fatalf("value older than threshold must be stale, got %+v", got)
	}
}

// 010 review: the 3-layer precedence and persistence semantics, pinned.
func TestExternalLayerPrecedence(t *testing.T) {
	b := NewBook()
	v := NewValuer(b, 1000)
	b.SetExternal(920, 1, 50, 100)
	if val := v.Value(920, 1, 200); val.Amount != 50 || val.Source != model.SourceExternal {
		t.Fatalf("external base must price: %+v", val)
	}
	b.SetEMV(920, 1, 70, 150)
	if val := v.Value(920, 1, 200); val.Amount != 70 || val.Source != model.SourceServerEstimate {
		t.Fatalf("EMV must beat external: %+v", val)
	}
	b.SetMarket(920, 1, 90, 180)
	if val := v.Value(920, 1, 200); val.Amount != 90 || val.Source != model.SourceLiveMarket {
		t.Fatalf("live must beat all: %+v", val)
	}
}

func TestQualityZeroFallbackAcrossLayers(t *testing.T) {
	b := NewBook()
	v := NewValuer(b, 1000)
	b.SetExternal(920, 3, 40, 100)
	if val := v.Value(920, 0, 200); val.Amount != 40 || val.Source != model.SourceExternal {
		t.Fatalf("q0 must fall back to external q3: %+v", val)
	}
	b.SetEMV(920, 4, 60, 100) // ANY emv quality beats ANY external in the fallback
	if val := v.Value(920, 0, 200); val.Amount != 60 {
		t.Fatalf("q0 fallback must prefer emv layer: %+v", val)
	}
}

// 020 live-test (2026-07-10): a HIGHER-quality query must also honor "in-game wins".
// The game recorded EMV only at normal quality (q0), while a lone troll AODP listing
// existed at the item's exact quality — a T4 artefact EMV'd at 140s showed as 25M,
// inflating net worth by 75M. A cross-quality game EMV must beat an exact external.
func TestGameEMVBeatsExternalAcrossQuality(t *testing.T) {
	b := NewBook()
	v := NewValuer(b, 1000)
	b.SetExternal(1195, 2, 25_000_142, 100) // troll AODP price at the stack's exact quality
	b.SetEMV(1195, 0, 140, 100)             // the game's own estimate, recorded at q0 only
	val := v.Value(1195, 2, 200)            // stack is quality 2
	if val.Source != model.SourceServerEstimate || val.Amount != 140 {
		t.Fatalf("cross-quality game EMV must beat exact-quality external, got %+v", val)
	}
	// With no game EMV anywhere, the external base layer still applies (unchanged).
	b2 := NewBook()
	v2 := NewValuer(b2, 1000)
	b2.SetExternal(1195, 2, 500, 100)
	if val := v2.Value(1195, 2, 200); val.Source != model.SourceExternal || val.Amount != 500 {
		t.Fatalf("external base must still price when no in-game data exists: %+v", val)
	}
}

func TestRestoreEMVNewestWins(t *testing.T) {
	b := NewBook()
	b.SetEMV(920, 1, 100, 500)
	b.RestoreEMV([]EMVEntry{
		{Index: 920, Quality: 1, Amount: 1, AsOf: 400}, // older — must not clobber
		{Index: 837, Quality: 2, Amount: 9, AsOf: 300}, // new key — restores
	})
	v := NewValuer(b, 1_000_000)
	if val := v.Value(920, 1, 600); val.Amount != 100 {
		t.Fatalf("older restore clobbered fresher EMV: %+v", val)
	}
	if val := v.Value(837, 2, 600); val.Amount != 9 {
		t.Fatalf("restore missed a new key: %+v", val)
	}
	// Round-trip: snapshot carries both entries.
	if n := len(b.SnapshotEMV()); n != 2 {
		t.Fatalf("snapshot entries = %d, want 2", n)
	}
}
