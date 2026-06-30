package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func TestReportIncludesEveryCategory(t *testing.T) {
	cov := probe.NewCoverage(probe.DefaultThresholds())
	cov.Add(model.CatLoot, 2, 2)
	rows := cov.Rollup("s1", 0.05)

	sess := model.CaptureSession{ID: "s1", SourceKind: model.SourceReplay, Interface: "f.pcap"}
	totals := model.SessionTotals{DecodedCount: 20, EncryptedCount: 1, UnhandledCount: 3}

	r := Build(sess, totals, rows, nil)

	if len(r.Categories) != len(model.AllCategories) {
		t.Fatalf("report categories = %d, want %d", len(r.Categories), len(model.AllCategories))
	}

	// JSON round-trips and contains a not_observed row.
	b, err := r.JSON()
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	var back CoverageReport
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	notObserved := 0
	for _, c := range back.Categories {
		if c.Verdict == string(model.VerdictNotObserved) {
			notObserved++
		}
	}
	if notObserved == 0 {
		t.Fatal("expected not_observed categories in report")
	}

	txt := r.Text()
	if !strings.Contains(txt, "loot") || !strings.Contains(txt, "unhandled=3") {
		t.Fatalf("text render missing expected content:\n%s", txt)
	}
}
