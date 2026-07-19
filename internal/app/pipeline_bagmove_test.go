package app

// Bag-move probe integration (feature 023, US2): the gated probe records an object-id
// reuse driven through the real declaration path, and — critically — changes nothing
// when on (FR-006/SC-005): holdings are byte-for-byte identical with the probe off.

import (
	"reflect"
	"testing"
)

// Reusing an objID for a different item across two declarations is recorded.
func TestBagProbeRecordsReuse(t *testing.T) {
	_, p := newGlue(t)
	p.EnableBagProbe()

	p.registerNewItem(32, declParams(500, 920, 1)) // objID 500 = item 920
	p.registerNewItem(32, declParams(500, 837, 1)) // same id, different item → reuse

	res := p.BagProbeResult()
	if res.Observed != 1 {
		t.Fatalf("Observed = %d, want 1", res.Observed)
	}
	if len(res.Records) != 1 || res.Records[0].ObjID != 500 {
		t.Fatalf("record wrong: %+v", res.Records)
	}
	if res.Records[0].PriorRef.Index != 920 || res.Records[0].NewRef.Index != 837 {
		t.Errorf("prior/new items wrong: %+v", res.Records[0])
	}
}

// The probe is read-only: the same declarations produce identical holdings whether
// the probe is on or off.
func TestBagProbeIsSideEffectFree(t *testing.T) {
	run := func(withProbe bool) any {
		svc, p := newGlue(t)
		if withProbe {
			p.EnableBagProbe()
		}
		p.registerNewItem(32, declParams(500, 920, 3))
		p.registerNewItem(32, declParams(500, 837, 1)) // reuse
		p.registerNewItem(32, declParams(501, 910, 2))
		return svc.HoldingsSummary()
	}
	off := run(false)
	on := run(true)
	if !reflect.DeepEqual(off, on) {
		t.Fatalf("probe changed holdings:\n off=%+v\n on =%+v", off, on)
	}
}
