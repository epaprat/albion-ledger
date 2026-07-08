package capture

import "testing"

func TestSellOrderSetup(t *testing.T) {
	// Q:79 — qty k1=1, item obj k2, unit price k3 (×10000).
	obj, qty, unit, ok := SellOrderSetup(map[byte]interface{}{1: int32(1), 2: int32(123807), 3: int64(1798340000), 4: int32(720)})
	if !ok || obj != 123807 || qty != 1 || unit != 1798340000 {
		t.Fatalf("SellOrderSetup wrong: obj=%d qty=%d unit=%d ok=%v", obj, qty, unit, ok)
	}
	if _, _, _, ok := SellOrderSetup(map[byte]interface{}{1: int32(1)}); ok {
		t.Fatal("missing price must reject")
	}
}

func TestBuyOrderSetup(t *testing.T) {
	// Q:80 — item idx k1=752, qty k2=20, unit price k5 (×10000).
	idx, qty, unit, ok := BuyOrderSetup(map[byte]interface{}{1: int32(752), 2: int32(20), 3: int32(1), 4: int32(720), 5: int64(53260000)})
	if !ok || idx != 752 || qty != 20 || unit != 53260000 {
		t.Fatalf("BuyOrderSetup wrong: idx=%d qty=%d unit=%d ok=%v", idx, qty, unit, ok)
	}
	if _, _, _, ok := BuyOrderSetup(map[byte]interface{}{1: int32(752), 2: int32(20)}); ok {
		t.Fatal("missing price must reject")
	}
}
