package capture

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

// decodeEvent builds an event packet (real bytes) and runs it through the parser,
// returning the decoded params — satisfying the recorded-bytes rule (Principle III).
func decodeEvent(t *testing.T, fields []photon.Field) map[byte]interface{} {
	t.Helper()
	payload := photon.BuildEventPacket(0, fields)
	var got map[byte]interface{}
	p := photon.NewPhotonParser(nil, nil, func(_ byte, params map[byte]interface{}) { got = params })
	if !p.ReceivePacket(payload) {
		t.Fatal("ReceivePacket false")
	}
	return got
}

func decodeResponse(t *testing.T, opByte byte, fields []photon.Field) map[byte]interface{} {
	t.Helper()
	payload := photon.BuildResponsePacket(opByte, 0, fields)
	var got map[byte]interface{}
	p := photon.NewPhotonParser(nil, func(_ byte, _ int16, _ string, params map[byte]interface{}) { got = params }, nil)
	if !p.ReceivePacket(payload) {
		t.Fatal("ReceivePacket false")
	}
	return got
}

func TestContainerItemsFromBytes(t *testing.T) {
	guid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	params := decodeEvent(t, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 3, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{920, 0, 3723, -1, 837}},
		{Key: 4, Type: photon.TypeInteger, Val: int32(5)},
		{Key: 252, Type: photon.TypeShort, Val: int16(99)},
	})
	cg, owner, idx, ok := ContainerItems(params)
	if !ok {
		t.Fatal("ContainerItems not ok")
	}
	if len(idx) != 3 || idx[0] != 920 || idx[1] != 3723 || idx[2] != 837 {
		t.Fatalf("object ids = %v, want [920 3723 837] (empties filtered)", idx)
	}
	if cg != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("container guid = %q", cg)
	}
	_ = owner
}

// A fresh session's small object ids arrive as []int16, not []int32 (Photon sizes
// int arrays by magnitude). intSlice must read both widths, else the field drops.
func TestIntSliceWidths(t *testing.T) {
	if got, ok := intSlice([]int16{1278, 0, 1325}); !ok || len(got) != 3 || got[0] != 1278 || got[2] != 1325 {
		t.Fatalf("[]int16 → %v ok=%v", got, ok)
	}
	if got, ok := intSlice([]int32{1063460, 0, 1063465}); !ok || got[2] != 1063465 {
		t.Fatalf("[]int32 → %v ok=%v", got, ok)
	}
	if _, ok := intSlice("not a slice"); ok {
		t.Fatal("non-slice must be not-ok")
	}
}

func TestEquippedItemFromBytes(t *testing.T) {
	params := decodeEvent(t, []photon.Field{
		{Key: 1, Type: photon.TypeShort, Val: int16(6977)},
		{Key: 2, Type: photon.TypeShort, Val: int16(2)},
		{Key: 252, Type: photon.TypeShort, Val: int16(30)},
	})
	idx, q, ok := EquippedItem(params)
	if !ok || idx != 6977 || q != 2 {
		t.Fatalf("equipped → idx=%d q=%d ok=%v", idx, q, ok)
	}
}

func TestCurrentCityFromBytes(t *testing.T) {
	// Notification event 163: key 0 = subtype 39 (city), key 2 = {"city":"<Name>"}.
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(39)},
		{Key: 2, Type: photon.TypeString, Val: `{"city":"Fort Sterling"}`},
		{Key: 252, Type: photon.TypeShort, Val: int16(163)},
	})
	city, ok := CurrentCity(params)
	if !ok || city != "Fort Sterling" {
		t.Fatalf("CurrentCity → city=%q ok=%v, want Fort Sterling/true", city, ok)
	}
}

func TestCurrentCityRejectsNonCity(t *testing.T) {
	// Wrong subtype (28 = challenge, not a city) → not-ok.
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(28)},
		{Key: 2, Type: photon.TypeString, Val: `{"reason":"X"}`},
		{Key: 252, Type: photon.TypeShort, Val: int16(163)},
	})
	if _, ok := CurrentCity(params); ok {
		t.Fatal("non-city subtype must be not-ok")
	}
	// Subtype 39 but no city in JSON → not-ok.
	p2 := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(39)},
		{Key: 2, Type: photon.TypeString, Val: `{"other":"Y"}`},
		{Key: 252, Type: photon.TypeShort, Val: int16(163)},
	})
	if _, ok := CurrentCity(p2); ok {
		t.Fatal("subtype 39 without a city must be not-ok")
	}
}

func TestPutItemFromBytes(t *testing.T) {
	guid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(1633639)},
		{Key: 1, Type: photon.TypeInteger, Val: int32(66)},
		{Key: 2, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 252, Type: photon.TypeShort, Val: int16(26)},
	})
	obj, cg, ok := PutItem(params)
	if !ok || obj != 1633639 || cg != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("PutItem → obj=%d cg=%q ok=%v", obj, cg, ok)
	}
}

func TestDeleteItemFromBytes(t *testing.T) {
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(1439513)},
		{Key: 1, Type: photon.TypeInteger, Val: int32(15)},
		{Key: 252, Type: photon.TypeShort, Val: int16(27)},
	})
	obj, ok := DeleteItem(params)
	if !ok || obj != 1439513 {
		t.Fatalf("DeleteItem → obj=%d ok=%v", obj, ok)
	}
}

func TestOwnInventoryFromBytes(t *testing.T) {
	// Own-state response (op-2): key 55 = bag slot object ids (0 = empty slot).
	params := decodeResponse(t, 0, []photon.Field{
		{Key: 55, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{1651108, 0, 1651104, 1651109}},
		{Key: 253, Type: photon.TypeShort, Val: int16(2)},
	})
	ids, ok := OwnInventory(params)
	if !ok || len(ids) != 3 || ids[0] != 1651108 || ids[2] != 1651109 {
		t.Fatalf("OwnInventory → %v ok=%v, want [1651108 1651104 1651109]", ids, ok)
	}
	if _, ok := OwnInventory(map[byte]interface{}{}); ok {
		t.Fatal("missing key 52 must be not-ok")
	}
}

func TestExtractorsTolerateMissing(t *testing.T) {
	if _, _, _, ok := ContainerItems(map[byte]interface{}{}); ok {
		t.Fatal("empty container params must be not-ok")
	}
	if _, _, ok := EquippedItem(map[byte]interface{}{}); ok {
		t.Fatal("empty equipped params must be not-ok")
	}
	if _, ok := OwnInventory(map[byte]interface{}{}); ok {
		t.Fatal("empty own-inventory params must be not-ok")
	}
}
