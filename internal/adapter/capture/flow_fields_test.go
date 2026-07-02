package capture

import (
	"testing"

	"github.com/epaprat/albion-ledger/internal/photon"
)

func TestSelfIdentityFromBytes(t *testing.T) {
	// Join own-state response (op-2): key 0 = userObjectId, key 2 = playerName.
	params := decodeResponse(t, 2, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(12345)},
		{Key: 2, Type: photon.TypeString, Val: "Epaprat"},
	})
	objID, name, ok := SelfIdentity(params)
	if !ok || objID != 12345 || name != "Epaprat" {
		t.Fatalf("SelfIdentity → objID=%d name=%q ok=%v", objID, name, ok)
	}
}

func TestSilverEventNet(t *testing.T) {
	// TakeSilver(62): key2 target(self)=202404, key0 obj(pile)=999, yield 500000,
	// guild 10000, cluster 20000 (×10000) → net 47. Self-filter is on key2 (target).
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(999)},    // pile/mob obj
		{Key: 2, Type: photon.TypeInteger, Val: int32(202404)}, // TargetEntityId = receiving player
		{Key: 3, Type: photon.TypeInteger, Val: int32(500000)},
		{Key: 5, Type: photon.TypeInteger, Val: int32(10000)},
		{Key: 6, Type: photon.TypeInteger, Val: int32(20000)},
		{Key: 252, Type: photon.TypeShort, Val: int16(62)},
	})
	target, obj, net, ok := SilverEvent(params)
	if !ok || target != 202404 || obj != 999 || net != 47 {
		t.Fatalf("SilverEvent → target=%d obj=%d net=%d ok=%v, want 202404/999/47", target, obj, net, ok)
	}
}

func TestSilverEventNoTax(t *testing.T) {
	// No guild/cluster tax → net = yield/10000. key0 = self (receiving player).
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(202404)},
		{Key: 3, Type: photon.TypeInteger, Val: int32(1230000)},
		{Key: 252, Type: photon.TypeShort, Val: int16(62)},
	})
	_, obj, net, ok := SilverEvent(params)
	if !ok || obj != 202404 || net != 123 {
		t.Fatalf("SilverEvent no-tax → obj=%d net=%d ok=%v, want 202404/123", obj, net, ok)
	}
}

// TestSilverEventRealCapture encodes the exact live-capture shape (dungeon run
// 2026-07-01): key0 = self (receiving player, 168), key2 = the killed mob entity
// (277), yield 680000 ×10000 → net 68. Proves key0 (not key2) is the self-filter key
// and net is correct — the regression guard for the key0↔key2 silver fix.
func TestSilverEventRealCapture(t *testing.T) {
	const self = 168
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(self)}, // receiving player = self
		{Key: 2, Type: photon.TypeInteger, Val: int32(277)},  // killed mob entity (varies)
		{Key: 3, Type: photon.TypeInteger, Val: int32(680000)},
		{Key: 252, Type: photon.TypeShort, Val: int16(62)},
	})
	mob, obj, net, ok := SilverEvent(params)
	if !ok || obj != self {
		t.Fatalf("silver obj (self-filter key) = %d, want %d (key0==self)", obj, self)
	}
	if mob == self {
		t.Fatalf("key2 (%d) must be the mob, not self — filtering on it would drop all silver", mob)
	}
	if net != 68 {
		t.Fatalf("net = %d, want 68", net)
	}
}

func TestLootGrabFromBytes(t *testing.T) {
	// OtherGrabbedLoot(279): looter "Me", not silver, item 920 ×5.
	params := decodeEvent(t, []photon.Field{
		{Key: 2, Type: photon.TypeString, Val: "Me"},
		{Key: 3, Type: photon.TypeBoolean, Val: false},
		{Key: 4, Type: photon.TypeInteger, Val: int32(920)},
		{Key: 5, Type: photon.TypeInteger, Val: int32(5)},
		{Key: 252, Type: photon.TypeShort, Val: int16(279)},
	})
	looter, isSilver, itemID, amount, ok := LootGrab(params)
	if !ok || looter != "Me" || isSilver || itemID != 920 || amount != 5 {
		t.Fatalf("LootGrab → looter=%q isSilver=%v item=%d amt=%d ok=%v", looter, isSilver, itemID, amount, ok)
	}
}

func TestLootGrabSilverFlag(t *testing.T) {
	params := decodeEvent(t, []photon.Field{
		{Key: 2, Type: photon.TypeString, Val: "Me"},
		{Key: 3, Type: photon.TypeBoolean, Val: true},
		{Key: 4, Type: photon.TypeInteger, Val: int32(1)},
		{Key: 5, Type: photon.TypeInteger, Val: int32(1000)},
		{Key: 252, Type: photon.TypeShort, Val: int16(279)},
	})
	_, isSilver, _, _, ok := LootGrab(params)
	if !ok || !isSilver {
		t.Fatalf("LootGrab silver flag → isSilver=%v ok=%v, want true/true", isSilver, ok)
	}
}

func TestHarvestEventFromBytes(t *testing.T) {
	// HarvestFinished(61): gatherer 12345, item 700, amount 3+2+1=6.
	params := decodeEvent(t, []photon.Field{
		{Key: 0, Type: photon.TypeInteger, Val: int32(12345)},
		{Key: 4, Type: photon.TypeInteger, Val: int32(700)},
		{Key: 5, Type: photon.TypeInteger, Val: int32(3)},
		{Key: 6, Type: photon.TypeInteger, Val: int32(2)},
		{Key: 7, Type: photon.TypeInteger, Val: int32(1)},
		{Key: 252, Type: photon.TypeShort, Val: int16(61)},
	})
	g, item, amount, ok := HarvestEvent(params)
	if !ok || g != 12345 || item != 700 || amount != 6 {
		t.Fatalf("HarvestEvent → g=%d item=%d amt=%d ok=%v, want 12345/700/6", g, item, amount, ok)
	}
}

func TestRewardEventFromBytes(t *testing.T) {
	// RewardGranted(267): item 800, qty 4.
	params := decodeEvent(t, []photon.Field{
		{Key: 1, Type: photon.TypeInteger, Val: int32(800)},
		{Key: 3, Type: photon.TypeInteger, Val: int32(4)},
		{Key: 252, Type: photon.TypeShort, Val: int16(267)},
	})
	item, qty, ok := RewardEvent(params)
	if !ok || item != 800 || qty != 4 {
		t.Fatalf("RewardEvent → item=%d qty=%d ok=%v, want 800/4", item, qty, ok)
	}
}

func TestFameEventFromBytes(t *testing.T) {
	// UpdateFame(82): base 100000, premium true (+50%→150000), satchel 20000 → 170000 /10000 = 17.
	params := decodeEvent(t, []photon.Field{
		{Key: 2, Type: photon.TypeInteger, Val: int32(100000)},
		{Key: 5, Type: photon.TypeBoolean, Val: true},
		{Key: 10, Type: photon.TypeInteger, Val: int32(20000)},
		{Key: 252, Type: photon.TypeShort, Val: int16(82)},
	})
	fame, ok := FameEvent(params)
	if !ok || fame != 17 {
		t.Fatalf("FameEvent → fame=%d ok=%v, want 17", fame, ok)
	}
}
