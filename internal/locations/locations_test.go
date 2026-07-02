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

// Generated instance ids (per-instance GUIDs no catalog can list) resolve to their
// type's friendly name — live-hit 2026-07-02: "@CORRUPTEDDUNGEON@4d5f…" in By zone.
func TestFriendlyGeneratedInstances(t *testing.T) {
	cases := map[string]string{
		"@CORRUPTEDDUNGEON@4d5ffb2e-463e-47b6-a2e9-3867c84c83eb": "Corrupted Dungeon",
		"@RANDOMDUNGEON@deadbeef-1111":                           "Dungeon",
		"@MISTSDUNGEON@x":                                        "Mists Dungeon",
		"@MISTS@y":                                               "Mists",
		"@HELLCLUSTER@z":                                         "Hellgate",
		"@hellcluster@lower":                                     "Hellgate", // case-insensitive
		"Pen Gent":                                               "Pen Gent", // no '@' → unchanged
		"4207":                                                   "4207",
	}
	for in, want := range cases {
		if got := Friendly(in); got != want {
			t.Fatalf("Friendly(%q) = %q, want %q", in, got, want)
		}
	}
	// Resolve falls through: catalog miss + instance id → friendly.
	l, _ := New([]byte(`{"clusters":{"4207":"Pen Gent"}}`))
	if got := l.Resolve("@CORRUPTEDDUNGEON@abc"); got != "Corrupted Dungeon" {
		t.Fatalf("Resolve instance → %q, want Corrupted Dungeon", got)
	}
}
