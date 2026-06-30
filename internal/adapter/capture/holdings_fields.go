package capture

import (
	"encoding/hex"
	"encoding/json"
	"strings"
)

// Holdings field extractors. Pure functions over decoded Photon params; they
// tolerate missing/odd keys (return ok=false) and never panic (Principle IV).
// Field positions are from live capture (see specs/003 research-fields.md).

// ContainerItems pulls a container's id, its owner id, and its non-empty slot
// item INDICES from an AttachItemContainer event: key 1 = container GUID, key 2 =
// owner GUID (distinguishes bank vault vs player inventory), key 3 = []i32 item
// indices (one per slot, -1/0 = empty). NOT object ids (research 004 R2).
func ContainerItems(params map[byte]interface{}) (containerGUID, ownerGUID string, itemIndices []int, ok bool) {
	raw, has := params[3]
	if !has {
		return "", "", nil, false
	}
	arr, isArr := raw.([]int32)
	if !isArr {
		return "", "", nil, false
	}
	for _, v := range arr {
		if v > 0 { // empty slots are 0 or -1
			itemIndices = append(itemIndices, int(v))
		}
	}
	if g, gok := params[1].([]byte); gok {
		containerGUID = hex.EncodeToString(g)
	}
	if g, gok := params[2].([]byte); gok {
		ownerGUID = hex.EncodeToString(g)
	}
	return containerGUID, ownerGUID, itemIndices, true
}

// BankVault pulls the bank's tab owner ids and names from a BankVaultInfo event:
// key 2 = concatenated 16-byte owner GUIDs (one per tab), key 3 = parallel tab names.
func BankVault(params map[byte]interface{}) (owners, tabNames []string, ok bool) {
	blob, bok := params[2].([]byte)
	if !bok || len(blob) < 16 {
		return nil, nil, false
	}
	for i := 0; i+16 <= len(blob); i += 16 {
		owners = append(owners, hex.EncodeToString(blob[i:i+16]))
	}
	if names, nok := params[3].([]string); nok {
		tabNames = names
	}
	return owners, tabNames, true
}

// EquippedItem pulls (item index, quality) from a NewEquipmentItem event:
// key 1 = item index, key 2 = quality.
func EquippedItem(params map[byte]interface{}) (index, quality int, ok bool) {
	idx, iok := toIntVal(params[1])
	if !iok {
		return 0, 0, false
	}
	q, _ := toIntVal(params[2])
	return idx, q, true
}

// cityChangeSubtype is the key-0 subtype of the notification event (163) that
// announces the player entered a city. Live-verified: key 0 = 39 → key 2 holds
// {"city":"<Name>"}; other subtypes (e.g. 28 = challenge) are ignored.
const cityChangeSubtype = 39

// CurrentCity pulls the player's current city NAME from a notification event (163):
// key 0 = subtype (39 = city entered), key 2 = JSON {"city":"<Name>"}. This is the
// player's own client-side "you entered <city>" notice — own-state, not a position
// (ToS-safe). Returns ok=false for any other subtype or unparseable payload. Tolerates
// odd keys, never panics (Principle IV). See research 004 R1 (live capture 2026-06-30).
func CurrentCity(params map[byte]interface{}) (city string, ok bool) {
	if sub, _ := toIntVal(params[0]); sub != cityChangeSubtype {
		return "", false
	}
	raw, isStr := params[2].(string)
	if !isStr {
		return "", false
	}
	var p struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return "", false
	}
	if p.City = strings.TrimSpace(p.City); p.City == "" {
		return "", false
	}
	return p.City, true
}

// MasteryLevels pulls the mastery level array from an own-state response: key 55.
func MasteryLevels(params map[byte]interface{}) ([]int, bool) {
	arr, ok := params[55].([]int32)
	if !ok {
		return nil, false
	}
	out := make([]int, len(arr))
	for i, v := range arr {
		out[i] = int(v)
	}
	return out, true
}

// IntParam reads an integer-valued param by key.
func IntParam(params map[byte]interface{}, key byte) (int, bool) {
	return toIntVal(params[key])
}

func toIntVal(v interface{}) (int, bool) {
	switch n := v.(type) {
	case byte:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
