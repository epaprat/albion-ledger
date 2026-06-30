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
	Item      Item      `json:"item"`
	Valuation Valuation `json:"valuation"` // per-item value + source + stale
	Location  Location  `json:"location"`
	Group     string    `json:"group"` // display group, e.g. "Inventory" or "Bank · Items"
	Count     int       `json:"count"`
	LastSeen  int64     `json:"lastSeen"`
}

// SectionState is the seen/stale status of one holdings location.
type SectionState struct {
	Seen     bool  `json:"seen"`
	LastSeen int64 `json:"lastSeen"`
	Stale    bool  `json:"stale"`
}

// HoldingsSummary is the rolled-up holdings state.
type HoldingsSummary struct {
	TotalValue    int64                     `json:"totalValue"`    // sum of known item values
	UnvaluedCount int                       `json:"unvaluedCount"` // held items with no known value yet
	Sections      map[Location]SectionState `json:"sections"`
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
