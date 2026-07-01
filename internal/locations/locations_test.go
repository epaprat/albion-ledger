package locations

import "testing"

func TestResolve(t *testing.T) {
	l, err := New([]byte(`{"clusters":{"4207":"Pen Gent","1000":"Thetford"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Resolve("4207"); got != "Pen Gent" {
		t.Fatalf("4207 → %q, want Pen Gent", got)
	}
	// Unknown id falls back to the raw id (stable, groupable).
	if got := l.Resolve("9999"); got != "9999" {
		t.Fatalf("unknown → %q, want raw 9999", got)
	}
}

func TestBadJSON(t *testing.T) {
	if _, err := New([]byte("not json")); err == nil {
		t.Fatal("want error on bad json")
	}
}
