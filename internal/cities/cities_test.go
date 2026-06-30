package cities

import "testing"

func TestName(t *testing.T) {
	tbl := New([]byte(`{"cities":{"3005":"Caerleon"}}`))
	if got := tbl.Name("3005"); got != "Caerleon" {
		t.Fatalf("known id → %q, want Caerleon", got)
	}
	if got := tbl.Name("9999"); got != "Unknown city (9999)" {
		t.Fatalf("unknown id → %q", got)
	}
	if got := tbl.Name("@ISLAND@abc"); got != "Island" {
		t.Fatalf("island → %q", got)
	}
	if got := tbl.Name("  "); got != "" {
		t.Fatalf("empty id → %q, want empty", got)
	}
}

func TestMalformedFileIsUsable(t *testing.T) {
	tbl := New([]byte(`not json`))
	if got := tbl.Name("3005"); got != "Unknown city (3005)" {
		t.Fatalf("malformed file should still resolve via fallback, got %q", got)
	}
}
