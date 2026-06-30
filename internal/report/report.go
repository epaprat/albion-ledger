// Package report renders the coverage report (the probe's primary deliverable)
// as JSON and human-readable text. This is the Principle VII boundary DTO — no
// domain/pcap types leak through it.
package report

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// SessionDTO is the session summary block.
type SessionDTO struct {
	ID             string  `json:"id"`
	SourceKind     string  `json:"source_kind"`
	Interface      string  `json:"interface"`
	GameServer     string  `json:"game_server,omitempty"`
	StartedAt      int64   `json:"started_at"`
	EndedAt        int64   `json:"ended_at"`
	DecodedCount   uint64  `json:"decoded_count"`
	EncryptedCount uint64  `json:"encrypted_count"`
	EncryptedRate  float64 `json:"encrypted_rate"`
	DroppedCount   uint64  `json:"dropped_count"`
	UnhandledCount uint64  `json:"unhandled_count"`
}

// CategoryDTO is one coverage row.
type CategoryDTO struct {
	Category      string  `json:"category"`
	ObservedCount int     `json:"observed_count"`
	Completeness  float64 `json:"completeness"`
	Verdict       string  `json:"verdict"`
}

// ReconciliationDTO is one ground-truth note.
type ReconciliationDTO struct {
	Category      string `json:"category"`
	Result        string `json:"result"`
	CapturedValue string `json:"captured_value"`
	IngameValue   string `json:"ingame_value"`
	Notes         string `json:"notes"`
}

// CoverageReport is the full report DTO.
type CoverageReport struct {
	Session         SessionDTO          `json:"session"`
	Categories      []CategoryDTO       `json:"categories"`
	Reconciliations []ReconciliationDTO `json:"reconciliations"`
}

// Build assembles a report DTO from domain values.
func Build(s model.CaptureSession, t model.SessionTotals,
	coverage []model.CategoryCoverage, notes []model.ReconciliationNote) CoverageReport {

	r := CoverageReport{
		Session: SessionDTO{
			ID: s.ID, SourceKind: string(s.SourceKind), Interface: s.Interface,
			GameServer: s.GameServer, StartedAt: s.StartedAt, EndedAt: s.EndedAt,
			DecodedCount: t.DecodedCount, EncryptedCount: t.EncryptedCount,
			EncryptedRate: t.EncryptedRate(), DroppedCount: t.DroppedCount,
			UnhandledCount: t.UnhandledCount,
		},
	}
	for _, c := range coverage {
		r.Categories = append(r.Categories, CategoryDTO{
			Category: string(c.Category), ObservedCount: c.ObservedCount,
			Completeness: c.Completeness, Verdict: string(c.Verdict),
		})
	}
	for _, n := range notes {
		r.Reconciliations = append(r.Reconciliations, ReconciliationDTO{
			Category: string(n.Category), Result: n.Result,
			CapturedValue: n.CapturedValue, IngameValue: n.IngameValue, Notes: n.Notes,
		})
	}
	return r
}

// JSON renders the report as indented JSON.
func (r CoverageReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Text renders a human-readable table (maintainer-legible, Principle VII/XII).
func (r CoverageReport) Text() string {
	var b strings.Builder
	s := r.Session
	fmt.Fprintf(&b, "Session %s [%s] iface=%s server=%s\n", s.ID, s.SourceKind, s.Interface, s.GameServer)
	fmt.Fprintf(&b, "  decoded=%d encrypted=%d (%.1f%%) dropped=%d unhandled=%d\n\n",
		s.DecodedCount, s.EncryptedCount, s.EncryptedRate*100, s.DroppedCount, s.UnhandledCount)
	fmt.Fprintf(&b, "  %-22s %8s %12s  %s\n", "CATEGORY", "OBSERVED", "COMPLETE", "VERDICT")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("-", 56))
	for _, c := range r.Categories {
		fmt.Fprintf(&b, "  %-22s %8d %11.0f%%  %s\n",
			c.Category, c.ObservedCount, c.Completeness*100, c.Verdict)
	}
	if len(r.Reconciliations) > 0 {
		fmt.Fprintf(&b, "\n  Reconciliations:\n")
		for _, n := range r.Reconciliations {
			fmt.Fprintf(&b, "  - %s: %s (%s)\n", n.Category, n.Result, n.Notes)
		}
	}
	return b.String()
}
