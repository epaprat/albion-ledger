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
	CatCharacterSpec:    {55},      // masteries/specialization array (own-state blob, key 55)
	CatLoot:             {0, 3},    // object id + source name
	CatGatherFishing:    {0, 1},    // harvestable/reward fields
	CatSilver:           {0, 2, 3}, // object id + target + yield (key 5 guild-tax often absent)
	CatFame:             {1, 2, 3}, // total/zone fame + multiplier
	CatItemValueEMV:     {0, 1},    // item id array + estimated value array
}

// FieldsExpected returns how many fields a category is expected to carry.
func FieldsExpected(c Category) int { return len(ExpectedFields[c]) }
