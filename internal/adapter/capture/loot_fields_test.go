package capture

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

func TestLootSourceFromBytes(t *testing.T) {
	// NewLoot(98): key0 objId, key3 source name.
	p := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(4242)},
		{Key: 3, Type: photon.TypeString, Val: "Elder Treant"},
		{Key: 252, Type: photon.TypeShort, Val: int16(98)},
	})
	id, name, ok := LootSource(p, 98)
	if !ok || id != 4242 || name != "Elder Treant" {
		t.Fatalf("98 → id=%d name=%q ok=%v", id, name, ok)
	}

	// NewLootChest(393): name may live at key4 when key3 is absent.
	p = decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(555)},
		{Key: 4, Type: photon.TypeString, Val: "GREEN_CHEST@Somewhere"},
		{Key: 252, Type: photon.TypeShort, Val: int16(393)},
	})
	id, name, ok = LootSource(p, 393)
	if !ok || id != 555 || name != "GREEN_CHEST@Somewhere" {
		t.Fatalf("393 → id=%d name=%q ok=%v", id, name, ok)
	}

	// NewLootChest(393) with BOTH name keys: key3 must win over key4.
	p = decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(556)},
		{Key: 3, Type: photon.TypeString, Val: "GREEN_CHEST"},
		{Key: 4, Type: photon.TypeString, Val: "GREEN_CHEST@Somewhere"},
		{Key: 252, Type: photon.TypeShort, Val: int16(393)},
	})
	if _, name, ok := LootSource(p, 393); !ok || name != "GREEN_CHEST" {
		t.Fatalf("393 both keys → name=%q, want key3 precedence GREEN_CHEST", name)
	}

	// Hostile: oversized slot array rejected (memory-balloon guard).
	huge := make([]int32, maxWireSlots+1)
	p = decodeEvent(t, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: make([]byte, 16)},
		{Key: 3, Type: photon.TypeArray | photon.TypeInteger, Val: huge},
		{Key: 252, Type: photon.TypeShort, Val: int16(99)},
	})
	if _, _, _, ok := ContainerSlots(p); ok {
		t.Fatal("oversized slot array must be rejected")
	}

	// LootChestOpened(395): objId only.
	p = decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(777)},
		{Key: 252, Type: photon.TypeShort, Val: int16(395)},
	})
	if id, _, ok := LootSource(p, 395); !ok || id != 777 {
		t.Fatalf("395 → id=%d ok=%v", id, ok)
	}
}

func TestContainerSlotsPreservesEmpties(t *testing.T) {
	guid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	p := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(4242)}, // source obj
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 3, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{0, 910, -1, 930}},
		{Key: 252, Type: photon.TypeShort, Val: int16(99)},
	})
	g, src, slots, ok := ContainerSlots(p)
	if !ok || src != 4242 || g != "0102030405060708090a0b0c0d0e0f10" {
		t.Fatalf("ContainerSlots → g=%q src=%d ok=%v", g, src, ok)
	}
	// Length preserved; empty positions zeroed IN PLACE (slot alignment).
	if len(slots) != 4 || slots[0] != 0 || slots[1] != 910 || slots[2] != 0 || slots[3] != 930 {
		t.Fatalf("slots = %v, want [0 910 0 930]", slots)
	}
}

func TestMoveItemFromRequestBytes(t *testing.T) {
	guid := []byte{9, 9, 9, 9, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	payload := photon.BuildRequestPacket(30, []photon.Field{
		{Key: 0, Type: photon.TypeByte, Val: byte(3)}, // src slot
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 253, Type: photon.TypeShort, Val: int16(30)},
	})
	var got map[byte]interface{}
	var op byte
	pr := photon.NewPhotonParser(func(opCode byte, params map[byte]interface{}) {
		op, got = opCode, params
	}, nil, nil)
	if !pr.ReceivePacket(payload) {
		t.Fatal("ReceivePacket false")
	}
	if op != 30 {
		t.Fatalf("raw op byte = %d, want 30", op)
	}
	g, slot, ok := MoveItem(got)
	if !ok || slot != 3 || g != "0909090905060708090a0b0c0d0e0f10" {
		t.Fatalf("MoveItem → g=%q slot=%d ok=%v", g, slot, ok)
	}
}

func TestMoveGivenItemsFromRequestBytes(t *testing.T) {
	guid := []byte{1, 1, 1, 1, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	payload := photon.BuildRequestPacket(39, []photon.Field{
		{Key: 0, Type: photon.TypeArray | photon.TypeByte, Val: guid},
		{Key: 4, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{10, 0, 30}},
		{Key: 253, Type: photon.TypeShort, Val: int16(39)},
	})
	var got map[byte]interface{}
	pr := photon.NewPhotonParser(func(_ byte, params map[byte]interface{}) { got = params }, nil, nil)
	if !pr.ReceivePacket(payload) {
		t.Fatal("ReceivePacket false")
	}
	g, ids, ok := MoveGivenItems(got)
	if !ok || g != "0101010105060708090a0b0c0d0e0f10" {
		t.Fatalf("MoveGivenItems → g=%q ok=%v", g, ok)
	}
	if len(ids) != 2 || ids[0] != 10 || ids[1] != 30 {
		t.Fatalf("ids = %v, want [10 30] (zeros filtered)", ids)
	}
}
