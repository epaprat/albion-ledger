package capture

// Flow (earnings) field extractors for feature 005: silver / loot / gather / fame.
// Pure functions over decoded Photon params; tolerant of missing/odd keys, never
// panic (Principle IV). Codes + keys confirmed against the ADAvalonia reference
// client (see specs/005-activity-flow/research.md). Silver & fame arrive as
// fixed-point integers scaled ×10000.

// silverScale is the fixed-point divisor for silver amounts (ToFixedPointDouble).
const silverScale = 10000

// SilverEvent pulls the recipient + NET silver from a TakeSilver event (62): key 0 =
// the RECEIVING player object id (self for own income — live-verified 2026-07-01:
// obj==self across a session), key 2 = target entity (the killed mob, varies), key 3
// = yield pre-tax, key 5 = guild tax, key 6 = cluster tax (all fixed-point ×10000).
// Net = yield − guild − cluster (the game computes net, not sent). The caller self-
// filters on objID (key 0). (key 2 returned only for debug/clarity.)
func SilverEvent(params map[byte]interface{}) (mobEntity, objID int, netSilver int64, ok bool) {
	obj, ook := toIntVal(params[0])
	target, _ := toIntVal(params[2])
	yield, yok := toIntVal(params[3])
	if !ook || !yok {
		return 0, 0, 0, false
	}
	guild, _ := toIntVal(params[5])
	cluster, _ := toIntVal(params[6])
	net := int64(yield-guild-cluster) / silverScale
	return target, obj, net, true
}

// LootGrab pulls one looted item from an OtherGrabbedLoot event (279): key 2 =
// looter player name (self-filter upstream), key 3 = isSilver flag, key 4 = item
// id (catalog index), key 5 = amount. Silver loot (isSilver) is not an item and is
// ignored by the caller. (NewLoot 98 announces a lootable object with no items.)
func LootGrab(params map[byte]interface{}) (looter string, isSilver bool, itemID, amount int, ok bool) {
	looter, _ = params[2].(string)
	if b, bok := params[3].(bool); bok {
		isSilver = b
	}
	id, iok := toIntVal(params[4])
	if !iok {
		return "", isSilver, 0, 0, false
	}
	amt, aok := toIntVal(params[5])
	if !aok || amt < 1 {
		amt = 1
	}
	return looter, isSilver, id, amt, true
}

// HarvestEvent pulls a gathered item from a HarvestFinished event (61): key 0 =
// gatherer object id (self-filter upstream), key 4 = item id, total amount = key 5
// (standard) + key 6 (gathering bonus) + key 7 (premium bonus).
func HarvestEvent(params map[byte]interface{}) (gathererObjID, itemID, amount int, ok bool) {
	gatherer, gok := toIntVal(params[0])
	id, iok := toIntVal(params[4])
	if !gok || !iok {
		return 0, 0, 0, false
	}
	std, _ := toIntVal(params[5])
	bonus, _ := toIntVal(params[6])
	premium, _ := toIntVal(params[7])
	amount = std + bonus + premium
	if amount < 1 {
		amount = 1
	}
	return gatherer, id, amount, true
}

// RewardEvent pulls a reward/expedition/fishing item from a RewardGranted event
// (267): key 1 = item id, key 3 = quantity. (No self-filter needed — reward is own.)
func RewardEvent(params map[byte]interface{}) (itemID, qty int, ok bool) {
	id, iok := toIntVal(params[1])
	if !iok {
		return 0, 0, false
	}
	q, qok := toIntVal(params[3])
	if !qok || q < 1 {
		q = 1
	}
	return id, q, true
}

// FameEvent pulls fame gained from an UpdateFame event (82): key 2 =
// FameWithZoneMultiplier (core gain), key 5 = premium flag (+50%), key 10 = satchel
// fame — all fixed-point ×10000. Fame is inherently the local player's (no filter).
//
// DEVIATION: the reference client's TotalGainedFame also multiplies by a BonusFactor
// (1 + key 17, a wire double we don't decode here); that factor is omitted, so fame
// is slightly under-counted when a bonus is active. Base + premium + satchel is a
// faithful approximation (see research Decision 4 / tasks T027).
func FameEvent(params map[byte]interface{}) (fame int64, ok bool) {
	base, bok := toIntVal(params[2])
	if !bok {
		return 0, false
	}
	total := base
	if p, pok := params[5].(bool); pok && p {
		total += base / 2 // premium bonus ≈ +50%
	}
	if satchel, sok := toIntVal(params[10]); sok {
		total += satchel
	}
	return int64(total) / silverScale, true
}
