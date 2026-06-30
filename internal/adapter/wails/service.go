// Package wailsadapter is the Go side of the UI boundary (Principle VII): it
// turns captured data into resolved, valued view rows and exposes them to the
// frontend via bindings + events. Only DTOs cross the boundary.
package wailsadapter

import (
	"sync"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/port"
	"github.com/epaprat/albion-ledger/internal/valuation"
)

// Emitter abstracts the Wails event runtime so the service is testable without it.
type Emitter interface {
	Emit(event string, data ...interface{})
}

// Events emitted to the frontend.
const (
	EventItemUpdated   = "item:updated"
	EventStatusChanged = "status:changed"
	EventDriftAlert    = "drift:alert"
)

// Service holds the bounded live-view state and the bound methods.
type Service struct {
	cat   port.Catalog
	book  *valuation.Book
	val   port.Valuer
	emit  Emitter
	cap   int
	nowMS func() int64

	mu     sync.Mutex
	items  map[int]*model.LiveViewItem // by item index
	order  []int                       // insertion order for FIFO cap eviction
	status model.CaptureStatusView
}

// NewService creates the view service. cap bounds the live list (Principle XI).
func NewService(cat port.Catalog, book *valuation.Book, val port.Valuer, emit Emitter, cap int, nowMS func() int64) *Service {
	if cap <= 0 {
		cap = 5000
	}
	return &Service{cat: cat, book: book, val: val, emit: emit, cap: cap, nowMS: nowMS, items: map[int]*model.LiveViewItem{}}
}

// IngestEMV records an item's estimated value and refreshes its view row.
func (s *Service) IngestEMV(index, quality int, value, asOf int64) {
	s.book.SetEMV(index, quality, value, asOf)
	s.upsert(index, quality, asOf)
}

// IngestMarket records a live market price and refreshes its view row.
func (s *Service) IngestMarket(index, quality int, price, asOf int64) {
	s.book.SetMarket(index, quality, price, asOf)
	s.upsert(index, quality, asOf)
}

func (s *Service) upsert(index, quality int, asOf int64) {
	now := s.nowMS()
	item := s.cat.Resolve(index, quality)
	val := s.val.Value(index, quality, now)

	s.mu.Lock()
	row, ok := s.items[index]
	if !ok {
		row = &model.LiveViewItem{}
		s.items[index] = row
		s.order = append(s.order, index)
		s.evict()
	}
	row.Item = item
	row.Valuation = val
	row.LastSeen = asOf
	row.Count++
	snapshot := *row
	s.mu.Unlock()

	if s.emit != nil {
		s.emit.Emit(EventItemUpdated, snapshot)
	}
}

func (s *Service) evict() {
	for len(s.items) > s.cap && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.items, oldest)
	}
}

// SetStatus updates and broadcasts the capture status (FR-006 / drift FR-012).
func (s *Service) SetStatus(st model.CaptureStatusView) {
	s.mu.Lock()
	s.status = st
	s.mu.Unlock()
	if s.emit != nil {
		s.emit.Emit(EventStatusChanged, st)
		if st.DriftAlert != "" {
			s.emit.Emit(EventDriftAlert, st.DriftAlert)
		}
	}
}

// ── Bound methods (called from the frontend) ─────────────────────────────────

// ListItems returns the current live view rows (most recently seen first).
func (s *Service) ListItems() []model.LiveViewItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.LiveViewItem, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if row, ok := s.items[s.order[i]]; ok {
			out = append(out, *row)
		}
	}
	return out
}

// Status returns the current capture status.
func (s *Service) Status() model.CaptureStatusView {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}
