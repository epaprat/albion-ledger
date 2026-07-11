package wailsadapter

import (
	"sync"
	"testing"

	"github.com/epaprat/albion-ledger/internal/catalog"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

// fakeEmitter is thread-safe like the real Wails runtime emitter — flow refreshes can be
// broadcast from both the pipeline and the status-ticker goroutine.
type fakeEmitter struct {
	mu     sync.Mutex
	events []string
}

func (f *fakeEmitter) Emit(event string, _ ...interface{}) {
	f.mu.Lock()
	f.events = append(f.events, event)
	f.mu.Unlock()
}

const cat = `{"items":[{"index":1,"uniqueName":"T4_BAG","name":"Adept's Bag"}]}`

func newSvc(t *testing.T) (*Service, *fakeEmitter) {
	t.Helper()
	c, err := catalog.New([]byte(cat))
	if err != nil {
		t.Fatal(err)
	}
	book := valuation.NewBook()
	val := valuation.NewValuer(book, model.DefaultStaleAfterMS)
	em := &fakeEmitter{}
	return NewService(c, book, val, em, 100, func() int64 { return 1000 }), em
}

func TestIngestProducesResolvedValuedRow(t *testing.T) {
	s, em := newSvc(t)
	s.IngestEMV(1, 2, 500, 1000)

	items := s.ListItems()
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	row := items[0]
	if row.Item.DisplayName != "Adept's Bag" || !row.Item.Known {
		t.Fatalf("name not resolved: %+v", row.Item)
	}
	if row.Valuation.Source != model.SourceServerEstimate || row.Valuation.Amount != 500 {
		t.Fatalf("valuation wrong: %+v", row.Valuation)
	}
	if len(em.events) == 0 || em.events[0] != EventItemUpdated {
		t.Fatalf("expected item:updated event, got %v", em.events)
	}
}

func TestUnknownItemFallback(t *testing.T) {
	s, _ := newSvc(t)
	s.IngestEMV(999, 0, 100, 1000)
	row := s.ListItems()[0]
	if row.Item.Known || row.Item.DisplayName != "Unknown item #999" {
		t.Fatalf("unknown fallback failed: %+v", row.Item)
	}
}

func TestMarketPreferredOverEMV(t *testing.T) {
	s, _ := newSvc(t)
	s.IngestEMV(1, 0, 400, 1000)
	s.IngestMarket(1, 0, 600, 1000)
	row := s.ListItems()[0]
	if row.Valuation.Source != model.SourceLiveMarket || row.Valuation.Amount != 600 {
		t.Fatalf("market should win: %+v", row.Valuation)
	}
}

func TestStatusEmits(t *testing.T) {
	s, em := newSvc(t)
	s.SetStatus(model.CaptureStatusView{Capturing: true, DriftAlert: "loot silent"})
	if s.Status().DriftAlert != "loot silent" {
		t.Fatal("status not stored")
	}
	var sawStatus, sawDrift bool
	for _, e := range em.events {
		sawStatus = sawStatus || e == EventStatusChanged
		sawDrift = sawDrift || e == EventDriftAlert
	}
	if !sawStatus || !sawDrift {
		t.Fatalf("expected status + drift events, got %v", em.events)
	}
}
