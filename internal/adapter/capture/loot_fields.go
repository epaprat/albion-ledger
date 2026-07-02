package capture

import "encoding/hex"

// Loot-correlation field extractors (feature 007). Pure functions over decoded Photon
// params; ok-returning, never panic (Principle IV). Key maps confirmed against the
// ADAvalonia reference (specs/007 research); request op codes live-verified separately.

// LootSource pulls a lootable-object announcement: NewLoot(98) key0=objId key3=srcName;
// NewLootChest(393) key0=objId key3/key4=names; LootChestOpened(395) key0 only.
func LootSource(params map[byte]interface{}, code int) (objID int, name string, ok bool) {
	objID, iok := toIntVal(params[0])
	if !iok || objID <= 0 {
		return 0, "", false
	}
	if s, sok := params[3].(string); sok && s != "" {
		name = s
	} else if code == 393 {
		if s, sok := params[4].(string); sok {
			name = s
		}
	}
	return objID, name, true
}

// ContainerSlots pulls the loot-correlation view of an AttachItemContainer event (99):
// key 0 = source object id (links the container to a lootable announcement), key 1 =
// container GUID, key 3 = slot-indexed item object ids WITH empties preserved (0/-1
// slots stay in place — the move request addresses items BY SLOT INDEX, so positions
// must not be compacted like holdings' ContainerItems does).
func ContainerSlots(params map[byte]interface{}) (containerGUID string, srcObjID int, slots []int, ok bool) {
	arr, isArr := intSlice(params[3])
	if !isArr {
		return "", 0, nil, false
	}
	g, gok := params[1].([]byte)
	if !gok {
		return "", 0, nil, false
	}
	srcObjID, _ = toIntVal(params[0])
	slots = make([]int, len(arr))
	for i, v := range arr {
		if v > 0 {
			slots[i] = v
		}
	}
	return hex.EncodeToString(g), srcObjID, slots, true
}

// MoveItem pulls the player's own single-item move request (op-30): key 0 = source
// slot, key 1 = source container GUID. (Destination keys 3/4 are not needed — loot is
// decided by the SOURCE container.)
func MoveItem(params map[byte]interface{}) (srcContainerGUID string, srcSlot int, ok bool) {
	slot, sok := toIntVal(params[0])
	g, gok := params[1].([]byte)
	if !sok || !gok || slot < 0 {
		return "", 0, false
	}
	return hex.EncodeToString(g), slot, true
}

// MoveGivenItems pulls the "take all" move request (op-39): key 0 = source container
// GUID, key 4 = item object ids.
func MoveGivenItems(params map[byte]interface{}) (srcContainerGUID string, itemObjIDs []int, ok bool) {
	g, gok := params[0].([]byte)
	ids, iok := intSlice(params[4])
	if !gok || !iok || len(ids) == 0 {
		return "", nil, false
	}
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return "", nil, false
	}
	return hex.EncodeToString(g), out, true
}
