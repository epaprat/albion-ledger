package bagmove

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/holdings"
)

func ref(idx, q, n int) holdings.ItemRef { return holdings.ItemRef{Index: idx, Quality: q, Count: n} }

// A differing item on the same id is recorded with prior/new/timestamps.
func TestObserveRecordsReuse(t *testing.T) {
	d := New(0)
	if r := d.Observe(100, ref(1201, 1, 5), 1000); r != nil {
		t.Fatal("first sighting must not record")
	}
	r := d.Observe(100, ref(990, 0, 1), 2000)
	if r == nil {
		t.Fatal("differing item on same id must record a reuse")
	}
	if r.PriorRef.Index != 1201 || r.NewRef.Index != 990 || r.FirstSeenMS != 1000 || r.ReuseMS != 2000 {
		t.Errorf("record fields wrong: %+v", *r)
	}
	if res := d.Result(); res.Observed != 1 || len(res.Records) != 1 {
		t.Errorf("Observed=%d records=%d, want 1/1", res.Observed, len(res.Records))
	}
}

// A count-only change is a restock, not a bag-move.
func TestObserveIgnoresCountOnly(t *testing.T) {
	d := New(0)
	d.Observe(7, ref(500, 2, 3), 100)
	if r := d.Observe(7, ref(500, 2, 9), 200); r != nil {
		t.Fatalf("count change must not record, got %+v", *r)
	}
	if d.Result().Observed != 0 {
		t.Error("count-only change counted as reuse")
	}
}

// A quality change on the same id/index IS a bag-move.
func TestObserveQualityChangeRecords(t *testing.T) {
	d := New(0)
	d.Observe(9, ref(500, 1, 1), 100)
	if r := d.Observe(9, ref(500, 3, 1), 200); r == nil {
		t.Fatal("quality change must record")
	}
}

// Result reports zero explicitly on a clean run (never silent).
func TestResultZeroIsExplicit(t *testing.T) {
	d := New(0)
	for i := 0; i < 50; i++ {
		d.Observe(i, ref(i, 0, 1), int64(i))
	}
	res := d.Result()
	if res.Observed != 0 {
		t.Errorf("Observed=%d, want 0", res.Observed)
	}
	if res.Declarations != 50 {
		t.Errorf("Declarations=%d, want 50", res.Declarations)
	}
}

// Reset clears session state.
func TestResetClears(t *testing.T) {
	d := New(0)
	d.Observe(1, ref(1, 0, 1), 10)
	d.Observe(1, ref(2, 0, 1), 20) // reuse
	d.Reset()
	res := d.Result()
	if res.Observed != 0 || res.Declarations != 0 || len(res.Records) != 0 {
		t.Errorf("after reset: %+v", res)
	}
}
