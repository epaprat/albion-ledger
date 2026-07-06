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

func TestWarmShorterThanEnum(t *testing.T) {
	e := New()
	e.Decode([]int{6, 17, 22, 30}, []int{1, 2, 3, 4}) // enum len 4
	// A shorter warm k3 decodes only the positions it covers (no OOB).
	pairs := e.Decode(nil, []int{9, 9})
	if len(pairs) != 2 || pairs[1].ID != 17 {
		t.Fatalf("short warm decode wrong: %+v", pairs)
	}
}
