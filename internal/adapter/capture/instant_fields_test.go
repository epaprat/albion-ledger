package capture

import "testing"

func TestInstantSell(t *testing.T) {
	// Q:315 — item type index k2=3543, amount k4=1, buy order id k1.
	if idx, amt, oid, ok := InstantSell(map[byte]interface{}{1: int64(3176375561), 2: int32(3543), 4: int32(1)}); !ok || idx != 3543 || amt != 1 || oid != 3176375561 {
		t.Fatalf("InstantSell wrong: idx=%d amt=%d oid=%d ok=%v", idx, amt, oid, ok)
	}
	if _, _, _, ok := InstantSell(map[byte]interface{}{2: int32(3543)}); ok {
		t.Fatal("missing amount must reject")
	}
	if _, _, _, ok := InstantSell(map[byte]interface{}{4: int32(1)}); ok {
		t.Fatal("missing item must reject")
	}
}

func TestInstantBuy(t *testing.T) {
	// Q:83 — amount k1=5, order id k2.
	if amt, oid, ok := InstantBuy(map[byte]interface{}{1: int32(5), 2: int64(3176516525)}); !ok || amt != 5 || oid != 3176516525 {
		t.Fatalf("InstantBuy wrong: amt=%d oid=%d ok=%v", amt, oid, ok)
	}
	if _, _, ok := InstantBuy(map[byte]interface{}{1: int32(5)}); ok {
		t.Fatal("missing order id must reject")
	}
}

func TestQuicksell(t *testing.T) {
	// Q:485 — batch of 9 item object ids at k1, order ids at k2.
	if n, oids, ok := Quicksell(map[byte]interface{}{1: make([]int32, 9), 2: make([]int64, 9)}); !ok || n != 9 || len(oids) != 9 {
		t.Fatalf("Quicksell wrong: n=%d oids=%d ok=%v", n, len(oids), ok)
	}
	if _, _, ok := Quicksell(map[byte]interface{}{1: []int32{}}); ok {
		t.Fatal("empty batch must reject")
	}
}
