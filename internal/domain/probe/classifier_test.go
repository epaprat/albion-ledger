package probe

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/codes"
	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// testRegistry covers the codes exercised by these tests.
const testCodes = `{"entries":[
  {"kind":"event","code":30,"category":"equipment"},
  {"kind":"event","code":62,"category":"silver"},
  {"kind":"event","code":466,"category":"item_value_emv"},
  {"kind":"response","code":82,"category":"market_sell_orders"},
  {"kind":"response","code":2,"category":"character_spec","guardKey":55}
]}`

func newClassifier(t *testing.T) *Classifier {
	t.Helper()
	reg, err := codes.New([]byte(testCodes))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return New(reg)
}

func TestClassifyKnownEvent(t *testing.T) {
	c := newClassifier(t)
	got, ok := c.Classify(KindEvent, 30, map[byte]interface{}{0: int64(1), 1: int16(2)})
	if !ok || got.Category != model.CatEquipment || got.FieldsPresent != 2 {
		t.Fatalf("equipment → %v ok=%v present=%d", got.Category, ok, got.FieldsPresent)
	}
}

func TestClassifyKnownResponse(t *testing.T) {
	c := newClassifier(t)
	got, ok := c.Classify(KindResponse, 82, map[byte]interface{}{0: []string{"order"}})
	if !ok || got.Category != model.CatMarketSellOrders {
		t.Fatalf("market sell → %v ok=%v", got.Category, ok)
	}
}

func TestUnknownCodeIsUnhandled(t *testing.T) {
	c := newClassifier(t)
	if _, ok := c.Classify(KindEvent, 9999, nil); ok {
		t.Fatal("unknown code must be unhandled")
	}
}

func TestPositionCodesExcluded(t *testing.T) {
	c := newClassifier(t)
	for _, code := range PositionCodes.Events {
		if cl, ok := c.Classify(KindEvent, code, map[byte]interface{}{0: 1, 1: 2}); ok {
			t.Fatalf("position event %d classified as %s — must be unhandled", code, cl.Category)
		}
	}
	for _, code := range PositionCodes.Ops {
		if cl, ok := c.Classify(KindResponse, code, map[byte]interface{}{0: 1}); ok {
			t.Fatalf("position op %d classified as %s — must be unhandled", code, cl.Category)
		}
	}
}

func TestCharacterSpecResponseGuard(t *testing.T) {
	c := newClassifier(t)
	if got, ok := c.Classify(KindResponse, 2, map[byte]interface{}{55: []int32{1, 2, 3}}); !ok || got.Category != model.CatCharacterSpec {
		t.Fatalf("player-state with key 55 → %v ok=%v", got.Category, ok)
	}
	if _, ok := c.Classify(KindResponse, 2, map[byte]interface{}{0: int64(1)}); ok {
		t.Fatal("code 2 without masteries key must be unhandled (guard)")
	}
}

func TestEMVVariants(t *testing.T) {
	c := newClassifier(t)
	if got, ok := c.Classify(KindEvent, evEstimatedMarketValue, map[byte]interface{}{0: []int16{1879}, 1: []int32{500}}); !ok || got.FieldsPresent != 2 {
		t.Fatalf("EMV variant A → ok=%v present=%d", ok, got.FieldsPresent)
	}
	if got, ok := c.Classify(KindEvent, evEstimatedMarketValue, map[byte]interface{}{2: []int16{7457}, 3: []byte{1}, 4: []int32{500}}); !ok || got.FieldsPresent != 2 {
		t.Fatalf("EMV variant B → ok=%v present=%d", ok, got.FieldsPresent)
	}
	if _, ok := c.Classify(KindEvent, evEstimatedMarketValue, map[byte]interface{}{7: byte(0)}); ok {
		t.Fatal("EMV with no value key must be unhandled")
	}
}

func TestPartialCompleteness(t *testing.T) {
	c := newClassifier(t)
	// Silver (TakeSilver 62) expects 5 fields {0,3,5,6,8}; provide 2 (keys 0,3).
	got, _ := c.Classify(KindEvent, 62, map[byte]interface{}{0: 1, 3: 2})
	if got.FieldsPresent != 2 || got.FieldsExpected != 5 {
		t.Fatalf("got %d/%d, want 2/5", got.FieldsPresent, got.FieldsExpected)
	}
}
