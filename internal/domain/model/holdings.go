package model

// Location is where a held item is kept.
type Location string

const (
	LocInventory Location = "inventory"
	LocBank      Location = "bank"
	LocEquipped  Location = "equipped"
	LocHoldings  Location = "holdings" // generic, until a container is matched to inventory/bank
)

// HoldingItem is a held item with its value and where it is held.
type HoldingItem struct {
	ObjID     int       `json:"objId"` // in-world object id — a stable per-item key for the UI
	Item      Item      `json:"item"`
	Valuation Valuation `json:"valuation"` // per-item value + source + stale
	Location  Location  `json:"location"`
	City      string    `json:"city"`  // city display name; "" for inventory (FR-003/FR-005)
	Group     string    `json:"group"` // bank tab name, or "Inventory"
	Count     int       `json:"count"`
	LastSeen  int64     `json:"lastSeen"`
}

// SectionState is the seen/stale status of one holdings group.
type SectionState struct {
	Seen     bool  `json:"seen"`
	LastSeen int64 `json:"lastSeen"`
	Stale    bool  `json:"stale"`
}

// TabSummary rolls up one bank tab (or the inventory pseudo-tab).
type TabSummary struct {
	Name          string       `json:"name"`
	ItemCount     int          `json:"itemCount"`
	Subtotal      int64        `json:"subtotal"`      // sum of known values in this tab
	UnvaluedCount int          `json:"unvaluedCount"` // items with no known value yet
	Opened        bool         `json:"opened"`        // false = known (named) but contents not observed
	State         SectionState `json:"state"`
}

// CitySummary rolls up one city's bank (or the inventory group).
type CitySummary struct {
	Name          string       `json:"name"` // "Inventory" | city name | "Unknown city (<id>)"
	IsInventory   bool         `json:"isInventory"`
	Total         int64        `json:"total"`
	UnvaluedCount int          `json:"unvaluedCount"`
	Tabs          []TabSummary `json:"tabs"`
	State         SectionState `json:"state"`
	// VaultValue is the game-reported total value of this city's vault from the
	// K bank-overview (R:516 k5 ÷10000); 0 = not reported (frontend hides it).
	VaultValue int64 `json:"vaultValue"`
}

// HoldingsSummary is the rolled-up holdings state, nested city → tab.
type HoldingsSummary struct {
	TotalValue    int64         `json:"totalValue"`    // grand total of known values
	GameEstTotal  int64         `json:"gameEstTotal"`  // Σ game-reported vault values (K overview, 010)
	UnvaluedCount int           `json:"unvaluedCount"` // total held items with no known value
	Cities        []CitySummary `json:"cities"`        // inventory first, then cities by last-seen desc
}

// MasteryLevel is one character specialization level.
type MasteryLevel struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"` // resolved node name; "Node #<index>" fallback
	Level    int     `json:"level"`
	Progress float64 `json:"progress"` // fraction to next level [0,1] (011)
	Fame     int64   `json:"fame"`     // accumulated fame on this node (011)
	Category    string  `json:"category"`    // top breakdown, e.g. "Combat" (011)
	Subcategory string  `json:"subcategory"` // mid breakdown, e.g. "Axes" (011)
	Slot        string  `json:"slot"`        // gear slot, e.g. "Weapon"/"Head"/"Chest" (012)
	Base        bool    `json:"base"`        // whole-line "Fighter" aggregate node (012)
	Touched     bool    `json:"touched"`     // false = catalog node with no progress yet (011)
	FameToMax   int64   `json:"fameToMax"`   // total fame to level 100; 0 = unknown (011)
}

// SpecNodeCatalog is one node's static identity from the name catalog (011).
type SpecNodeCatalog struct {
	ID          int
	Name        string
	Category    string
	Subcategory string
	Slot        string
	Base        bool
	FameToMax   int64 // total fame from 0 to level 100 (011); 0 = unknown
}

// CharacterSpec is the player's Destiny Board (specialization) state.
type CharacterSpec struct {
	Masteries []MasteryLevel `json:"masteries"`
	NodeCount int            `json:"nodeCount"` // total nodes (011)
	TotalFame int64          `json:"totalFame"` // summed node fame (011)
	Complete  bool           `json:"complete"`  // true once the unlocked/maxed set (E:155) is known (011)
}
