// Package port defines the hexagonal ports (interfaces) the domain owns and
// adapters implement (Constitution Principle I).
package port

import (
	"context"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// PacketSource yields raw Photon UDP payloads to the parser. Implemented by the
// live capture adapter (gopacket) and the deterministic replay adapter.
type PacketSource interface {
	// Packets returns a channel of raw UDP payloads. The channel closes when ctx
	// is cancelled or the source is exhausted (replay).
	Packets(ctx context.Context) (<-chan []byte, error)
	// Status reports the current capture state (Principle VII observability).
	Status() model.CaptureStatus
}

// Store is the local-first durable persistence port (Principle VIII).
type Store interface {
	StartSession(ctx context.Context, s model.CaptureSession) error
	EndSession(ctx context.Context, id string, endedAt int64, totals model.SessionTotals) error
	AppendObservations(ctx context.Context, batch []model.Observation) error
	UpsertCoverage(ctx context.Context, rows []model.CategoryCoverage) error
	AddReconciliation(ctx context.Context, n model.ReconciliationNote) error
	LoadCoverage(ctx context.Context, sessionID string) ([]model.CategoryCoverage, error)
	LoadReconciliations(ctx context.Context, sessionID string) ([]model.ReconciliationNote, error)
	Close() error
}
