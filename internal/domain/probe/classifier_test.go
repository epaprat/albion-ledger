package probe

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func TestClassifyKnownEvent(t *testing.T) {
	c := New()
	// NewEquipmentItem (30) with its expected fields {0,1} present.
	got, ok := c.Classify(KindEvent, evNewEquipmentItem, map[byte]interface{}{0: int64(1), 1: int16(2)})
	if !ok {
		t.Fatal("NewEquipmentItem should classify")
	}
	if got.Category != model.CatEquipment {
		t.Fatalf("category = %s, want equipment", got.Category)
	}
	if got.FieldsPresent != 2 || got.FieldsExpected != 2 {
		t.Fatalf("completeness fields = %d/%d, want 2/2", got.FieldsPresent, got.FieldsExpected)
	}
}

func TestCharacterSpecResponseGuard(t *testing.T) {
	c := New()
	// The own-state response (code 2) WITH the masteries array (key 55) classifies.
	got, ok := c.Classify(KindResponse, opPlayerState, map[byte]interface{}{55: []int32{1, 2, 3}})
	if !ok || got.Category != model.CatCharacterSpec || got.FieldsPresent != 1 {
		t.Fatalf("player-state with key 55 → %v ok=%v present=%d, want character_spec 1", got.Category, ok, got.FieldsPresent)
	}
	// Same code WITHOUT key 55 (e.g. a bare ping reusing code 2) must NOT classify.
	if _, ok := c.Classify(KindResponse, opPlayerState, map[byte]interface{}{0: int64(1)}); ok {
		t.Fatal("code 2 without masteries key must be unhandled (guard)")
	}
}

func TestClassifyKnownResponse(t *testing.T) {
	c := New()
	got, ok := c.Classify(KindResponse, opAuctionGetOffers, map[byte]interface{}{0: []string{"order"}})
	if !ok || got.Category != model.CatMarketSellOrders {
		t.Fatalf("AuctionGetOffers → %v ok=%v, want market_sell_orders", got.Category, ok)
	}
}

func TestUnknownCodeIsUnhandled(t *testing.T) {
	c := New()
	if _, ok := c.Classify(KindEvent, 9999, nil); ok {
		t.Fatal("unknown code must be unhandled")
	}
}

// TestPositionCodesExcluded enforces Constitution V / FR-004: position and
// movement codes MUST resolve to unhandled, never to a category.
func TestPositionCodesExcluded(t *testing.T) {
	c := New()
	for _, code := range PositionCodes.Events {
		if cl, ok := c.Classify(KindEvent, code, map[byte]interface{}{0: 1, 1: 2}); ok {
			t.Fatalf("position event code %d classified as %s — must be unhandled", code, cl.Category)
		}
	}
	for _, code := range PositionCodes.Ops {
		if cl, ok := c.Classify(KindResponse, code, map[byte]interface{}{0: 1}); ok {
			t.Fatalf("position op code %d classified as %s — must be unhandled", code, cl.Category)
		}
	}
}

func TestEMVVariants(t *testing.T) {
	c := New()
	// Variant A: {0=id, 1=value}.
	if got, ok := c.Classify(KindEvent, evEstimatedMarketValue, map[byte]interface{}{0: []int16{1879}, 1: []int32{500}}); !ok || got.Category != model.CatItemValueEMV || got.FieldsPresent != 2 {
		t.Fatalf("EMV variant A → %v ok=%v present=%d, want emv 2", got.Category, ok, got.FieldsPresent)
	}
	// Variant B: {2=id, 3=quality, 4=value}.
	if got, ok := c.Classify(KindEvent, evEstimatedMarketValue, map[byte]interface{}{2: []int16{7457}, 3: []byte{1}, 4: []int32{500}}); !ok || got.Category != model.CatItemValueEMV || got.FieldsPresent != 2 {
		t.Fatalf("EMV variant B → %v ok=%v present=%d, want emv 2", got.Category, ok, got.FieldsPresent)
	}
	// No value array → unhandled.
	if _, ok := c.Classify(KindEvent, evEstimatedMarketValue, map[byte]interface{}{7: byte(0)}); ok {
		t.Fatal("EMV with no value key must be unhandled")
	}
}

func TestPartialCompleteness(t *testing.T) {
	c := New()
	// Silver expects 3 fields {0,3,5}; provide 2.
	got, _ := c.Classify(KindEvent, evTakeSilver, map[byte]interface{}{0: 1, 3: 2})
	if got.FieldsPresent != 2 || got.FieldsExpected != 3 {
		t.Fatalf("got %d/%d, want 2/3", got.FieldsPresent, got.FieldsExpected)
	}
}
