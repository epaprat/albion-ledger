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
}

// HoldingsSummary is the rolled-up holdings state, nested city → tab.
type HoldingsSummary struct {
	TotalValue    int64         `json:"totalValue"`    // grand total of known values
	UnvaluedCount int           `json:"unvaluedCount"` // total held items with no known value
	Cities        []CitySummary `json:"cities"`        // inventory first, then cities by last-seen desc
}

// MasteryLevel is one character specialization level.
type MasteryLevel struct {
	Index int    `json:"index"`
	Name  string `json:"name"` // best-effort; "Mastery #<index>" fallback
	Level int    `json:"level"`
}

// CharacterSpec is the player's specialization levels.
type CharacterSpec struct {
	Masteries []MasteryLevel `json:"masteries"`
}
