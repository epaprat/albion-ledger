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

// Generated instance ids (per-instance GUIDs no catalog can list) map to their
// type's friendly name AT DISPLAY TIME — live-hit 2026-07-02: "@CORRUPTEDDUNGEON@…".
func TestFriendlyGeneratedInstances(t *testing.T) {
	cases := map[string]string{
		"@CORRUPTEDDUNGEON@4d5ffb2e-463e-47b6-a2e9-3867c84c83eb": "Corrupted Dungeon",
		"@RANDOMDUNGEON@deadbeef-1111":                           "Dungeon",
		"@MISTSDUNGEON@x":                                        "Mists Dungeon",
		"@MISTS@y":                                               "Mists",
		"@HELLCLUSTER@z":                                         "Hellgate",
		"@hellcluster@lower":                                     "Hellgate", // case-insensitive
		"@EXPEDITION@e":                                          "Expedition",
		"@ARENA@a":                                               "Arena",
		"@HELLDUNGEON@h":                                         "Abyssal Depths",
		"@island@i-guild-0001":                                   "Island",
		"Pen Gent":                                               "Pen Gent", // no '@' → unchanged
		"4207":                                                   "4207",
		"@UNKNOWNTYPE@guid":                                      "@UNKNOWNTYPE@guid", // unmatched token → raw
	}
	for in, want := range cases {
		if got := Friendly(in); got != want {
			t.Fatalf("Friendly(%q) = %q, want %q", in, got, want)
		}
	}
	// Resolve must NOT coarsen: raw instance ids are persisted as-is (write-time
	// lossless); Friendly is a read/display concern.
	l, _ := New([]byte(`{"clusters":{"4207":"Pen Gent"}}`))
	if got := l.Resolve("@CORRUPTEDDUNGEON@abc"); got != "@CORRUPTEDDUNGEON@abc" {
		t.Fatalf("Resolve must keep raw instance id, got %q", got)
	}
	if got := l.Resolve("4207"); got != "Pen Gent" {
		t.Fatalf("catalog hit → %q, want Pen Gent", got)
	}
}
