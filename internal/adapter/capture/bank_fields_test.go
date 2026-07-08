package capture

// Byte-level fixtures for the K-key bank-overview chain (feature 010). Values mirror
// the kb.pcap discovery capture (2026-07-05): R:516 locations, R:517 tabs, R:1/518
// tab contents. The content response carries NO key 253 — the extractor's strict
// shape guard is the second lock behind the opByte-1 classification (Principle IV).

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

func guid16(prefix byte) []byte {
	b := make([]byte, 16)
	b[0] = prefix
	for i := 1; i < 16; i++ {
		b[i] = byte(i)
	}
	return b
}

func TestBankLocationsFromBytes(t *testing.T) {
	guids := append(guid16(0xAA), guid16(0xBB)...) // 2 locations, 16B each
	params := decodeResponse(t, 3, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guids},
		{Key: 2, Type: photon.TypeArray | photon.TypeString, Val: []string{"0006", "4001"}},
		{Key: 4, Type: photon.TypeArray | photon.TypeShort, Val: []int16{18312, 23898}},
		{Key: 5, Type: photon.TypeArray | photon.TypeInt64, Val: []int64{110942100828, 3187258530}},
		{Key: 253, Type: photon.TypeShort, Val: int16(516)},
	})
	locs, ok := BankLocations(params)
	if !ok || len(locs) != 2 {
		t.Fatalf("BankLocations → %d/%v, want 2/true", len(locs), ok)
	}
	if locs[0].ClusterID != "0006" || locs[0].VaultGUID != "aa0102030405060708090a0b0c0d0e0f" ||
		locs[0].RawValue != 110942100828 {
		t.Fatalf("loc0 wrong: %+v", locs[0])
	}
	if locs[1].ClusterID != "4001" || locs[1].RawValue != 3187258530 {
		t.Fatalf("loc1 wrong: %+v", locs[1])
	}

	// Length mismatch (3 clusters, guid bytes for 2) → reject.
	params = decodeResponse(t, 3, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guids},
		{Key: 2, Type: photon.TypeArray | photon.TypeString, Val: []string{"0006", "4001", "3005"}},
		{Key: 253, Type: photon.TypeShort, Val: int16(516)},
	})
	if _, ok := BankLocations(params); ok {
		t.Fatal("guid/cluster length mismatch must be rejected")
	}
}

func TestBankTabsFromBytes(t *testing.T) {
	tabGUIDs := append(guid16(0x11), guid16(0x22)...)
	params := decodeResponse(t, 3, []photon.Field{
		{Key: 0, Type: photon.TypeArray | photon.TypeByte, Val: guid16(0xAA)},
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: tabGUIDs},
		{Key: 2, Type: photon.TypeArray | photon.TypeString, Val: []string{"Hammadde", "Setler"}},
		{Key: 3, Type: photon.TypeArray | photon.TypeString, Val: []string{"icon_tag_anvil", "icon_tag_chest"}},
		{Key: 5, Type: photon.TypeArray | photon.TypeShort, Val: []int16{13069, 291}},
		{Key: 253, Type: photon.TypeShort, Val: int16(517)},
	})
	vault, tabs, ok := BankTabs(params)
	if !ok || vault != "aa0102030405060708090a0b0c0d0e0f" || len(tabs) != 2 {
		t.Fatalf("BankTabs → vault=%q n=%d ok=%v", vault, len(tabs), ok)
	}
	if tabs[0].TabGUID != "110102030405060708090a0b0c0d0e0f" || tabs[0].Name != "Hammadde" {
		t.Fatalf("tab0 wrong: %+v", tabs[0])
	}
	if tabs[1].Name != "Setler" || tabs[1].Icon != "icon_tag_chest" {
		t.Fatalf("tab1 wrong: %+v", tabs[1])
	}

	// Name/guid length mismatch → reject.
	params = decodeResponse(t, 3, []photon.Field{
		{Key: 0, Type: photon.TypeArray | photon.TypeByte, Val: guid16(0xAA)},
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: tabGUIDs},
		{Key: 2, Type: photon.TypeArray | photon.TypeString, Val: []string{"Solo"}},
		{Key: 253, Type: photon.TypeShort, Val: int16(517)},
	})
	if _, _, ok := BankTabs(params); ok {
		t.Fatal("tab guid/name length mismatch must be rejected")
	}
}

func TestBankTabContentFromBytes(t *testing.T) {
	// The real content response has NO key 253 (raw op byte 1).
	params := decodeResponse(t, 1, []photon.Field{
		{Key: 0, Type: photon.TypeArray | photon.TypeByte, Val: guid16(0x11)},
		{Key: 1, Type: photon.TypeInteger, Val: int32(4)},
		{Key: 2, Type: photon.TypeArray | photon.TypeShort, Val: []int16{1340, 1249, 193}},
		{Key: 4, Type: photon.TypeArray | photon.TypeShort, Val: []int16{1, 19, 346}},
	})
	tab, rows, ok := BankTabContent(params)
	if !ok || tab != "110102030405060708090a0b0c0d0e0f" || len(rows) != 3 {
		t.Fatalf("BankTabContent → tab=%q n=%d ok=%v", tab, len(rows), ok)
	}
	if rows[1].ItemIndex != 1249 || rows[1].Count != 19 {
		t.Fatalf("row1 wrong: %+v", rows[1])
	}

	// Shape locks (contract rule 4): each must reject.
	bad := []map[byte]interface{}{
		{0: guid16(0x11)[:15], 2: []int16{1}, 4: []int16{1}},          // 15B guid
		{0: guid16(0x11), 2: []int16{1, 2}, 4: []int16{1}},            // len mismatch
		{0: guid16(0x11), 4: []int16{1}},                              // no item array
		{0: guid16(0x11), 2: []int16{1}},                              // no count array
		{0: int64(675884), 2: []int16{1}, 4: []int16{1}},              // guid not bytes (E:1-style key 0)
		{0: guid16(0x11), 2: make([]int16, 300), 4: make([]int16, 300)}, // over cap
	}
	for i, p := range bad {
		if _, _, ok := BankTabContent(p); ok {
			t.Fatalf("bad shape %d must be rejected", i)
		}
	}
}

// 010 review: the width trap, third strike — location values arrive []int32 for
// modest vault totals, and item indexes arrive []byte for low-index tabs.
func TestBankWidthVariants(t *testing.T) {
	params := decodeResponse(t, 3, []photon.Field{
		{Key: 1, Type: photon.TypeArray | photon.TypeByte, Val: guid16(0xAA)},
		{Key: 2, Type: photon.TypeArray | photon.TypeString, Val: []string{"0006"}},
		{Key: 5, Type: photon.TypeArray | photon.TypeInteger, Val: []int32{123450000}},
		{Key: 253, Type: photon.TypeShort, Val: int16(516)},
	})
	locs, ok := BankLocations(params)
	if !ok || locs[0].RawValue != 123450000 {
		t.Fatalf("int32-width vault value dropped: %+v ok=%v", locs, ok)
	}

	content := map[byte]interface{}{
		0: guid16(0x11),
		2: []byte{10, 20},  // low indexes arrive as a byte array
		4: []byte{1, 2},
	}
	_, rows, ok := BankTabContent(content)
	if !ok || len(rows) != 2 || rows[1].ItemIndex != 20 {
		t.Fatalf("byte-width indexes rejected: %+v ok=%v", rows, ok)
	}
}

func TestBankTabRequest(t *testing.T) {
	g := make([]byte, 16)
	g[0] = 0xd6
	if guid, ok := BankTabRequest(map[byte]interface{}{0: g}); !ok || guid != "d6000000000000000000000000000000" {
		t.Fatalf("BankTabRequest wrong: %q ok=%v", guid, ok)
	}
	if _, ok := BankTabRequest(map[byte]interface{}{0: []byte{1, 2}}); ok {
		t.Fatal("short guid must reject")
	}
}

func TestBankTabContentGuidless(t *testing.T) {
	// The default/open tab's content arrives with NO guid at k0 — still parses; the
	// handler supplies the guid from the pending tab request (010 fix).
	params := map[byte]interface{}{
		2: []int16{101, 102, 103}, // item indexes
		4: []int16{1, 2, 3},       // counts
		7: []byte{1, 1, 1},        // qualities
	}
	guid, rows, ok := BankTabContent(params)
	if !ok || guid != "" || len(rows) != 3 {
		t.Fatalf("guid-less content: guid=%q rows=%d ok=%v", guid, len(rows), ok)
	}
	if rows[1].ItemIndex != 102 || rows[1].Count != 2 {
		t.Fatalf("row parse wrong: %+v", rows[1])
	}
}
