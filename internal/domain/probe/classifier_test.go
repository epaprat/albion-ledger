package probe

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

func TestClassifyKnownEvent(t *testing.T) {
	c := New()
	// CharacterStats (143) with its expected field present.
	got, ok := c.Classify(KindEvent, evCharacterStats, map[byte]interface{}{0: byte(1)})
	if !ok {
		t.Fatal("CharacterStats should classify")
	}
	if got.Category != model.CatCharacterSpec {
		t.Fatalf("category = %s, want character_spec", got.Category)
	}
	if got.FieldsPresent != 1 || got.FieldsExpected != 1 {
		t.Fatalf("completeness fields = %d/%d, want 1/1", got.FieldsPresent, got.FieldsExpected)
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

func TestPartialCompleteness(t *testing.T) {
	c := New()
	// Silver expects 3 fields {0,3,5}; provide 2.
	got, _ := c.Classify(KindEvent, evTakeSilver, map[byte]interface{}{0: 1, 3: 2})
	if got.FieldsPresent != 2 || got.FieldsExpected != 3 {
		t.Fatalf("got %d/%d, want 2/3", got.FieldsPresent, got.FieldsExpected)
	}
}
