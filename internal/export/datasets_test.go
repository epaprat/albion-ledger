package export

// Per-dataset goldens (contract §2): headers are LITERAL contract strings and a
// realistic view-model sample renders to exact rows. Empty slices yield only
// the header.

import (
	"reflect"
	"testing"
	"time"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

// ts targets 2026-07-06 15:30:00 local in epoch ms so the golden strings match.
var ts = time.Date(2026, 7, 6, 15, 30, 0, 0, time.Local).UnixMilli()

func TestHoldingsGolden(t *testing.T) {
	header, rows := HoldingsRows([]model.HoldingItem{{
		Item:      model.Item{DisplayName: "Scholar Robe", UniqueName: "T4_ARMOR_CLOTH_SET1", Tier: 4, Enchant: 1, Quality: 2},
		Valuation: model.Valuation{Amount: 12500, Source: model.SourceLiveMarket, Stale: true},
		City:      "Lymhurst", Group: "Tab 1", Count: 3, LastSeen: ts,
	}})
	wantHeader := []string{"city", "group", "item", "uniqueName", "tier", "enchant", "quality",
		"count", "unitValue", "totalValue", "valueSource", "stale", "lastSeen"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Fatalf("holdings header contract broken: %v", header)
	}
	want := []string{"Lymhurst", "Tab 1", "Scholar Robe", "T4_ARMOR_CLOTH_SET1", "4", "1", "2",
		"3", "12500", "37500", "live_market", "true", "2026-07-06 15:30:00"}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("holdings row wrong:\n got %v\nwant %v", rows[0], want)
	}
}

func TestHoldingsUnknownValueEmpty(t *testing.T) {
	_, rows := HoldingsRows([]model.HoldingItem{{Item: model.Item{DisplayName: "X"}, Count: 2}})
	if rows[0][8] != "" || rows[0][9] != "" {
		t.Fatalf("unknown value must be empty cells, got unit=%q total=%q", rows[0][8], rows[0][9])
	}
}

func TestFlowGolden(t *testing.T) {
	header, rows := FlowRows([]model.FlowEventView{{
		Kind: model.FlowLoot, TS: ts, ItemDisplayName: "Bear Paws", UniqueName: "T5_2H_DUALAXE_KEEPER",
		Tier: 5, Enchant: 0, Quality: 1, Count: 1, Silver: 0, Fame: 250, Valued: true,
		Source: "Speckschwarte", Zone: "Lymhurst",
	}})
	wantHeader := []string{"time", "kind", "item", "uniqueName", "tier", "enchant", "quality",
		"count", "silver", "fame", "valued", "source", "zone"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Fatalf("flow header contract broken: %v", header)
	}
	want := []string{"2026-07-06 15:30:00", "loot", "Bear Paws", "T5_2H_DUALAXE_KEEPER",
		"5", "0", "1", "1", "0", "250", "true", "Speckschwarte", "Lymhurst"}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("flow row wrong:\n got %v\nwant %v", rows[0], want)
	}
}

func TestZoneGolden(t *testing.T) {
	header, rows := ZoneRows([]model.ZoneStatView{{
		Zone: "Deadvein Gully", ActiveMS: 90_000, NetSilver: 15000, SilverPerHour: 600000,
		GatherValue: 2000, GatherPerHour: 80000, Fame: 12345, FamePerHour: 493800,
		EventCount: 42, InsufficientData: false,
	}})
	wantHeader := []string{"zone", "activeMinutes", "netSilver", "silverPerHour", "gatherValue",
		"gatherPerHour", "fame", "famePerHour", "events", "insufficientData"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Fatalf("zones header contract broken: %v", header)
	}
	want := []string{"Deadvein Gully", "1.5", "15000", "600000", "2000", "80000",
		"12345", "493800", "42", "false"}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("zone row wrong:\n got %v\nwant %v", rows[0], want)
	}
}

func TestMarketGolden(t *testing.T) {
	header, rows := MarketRows([]model.LiveViewItem{{
		Item:      model.Item{DisplayName: "Dagger Pair", UniqueName: "T6_2H_DUALDAGGER", Tier: 6, Quality: 3},
		Valuation: model.Valuation{Amount: 98765, Source: model.SourceServerEstimate},
		Count:     2, LastSeen: ts,
	}})
	wantHeader := []string{"item", "uniqueName", "tier", "enchant", "quality", "count",
		"value", "valueSource", "stale", "lastSeen"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Fatalf("market header contract broken: %v", header)
	}
	want := []string{"Dagger Pair", "T6_2H_DUALDAGGER", "6", "0", "3", "2",
		"98765", "server_estimate", "false", "2026-07-06 15:30:00"}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("market row wrong:\n got %v\nwant %v", rows[0], want)
	}
}

func TestSpecGolden(t *testing.T) {
	header, rows := SpecRows([]model.MasteryLevel{{
		Index: 96, Name: "Dagger Pair", Category: "Combat", Subcategory: "Daggers",
		Slot: "Weapon", Base: false, Level: 100, Progress: 1, Fame: 0, FameToMax: 447000, Touched: true,
	}})
	wantHeader := []string{"id", "name", "category", "line", "slot", "base", "level",
		"progress", "fame", "fameToMax", "touched"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Fatalf("spec header contract broken: %v", header)
	}
	want := []string{"96", "Dagger Pair", "Combat", "Daggers", "Weapon", "false",
		"100", "1.00", "0", "447000", "true"}
	if !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("spec row wrong:\n got %v\nwant %v", rows[0], want)
	}
}

func TestEmptySlicesHeaderOnly(t *testing.T) {
	for name, n := range map[string]int{
		"holdings": len(mustRows(HoldingsRows(nil))),
		"flow":     len(mustRows(FlowRows(nil))),
		"zones":    len(mustRows(ZoneRows(nil))),
		"market":   len(mustRows(MarketRows(nil))),
		"spec":     len(mustRows(SpecRows(nil))),
	} {
		if n != 0 {
			t.Fatalf("%s: empty input must yield zero rows, got %d", name, n)
		}
	}
}

func mustRows(_ []string, rows [][]string) [][]string { return rows }

func TestTradeGolden(t *testing.T) {
	header, rows := TradeRows([]model.Trade{{
		TradeID: "mail:1", Direction: "sold", Source: "mail", ItemName: "Adept's Bag", ItemID: "T4_BAG@1",
		PartialAmount: 1, TotalAmount: 1, Gross: 8951, SetupFee: 0, SalesTax: 358, Net: 8593,
		TaxEstimated: true, UnitSilver: 8951, Received: 1_700_000_000_000, LocationID: "3005",
	}})
	wantHeader := []string{"time", "type", "source", "item", "uniqueName", "amount", "totalAmount",
		"gross", "setupFee", "salesTax", "net", "taxEstimated", "unitSilver", "location", "netEstimated"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Fatalf("trades header contract broken: %v", header)
	}
	if len(rows) != 1 || rows[0][1] != "sold" || rows[0][7] != "8951" || rows[0][10] != "8593" {
		t.Fatalf("trade row wrong: %v", rows[0])
	}
}
