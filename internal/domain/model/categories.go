// Package model holds the probe's core domain types — target categories,
// observations, coverage, and verdicts. It has zero infrastructure dependencies.
package model

// Category is a ToS-safe target data category the probe measures.
type Category string

const (
	CatMarketSellOrders Category = "market_sell_orders"
	CatMarketBuyOrders  Category = "market_buy_orders"
	CatMarketHistory    Category = "market_history"
	CatGoldPrice        Category = "gold_price"
	CatInventory        Category = "inventory"
	CatEquipment        Category = "equipment"
	CatBank             Category = "bank"
	CatCharacterSpec    Category = "character_spec"
	CatLoot             Category = "loot"
	CatGatherFishing    Category = "gather_fishing"
	CatSilver           Category = "silver"
	CatFame             Category = "fame"
	CatItemValueEMV     Category = "item_value_emv"
	// CatCurrentLocation is the player's own current city/cluster (Join response key 8),
	// consumed by the holdings-by-location view (feature 004). It is NOT a probe coverage
	// target, so it is intentionally absent from AllCategories/ExpectedFields.
	CatCurrentLocation Category = "current_location"
	// CatInventoryPut / CatInventoryDelete are incremental item moves (events 26/27)
	// that keep holdings live without a full re-snapshot. Not probe coverage targets.
	CatInventoryPut    Category = "inventory_put"
	CatInventoryDelete Category = "inventory_delete"
	// CatLootSource marks lootable-object announcements (NewLoot 98, NewLootChest 393,
	// LootChestOpened 395) and CatLootMove the player's own outgoing item-move requests
	// (ops 30/39) — together they drive item-loot correlation (feature 007). Not probe
	// coverage targets.
	CatLootSource Category = "loot_source"
	CatLootMove   Category = "loot_move"
	// CatBankLocations/CatBankTabs/CatBankTabContent carry the K-key bank-overview
	// chain (R:516 locations+total values, R:517 tab guids+real names, R:1/518 tab
	// content summaries) — full bank sync without opening banks (feature 010). Not
	// probe coverage targets. NOTE: the content response (raw op byte 1) carries NO
	// key 253 — classification relies on the opByte fallback plus a strict shape
	// guard in the extractor (Principle IV double-lock).
	CatBankLocations  Category = "bank_locations"
	CatBankTabs       Category = "bank_tabs"
	CatBankTabContent Category = "bank_tab_content"
	// CatSpecSnapshot/CatSpecDelta/CatSpecDone carry the Destiny Board (achievement
	// family, feature 011): E:154 full self snapshot every Join, E:153 live progress
	// delta, E:152 node completion. Not probe coverage targets.
	CatSpecSnapshot Category = "spec_snapshot"
	CatSpecDelta    Category = "spec_delta"
	CatSpecDone     Category = "spec_done"
	// CatSpecFull = E:151 FullAchievementInfo (the FINISHED/maxed node list, login
	// burst). Layout not yet pinned — the handler logs its shape under -debugflow so
	// the next live capture reveals it, then marks those nodes maxed (011 follow-up).
	CatSpecFull Category = "spec_full"
)

// AllCategories is the full ordered set of target categories (13). The coverage
// report MUST include every one, even when never observed (FR-006/SC-002).
var AllCategories = []Category{
	CatMarketSellOrders, CatMarketBuyOrders, CatMarketHistory, CatGoldPrice,
	CatInventory, CatEquipment, CatBank, CatCharacterSpec, CatLoot,
	CatGatherFishing, CatSilver, CatFame, CatItemValueEMV,
}

// ExpectedFields maps each category to the parameter keys we attempt to extract.
// Field-completeness = present / len(ExpectedFields[cat]). Values are the Photon
// parameter indices each category's message is expected to carry.
var ExpectedFields = map[Category][]byte{
	CatMarketSellOrders: {0},       // string-array of serialized orders (params[0])
	CatMarketBuyOrders:  {0},       // string-array of serialized orders
	CatMarketHistory:    {0, 1},    // item id + history points
	CatGoldPrice:        {0, 1},    // prices + timestamps
	CatInventory:        {0, 2},    // container id + slot items
	CatEquipment:        {0, 1},    // object id + item id
	CatBank:             {0, 1, 5}, // vault id + container ids/names + counts
	CatCharacterSpec:    {55},      // own-state discriminator (key 55 present = op-2 own-state; it holds the bag object ids, not masteries)
	// Flow categories list only ALWAYS-PRESENT keys (live-verified 2026-07-01) — optional
	// keys (taxes, premium, satchel) would crater completeness and raise false drift alarms.
	CatLoot:             {0, 3}, // OtherGrabbedLoot(279): objId + isSilver (98 now maps to loot_source)
	CatGatherFishing:    {3},    // only key present in BOTH layouts: 61 (node objId) and 267 (quantity)
	CatSilver:           {0, 3}, // TakeSilver(62): receiving player + yield (taxes 5/6 often absent)
	CatFame:             {2},    // UpdateFame(82): zone-mult fame gain (premium/satchel/bonus optional)
	CatLootSource:       {0},    // lootable object id (98/393/395 all carry key 0)
	CatLootMove:         {0},    // key 0 present in both layouts (op-30: src slot, op-39: src guid)
	CatItemValueEMV:     {0, 1},    // item id array + estimated value array
}

// FieldsExpected returns how many fields a category is expected to carry.
func FieldsExpected(c Category) int { return len(ExpectedFields[c]) }
