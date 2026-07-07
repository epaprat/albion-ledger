package wailsadapter

// Export core tests (013): the dialog-free paths — key→getter mapping, header
// contracts, file writing, ExportAll independence (contract §4/§5).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func TestBuildDatasetHeaders(t *testing.T) {
	svc, _ := newSvc(t)
	want := map[string]string{
		"holdings": "city",
		"flow":     "time",
		"zones":    "zone",
		"market":   "item",
		"spec":     "id",
	}
	for _, key := range DatasetKeys {
		header, _, err := svc.buildDataset(key, "")
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if header[0] != want[key] {
			t.Fatalf("%s header contract broken: starts with %q", key, header[0])
		}
	}
}

func TestBuildDatasetUnknownKey(t *testing.T) {
	svc, _ := newSvc(t)
	if _, _, err := svc.buildDataset("nope", ""); err == nil {
		t.Fatal("unknown dataset key must error")
	}
}

func TestWriteDatasetAndRowCount(t *testing.T) {
	svc, _ := newSvc(t)
	svc.SetSpec(model.CharacterSpec{Masteries: []model.MasteryLevel{
		{Index: 96, Name: "Dagger Pair", Level: 100, Touched: true},
		{Index: 22, Name: "Bear Paws", Level: 99, Touched: true},
	}})
	path := filepath.Join(t.TempDir(), "spec.csv")
	res := svc.writeDataset("spec", "", path)
	if res.Err != "" || res.Rows != 2 || res.Path != path {
		t.Fatalf("write result wrong: %+v", res)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "\xEF\xBB\xBF"+"id,name,") {
		t.Fatalf("file must start with BOM+header, got %q", data[:20])
	}
	if !strings.Contains(string(data), "Dagger Pair") {
		t.Fatal("rows missing from written file")
	}
}

func TestWriteDatasetEmptyStillWrites(t *testing.T) {
	svc, _ := newSvc(t)
	path := filepath.Join(t.TempDir(), "market.csv")
	res := svc.writeDataset("market", "", path)
	if res.Err != "" || res.Rows != 0 {
		t.Fatalf("empty dataset must write header-only with Rows=0: %+v", res)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("header-only file must exist")
	}
}

func TestExportAllToIndependentFailure(t *testing.T) {
	svc, _ := newSvc(t)
	dir := t.TempDir()
	// Make the target unwritable for ONE dataset by pre-creating a directory at
	// its exact path — os.Create fails, the others still succeed.
	stamp := time.Date(2026, 7, 6, 15, 30, 0, 0, time.Local)
	blocked := filepath.Join(dir, "albion-flow-20260706-153000.csv")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	results := svc.exportAllTo(dir, "", stamp)
	if len(results) != len(DatasetKeys) {
		t.Fatalf("want %d results, got %d", len(DatasetKeys), len(results))
	}
	for i, key := range DatasetKeys {
		if results[i].Dataset != key {
			t.Fatalf("result order broken at %d: %s", i, results[i].Dataset)
		}
		if key == "flow" {
			if results[i].Err == "" {
				t.Fatal("blocked flow export must report an error")
			}
			continue
		}
		if results[i].Err != "" {
			t.Fatalf("%s must succeed despite flow failure: %+v", key, results[i])
		}
	}
}

func TestExportDatasetWithoutUIContext(t *testing.T) {
	svc, _ := newSvc(t)
	// Unknown key errors BEFORE any dialog/context use.
	if res := svc.ExportDataset("nope", ""); res.Err == "" {
		t.Fatal("unknown key must error")
	}
	// Valid key without a UI context must fail gracefully, not panic.
	if res := svc.ExportDataset("spec", ""); res.Err == "" || res.Canceled {
		t.Fatalf("no-UI-context export must report an error: %+v", res)
	}
}
