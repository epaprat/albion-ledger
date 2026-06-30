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
