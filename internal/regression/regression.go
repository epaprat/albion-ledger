// Package regression is the pure aggregate snapshot + baseline diff for the
// release regression tripwire (feature 023, US3). Replaying a fixed golden capture
// yields a small struct of the headline numbers every release is judged on; a
// fresh snapshot is diffed against a committed baseline so a change that silently
// shifts a number is caught. The baseline is only ever (re)written by an explicit
// Establish — never on a mismatch (FR-009).
package regression

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// Source is the narrow read view the snapshot needs — the headline aggregate
// getters. The wails Service satisfies it, so regression depends on the domain
// model only, never the adapter (Principle I).
type Source interface {
	HoldingsSummary() model.HoldingsSummary
	FlowSummary() model.SessionSummary
	TradeSummary(window string) model.TradeSummary
	Spec() model.CharacterSpec
}

// Snapshot reads the headline aggregates from a replayed source. Trade net uses
// the all-time window so the number is deterministic for a fixed capture.
func Snapshot(s Source) AggregateSnapshot {
	h := s.HoldingsSummary()
	f := s.FlowSummary()
	return AggregateSnapshot{
		NetWorthSilver:       h.NetWorth,
		HoldingsTotalValue:   h.TotalValue,
		HoldingsGameEstTotal: h.GameEstTotal,
		SilverValue:          f.SilverValue,
		LootValue:            f.LootValue,
		GatherValue:          f.GatherValue,
		Fame:                 f.Fame,
		TradeNet:             s.TradeSummary("all").Net,
		SpecNodeCount:        s.Spec().NodeCount,
	}
}

// AggregateSnapshot is the fixed set of headline numbers (data-model.md). Field
// order is the canonical JSON order; adding fields is a deliberate later change.
type AggregateSnapshot struct {
	NetWorthSilver       int64 `json:"netWorthSilver"`
	HoldingsTotalValue   int64 `json:"holdingsTotalValue"`
	HoldingsGameEstTotal int64 `json:"holdingsGameEstTotal"`
	SilverValue          int64 `json:"silverValue"`
	LootValue            int64 `json:"lootValue"`
	GatherValue          int64 `json:"gatherValue"`
	Fame                 int64 `json:"fame"`
	TradeNet             int64 `json:"tradeNet"`
	SpecNodeCount        int   `json:"specNodeCount"`
}

// AggregateDiff is one field that moved between baseline and current.
type AggregateDiff struct {
	Field    string
	Baseline int64
	Current  int64
}

// RegressionResult is the outcome of a diff (data-model.md).
type RegressionResult struct {
	Baseline  AggregateSnapshot
	Current   AggregateSnapshot
	Diffs     []AggregateDiff
	Regressed bool
}

// fields returns the (name,value) pairs in canonical order for diffing.
func (s AggregateSnapshot) fields() []struct {
	name string
	val  int64
} {
	return []struct {
		name string
		val  int64
	}{
		{"NetWorthSilver", s.NetWorthSilver},
		{"HoldingsTotalValue", s.HoldingsTotalValue},
		{"HoldingsGameEstTotal", s.HoldingsGameEstTotal},
		{"SilverValue", s.SilverValue},
		{"LootValue", s.LootValue},
		{"GatherValue", s.GatherValue},
		{"Fame", s.Fame},
		{"TradeNet", s.TradeNet},
		{"SpecNodeCount", int64(s.SpecNodeCount)},
	}
}

// Diff compares current against baseline, listing every moved field.
func Diff(baseline, current AggregateSnapshot) RegressionResult {
	r := RegressionResult{Baseline: baseline, Current: current}
	bf, cf := baseline.fields(), current.fields()
	for i := range bf {
		if bf[i].val != cf[i].val {
			r.Diffs = append(r.Diffs, AggregateDiff{Field: bf[i].name, Baseline: bf[i].val, Current: cf[i].val})
		}
	}
	r.Regressed = len(r.Diffs) > 0
	return r
}

// LoadBaseline reads a baseline snapshot. Returns found=false (no error) when the
// file does not exist, so the caller can establish it on first run.
func LoadBaseline(path string) (snap AggregateSnapshot, found bool, err error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return AggregateSnapshot{}, false, nil
	}
	if err != nil {
		return AggregateSnapshot{}, false, err
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return AggregateSnapshot{}, false, fmt.Errorf("parse baseline %s: %w", path, err)
	}
	return snap, true, nil
}

// Establish writes snap as the baseline at path (canonical, indented JSON),
// creating the parent directory if needed. This is the ONLY function that writes
// the baseline; a mismatch never calls it.
func Establish(path string, snap AggregateSnapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, b, 0o644)
}
