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

// BankTabRow is one type-based summary row of a tab (no object id).
type BankTabRow struct {
	ItemIndex int
	Count     int
}

// BankTabContent pulls a tab content summary from R:1 (no key 253!) or R:518.
// The strict shape gate — 16-byte GUID at k0 AND equal-length item/count arrays —
// is what keeps unrelated op-1 responses out (the classification double-lock).
func BankTabContent(params map[byte]interface{}) (tabGUID string, rows []BankTabRow, ok bool) {
	guid, gok := params[0].([]byte)
	if !gok || len(guid) != 16 {
		return "", nil, false
	}
	idx, iok := intSlice(params[2])
	counts, cok := intSlice(params[4])
	if !iok || !cok || len(idx) != len(counts) || len(idx) == 0 || len(idx) > maxBankTabRows {
		return "", nil, false
	}
	rows = make([]BankTabRow, len(idx))
	for i := range idx {
		rows[i] = BankTabRow{ItemIndex: idx[i], Count: counts[i]}
	}
	return hex.EncodeToString(guid), rows, true
}
