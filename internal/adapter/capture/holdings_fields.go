package capture

import (
	"encoding/hex"
	"encoding/json"
	"strings"
)

// Holdings field extractors. Pure functions over decoded Photon params; they
// tolerate missing/odd keys (return ok=false) and never panic (Principle IV).
// Field positions are from live capture (see specs/003 research-fields.md).

// intSlice reads an integer array param regardless of its Photon element width.
// Photon sizes int arrays by value magnitude, so the SAME field is []int16 when the
// values are small (e.g. object ids early in a session) and []int32 when large — both
// must be accepted or the field silently drops.
func intSlice(v interface{}) ([]int, bool) {
	switch a := v.(type) {
	case []int32:
		out := make([]int, len(a))
		for i, x := range a {
			out[i] = int(x)
		}
		return out, true
	case []int16:
		out := make([]int, len(a))
		for i, x := range a {
			out[i] = int(x)
		}
		return out, true
	case []int64:
		out := make([]int, len(a))
		for i, x := range a {
			out[i] = int(x)
		}
		return out, true
	}
	return nil, false
}

// ContainerItems pulls a container's id, its owner id, and its non-empty slot
// in-world OBJECT IDS from an AttachItemContainer event: key 1 = container GUID,
// key 2 = owner GUID (distinguishes bank vault vs player inventory), key 3 = slot
// object ids (one per slot, -1/0 = empty). The caller resolves them via the New*Item
// object registry. (Real capture 2026-07-01: slots are object ids; small ones arrive
// as []int16 in a fresh session, larger ones as []int32 — both handled.)
func ContainerItems(params map[byte]interface{}) (containerGUID, ownerGUID string, objIDs []int, ok bool) {
	arr, isArr := intSlice(params[3])
	if !isArr {
		return "", "", nil, false
	}
	for _, v := range arr {
		if v > 0 { // empty slots are 0 or -1
			objIDs = append(objIDs, v)
		}
	}
	if g, gok := params[1].([]byte); gok {
		containerGUID = hex.EncodeToString(g)
	}
	if g, gok := params[2].([]byte); gok {
		ownerGUID = hex.EncodeToString(g)
	}
	return containerGUID, ownerGUID, objIDs, true
}

// PutItem pulls (object id, slot, container GUID) from an InventoryPutItem event (26):
// key 0 = item object id, key 1 = slot index, key 2 = container GUID (matches
// AttachItemContainer key 1). The item is now in that container at that slot.
func PutItem(params map[byte]interface{}) (objID, slot int, containerGUID string, ok bool) {
	id, iok := toIntVal(params[0])
	g, gok := params[2].([]byte)
	if !iok || !gok {
		return 0, 0, "", false
	}
	s, sok := toIntVal(params[1])
	if !sok || s < 0 {
		s = -1 // slot unknown — callers tolerate
	}
	return id, s, hex.EncodeToString(g), true
}

// DeleteItem pulls the removed object id from an InventoryDeleteItem event (27):
// key 0 = item object id (which container it left is found by id).
func DeleteItem(params map[byte]interface{}) (objID int, ok bool) {
	return toIntVal(params[0])
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

// OwnInventory pulls the player's BAG slot object ids from the Join own-state
// response (op-2): key 55 = []i32 slot object ids (0 = empty). This is the login
// baseline inventory — NOT re-sent as an AttachItemContainer on bank/bag open, so it
// must be read here. (Key 52 is the WORN/equipped set, verified live: it held one
// mainhand while key 55 held two two-handers — impossible if equipped.) Objects are
// declared by New*Item events (resolved via the object registry). Live 2026-07-01.
func OwnInventory(params map[byte]interface{}) ([]int, bool) {
	return slotObjIDs(params, 55)
}

// OwnEquipped pulls the player's WORN/equipped slot object ids: own-state key 52.
func OwnEquipped(params map[byte]interface{}) ([]int, bool) {
	return slotObjIDs(params, 52)
}

// OwnInventorySlots pulls the bag as a SLOT-INDEXED array (own-state key 55): empties
// stay as 0 IN PLACE, because the player's move requests address bag items by slot
// index — compacting (like OwnInventory does for the snapshot view) would misalign
// every live move (feature 008). Hostile-size arrays are rejected (maxWireSlots).
func OwnInventorySlots(params map[byte]interface{}) ([]int, bool) {
	arr, ok := intSlice(params[55])
	if !ok || len(arr) > maxWireSlots {
		return nil, false
	}
	for i, v := range arr {
		if v <= 0 {
			arr[i] = 0
		}
	}
	return arr, true
}

func slotObjIDs(params map[byte]interface{}, key byte) ([]int, bool) {
	arr, ok := intSlice(params[key])
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(arr))
	for _, v := range arr {
		if v > 0 {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

// (Removed MasteryLevels: own-state key 55 is the BAG, not mastery levels — the real
// spec/mastery source is not yet identified. See protocol-findings.md open follow-ups.)

// SelfIdentity pulls the local player's own object id + name from the Join own-state
// response (op-2): key 0 = userObjectId, key 2 = playerName. Same response that carries
// the login bag (key 55) and city (key 8). Used to attribute own earnings — silver/
// harvest by object id, loot by name (feature 005). Own-state only, ToS-safe.
func SelfIdentity(params map[byte]interface{}) (objID int, name string, ok bool) {
	objID, iok := toIntVal(params[0])
	name, nok := params[2].(string)
	name = strings.TrimSpace(name)
	if !iok && !nok {
		return 0, "", false
	}
	return objID, name, true
}

// SelfContainers pulls the player's own container GUIDs from the Join own-state
// response: key 54 = the BAG container GUID (live-confirmed 3× — E:26 key2, op-30
// key4 and op-36 key0 all reference it), key 51 = the equipped-set GUID CANDIDATE
// (unconfirmed; a wrong candidate is harmless — see 008 research Decision 4). Either
// may be absent; ok=true when at least the bag GUID is present.
func SelfContainers(params map[byte]interface{}) (bagGUID, equippedGUID string, ok bool) {
	if g, gok := params[54].([]byte); gok && len(g) > 0 {
		bagGUID = hex.EncodeToString(g)
	}
	if g, gok := params[51].([]byte); gok && len(g) > 0 {
		equippedGUID = hex.EncodeToString(g)
	}
	return bagGUID, equippedGUID, bagGUID != ""
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
