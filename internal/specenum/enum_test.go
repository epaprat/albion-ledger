package specenum

import "testing"

func TestDecodeFromWire(t *testing.T) {
	e := New()
	// Cold login: ids present → direct pairs + enum learned.
	pairs := e.Decode([]int{6, 17, 22}, []int{10, 85, 100})
	if len(pairs) != 3 || pairs[1] != (Pair{17, 85}) || pairs[2] != (Pair{22, 100}) {
		t.Fatalf("wire decode wrong: %+v", pairs)
	}
	if !e.Known() {
		t.Fatal("enum must be learned from the wire id list")
	}
}

func TestDecodeWarmUsesEnum(t *testing.T) {
	e := New()
	e.Decode([]int{6, 17, 22}, []int{10, 85, 40}) // cold login learns order
	// Warm login: k2 absent (nil ids), k3 position-indexed by the learned order.
	pairs := e.Decode(nil, []int{11, 90, 100})
	if len(pairs) != 3 || pairs[0] != (Pair{6, 11}) || pairs[2] != (Pair{22, 100}) {
		t.Fatalf("warm decode via enum wrong: %+v", pairs)
	}
}

func TestDecodeWarmWithoutEnumIsNil(t *testing.T) {
	e := New()
	if got := e.Decode(nil, []int{1, 2, 3}); got != nil {
		t.Fatalf("warm decode without a learned enum must be nil (011 fallback), got %+v", got)
	}
}

func TestSnapshotRestore(t *testing.T) {
	e := New()
	e.Decode([]int{6, 17, 22}, []int{1, 2, 3})
	snap := e.Snapshot()
	e2 := New()
	e2.Restore(snap)
	pairs := e2.Decode(nil, []int{4, 5, 6})
	if len(pairs) != 3 || pairs[2] != (Pair{22, 6}) {
		t.Fatalf("restore round-trip wrong: %+v", pairs)
	}
}

func TestWarmLengthMismatchRefused(t *testing.T) {
	e := New()
	e.Decode([]int{6, 17, 22, 30}, []int{1, 2, 3, 4}) // enum len 4
	// A warm k3 whose length differs from the learned order is a different board
	// version — refuse it (nil → 011 fallback) rather than mislabel/drop nodes (FR-004).
	if got := e.Decode(nil, []int{9, 9}); got != nil {
		t.Fatalf("shorter warm k3 must be refused, got %+v", got)
	}
	if got := e.Decode(nil, []int{9, 9, 9, 9, 9}); got != nil {
		t.Fatalf("longer warm k3 must be refused, got %+v", got)
	}
	// Exact length still decodes.
	if got := e.Decode(nil, []int{1, 2, 3, 4}); len(got) != 4 {
		t.Fatalf("exact-length warm decode should work: %+v", got)
	}
}
