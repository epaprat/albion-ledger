// Runner orchestrates a probe capture/replay run: source → Photon parse →
// classify → coverage rollup → local store. It depends only on ports + domain.
// (The package doc lives in pipeline.go — the live-app pipeline, feature 009.)
package app

import (
	"context"
	"fmt"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/photon"
	"github.com/epaprat/albion-ledger/internal/port"
)

const observationBatch = 500 // flush to store in batches (Principle XI: bounded RAM)

// Result is the outcome of a run.
type Result struct {
	Totals   model.SessionTotals
	Coverage []model.CategoryCoverage
	// Unhandled counts decoded-but-unclassified messages per (kind,code), so we
	// can discover which real codes fire for an activity and map them.
	Unhandled map[string]int
}

// Runner wires a packet source through parsing, classification, and persistence.
type Runner struct {
	clf *probe.Classifier
	th  probe.Thresholds
	// OnMessage, if set, is called for EVERY decoded message (handled or not)
	// before classification — used by the --dump discovery tool.
	OnMessage func(kind probe.Kind, code int, params map[byte]interface{})
}

// NewRunner creates a Runner with the given classifier and verdict thresholds.
func NewRunner(clf *probe.Classifier, th probe.Thresholds) *Runner {
	return &Runner{clf: clf, th: th}
}

// Run consumes the source to completion (or ctx cancel), persists observations
// and the coverage rollup, and returns the result.
func (r *Runner) Run(ctx context.Context, src port.PacketSource, store port.Store,
	session model.CaptureSession, nowMS func() int64) (Result, error) {

	// Persistence must NOT be tied to the capture-stop signal: when the user
	// presses Ctrl+C, ctx cancels to stop capture, but we still need to flush
	// and write the report. Use a context that survives that cancellation.
	storeCtx := context.WithoutCancel(ctx)

	if err := store.StartSession(storeCtx, session); err != nil {
		return Result{}, err
	}

	cov := probe.NewCoverage(r.th)
	var totals model.SessionTotals
	unhandled := make(map[string]int)
	batch := make([]model.Observation, 0, observationBatch)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := store.AppendObservations(storeCtx, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	record := func(kind probe.Kind, code int, params map[byte]interface{}) {
		totals.DecodedCount++
		if r.OnMessage != nil {
			r.OnMessage(kind, code, params)
		}
		cl, ok := r.clf.Classify(kind, code, params)
		if !ok {
			totals.UnhandledCount++
			kc := "E"
			if kind == probe.KindResponse {
				kc = "R"
			} else if kind == probe.KindRequest {
				kc = "Q"
			}
			if len(unhandled) < 4096 { // bounded (Principle XI)
				unhandled[fmt.Sprintf("%s:%d", kc, code)]++
			}
			return
		}
		cov.Add(cl.Category, cl.FieldsPresent, cl.FieldsExpected)
		batch = append(batch, model.Observation{
			SessionID: session.ID, TS: nowMS(), Category: cl.Category,
			MessageCode: code, FieldsPresent: cl.FieldsPresent, FieldsExpected: cl.FieldsExpected,
		})
		if len(batch) >= observationBatch {
			_ = flush()
		}
	}

	parser := photon.NewPhotonParser(
		func(opByte byte, params map[byte]interface{}) {
			record(probe.KindRequest, codeFor(params, opCodeParam, int(opByte)), params)
		},
		func(opByte byte, _ int16, _ string, params map[byte]interface{}) {
			record(probe.KindResponse, codeFor(params, opCodeParam, int(opByte)), params)
		},
		func(evByte byte, params map[byte]interface{}) {
			record(probe.KindEvent, codeFor(params, eventCodeParam, int(evByte)), params)
		},
	)
	parser.OnEncrypted = func() { totals.EncryptedCount++ }

	ch, err := src.Packets(ctx)
	if err != nil {
		return Result{}, err
	}
	for payload := range ch {
		parser.ReceivePacket(payload)
	}
	if err := flush(); err != nil {
		return Result{}, err
	}

	coverage := cov.Rollup(session.ID, totals.EncryptedRate())
	if err := store.UpsertCoverage(storeCtx, coverage); err != nil {
		return Result{}, err
	}
	if err := store.EndSession(storeCtx, session.ID, nowMS(), totals); err != nil {
		return Result{}, err
	}
	return Result{Totals: totals, Coverage: coverage, Unhandled: unhandled}, nil
}

const (
	eventCodeParam byte = 252
	opCodeParam    byte = 253
)

// codeFor reads the semantic Albion code from a routing param, falling back to
// the Photon message byte code when absent.
func codeFor(params map[byte]interface{}, key byte, fallback int) int {
	if v, ok := params[key]; ok {
		if n, ok := toInt(v); ok {
			return n
		}
	}
	return fallback
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case byte:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}
