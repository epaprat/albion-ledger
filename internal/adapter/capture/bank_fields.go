package capture

// K-key bank-overview extractors (feature 010). The chain, live-discovered from the
// kb.pcap capture (2026-07-05):
//
//	Q:516 → R:516  locations: k1 = 16-byte vault GUIDs (concatenated), k2 = cluster
//	               ids, k4 = weights, k5 = per-location total value ×10000
//	Q:517 → R:517  tabs of one vault: k0 = vault GUID, k1 = 16-byte tab GUIDs
//	               (concatenated), k2 = REAL tab names, k3 = icon tags, k5 = fill
//	Q:518 → R:1    tab content SUMMARY: k0 = tab GUID, k2 = item indexes, k4 = counts
//	               (type-based rows — no object ids). NOTE: this response carries NO
//	               key 253 (raw op byte 1); classification relies on the opByte
//	               fallback, so the shape checks here are the SECOND LOCK against
//	               unrelated op-1 responses (Principle IV). R:518 is a named variant
//	               of the same layout (carries 253=518 + k8 names, tolerated).
//
// All extractors are ok-returning and reject hostile shapes: length mismatches,
// non-16-byte GUID slots, and oversized lists.

import "encoding/hex"

// Caps: a game account has a bounded number of bank locations and tabs; anything
// past these is a malformed or hostile packet, not a real overview.
const (
	maxBankLocations   = 64
	maxBankTabsPerVault = 64
	maxBankTabRows     = 256
)

// BankLocation is one city entry of the K overview.
type BankLocation struct {
	ClusterID string // raw cluster id ("0006") — resolved to a city name upstream
	VaultGUID string // hex
	Weight    int
	RawValue  int64 // total vault value ×10000 (scaled by the consumer)
}

// BankLocations pulls the location list from R:516.
func BankLocations(params map[byte]interface{}) ([]BankLocation, bool) {
	guids, gok := params[1].([]byte)
	clusters, cok := params[2].([]string)
	if !gok || !cok || len(clusters) == 0 || len(clusters) > maxBankLocations {
		return nil, false
	}
	if len(guids) != 16*len(clusters) {
		return nil, false
	}
	weights, _ := intSlice(params[4])
	values, vok := params[5].([]int64)
	locs := make([]BankLocation, len(clusters))
	for i, c := range clusters {
		locs[i] = BankLocation{
			ClusterID: c,
			VaultGUID: hex.EncodeToString(guids[i*16 : (i+1)*16]),
		}
		if i < len(weights) {
			locs[i].Weight = weights[i]
		}
		if vok && i < len(values) {
			locs[i].RawValue = values[i]
		}
	}
	return locs, true
}

// BankTab is one tab of a vault, with its REAL in-game name.
type BankTab struct {
	TabGUID string // hex
	Name    string
	Icon    string
	Fill    int
}

// BankTabs pulls the tab list of one vault from R:517.
func BankTabs(params map[byte]interface{}) (vaultGUID string, tabs []BankTab, ok bool) {
	vault, vok := params[0].([]byte)
	guids, gok := params[1].([]byte)
	names, nok := params[2].([]string)
	if !vok || len(vault) != 16 || !gok || !nok || len(names) == 0 || len(names) > maxBankTabsPerVault {
		return "", nil, false
	}
	if len(guids) != 16*len(names) {
		return "", nil, false
	}
	icons, _ := params[3].([]string)
	fill, _ := intSlice(params[5])
	tabs = make([]BankTab, len(names))
	for i, n := range names {
		tabs[i] = BankTab{
			TabGUID: hex.EncodeToString(guids[i*16 : (i+1)*16]),
			Name:    n,
		}
		if i < len(icons) {
			tabs[i].Icon = icons[i]
		}
		if i < len(fill) {
			tabs[i].Fill = fill[i]
		}
	}
	return hex.EncodeToString(vault), tabs, true
}

// BankTabRow is one type-based summary row of a tab (no object id). Quality and
// UnitValue come from the parallel arrays k7/k5, decoded live 2026-07-05: k7 =
// quality 1-5 (resources always 1), k5 = per-row UNIT value ×10000 (populated for
// equipment; the game sends 0 for resources). Zero means "not reported".
type BankTabRow struct {
	ItemIndex int
	Count     int
	Quality   int
	UnitValue int64 // silver ×10000; 0 = not reported
}

// BankTabContent pulls a tab content summary from R:1 (no key 253!) or R:518.
// The strict shape gate — 16-byte GUID at k0 AND equal-length item/count arrays —
// is what keeps unrelated op-1 responses out (the classification double-lock).
// The count array is width-variable ([]int16 or []byte when all counts ≤255 —
// live-seen both ways); quality (k7) and unit values (k5) are optional extras.
func BankTabContent(params map[byte]interface{}) (tabGUID string, rows []BankTabRow, ok bool) {
	guid, gok := params[0].([]byte)
	if !gok || len(guid) != 16 {
		return "", nil, false
	}
	idx, iok := intSlice(params[2])
	counts, cok := intSlice(params[4])
	if !cok { // width-variable: small-count tabs arrive as a byte array
		if b, bok := params[4].([]byte); bok {
			counts = make([]int, len(b))
			for i, v := range b {
				counts[i] = int(v)
			}
			cok = true
		}
	}
	if !iok || !cok || len(idx) != len(counts) || len(idx) == 0 || len(idx) > maxBankTabRows {
		return "", nil, false
	}
	qualities := byteOrIntSlice(params[7])
	values := int64Slice(params[5])
	rows = make([]BankTabRow, len(idx))
	for i := range idx {
		rows[i] = BankTabRow{ItemIndex: idx[i], Count: counts[i]}
		if i < len(qualities) && qualities[i] >= 1 && qualities[i] <= 5 {
			rows[i].Quality = qualities[i]
		}
		if i < len(values) && values[i] > 0 {
			rows[i].UnitValue = values[i]
		}
	}
	return hex.EncodeToString(guid), rows, true
}

// byteOrIntSlice reads a small-int array in ANY Photon width — the serializer picks
// the narrowest element type per packet ([]byte, []int16, []int32), so a fixed-type
// assertion silently loses fields on real traffic (the classic width trap).
func byteOrIntSlice(v interface{}) []int {
	if b, ok := v.([]byte); ok {
		out := make([]int, len(b))
		for i, x := range b {
			out[i] = int(x)
		}
		return out
	}
	if n, ok := intSlice(v); ok {
		return n
	}
	return nil
}

// int64Slice reads an int64-ish array in any width ([]int64 or []int32) — values
// ×10000 usually need int64, but small-value tabs may arrive narrower.
func int64Slice(v interface{}) []int64 {
	switch a := v.(type) {
	case []int64:
		return a
	case []int32:
		out := make([]int64, len(a))
		for i, x := range a {
			out[i] = int64(x)
		}
		return out
	}
	return nil
}
