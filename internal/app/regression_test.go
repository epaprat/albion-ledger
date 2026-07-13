package app

// Always-on regression tripwire (feature 023, US3). Replays the deterministic golden
// capture through the REAL pipeline, snapshots the headline aggregates, and diffs
// against the committed baseline. A change that shifts any headline number fails here
// with the field and both values. Re-baseline deliberately: REGRESS_UPDATE=1 go test.
//
// NOTE: the synthetic fixture is minimal, so the baseline is currently all-zero — this
// is a smoke tripwire (the pipeline still yields the known snapshot), not a rich-value
// guard. A value-bearing golden capture is privacy-gated (Principle V) and tracked as
// an open item; the diff machinery is identical either way.

import (
	"context"
	"os"
	"testing"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/regression"
)

const regressionBaseline = "testdata/regression_baseline.json"

func TestRegressionBaseline(t *testing.T) {
	svc, parser := newSoakHarness(t)
	fixture := writeGoldenFixture(t)

	ch, err := capture.NewReplay(fixture).Packets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for payload := range ch {
		parser.ReceivePacket(payload)
	}
	cur := regression.Snapshot(svc)

	if os.Getenv("REGRESS_UPDATE") == "1" {
		if err := regression.Establish(regressionBaseline, cur); err != nil {
			t.Fatal(err)
		}
		t.Skipf("baseline re-established at %s: %+v", regressionBaseline, cur)
	}

	base, found, err := regression.LoadBaseline(regressionBaseline)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("baseline missing at %s — establish it with: REGRESS_UPDATE=1 go test ./internal/app/ -run TestRegressionBaseline", regressionBaseline)
	}
	res := regression.Diff(base, cur)
	if res.Regressed {
		for _, d := range res.Diffs {
			t.Errorf("regression: %s %d -> %d", d.Field, d.Baseline, d.Current)
		}
		t.Fatalf("%d headline aggregate(s) moved vs baseline — intended? re-run with REGRESS_UPDATE=1", len(res.Diffs))
	}
}
