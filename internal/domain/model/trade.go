package model

// Trade is one captured marketplace transaction (feature 017): an order-fill (from the
// in-game mail) or an instant/quicksell trade (reconstructed from the request + wallet
// delta). It is the itemized P&L flow that pairs with the net-worth snapshot (016).
//
// Full breakdown (all silver real, ÷10000 already):
//   - Gross     : pre-fee item value (mail total, or order unit price × amount).
//   - SetupFee   : order-listing fee paid when the order was placed (0 for instant).
//   - SalesTax   : market tax withheld on a sale (0 on a buy); Gross − Net where both known.
//   - Net        : what actually hit the wallet (+ for sold, − magnitude for bought).
//
// TaxEstimated marks a Net/SalesTax that was derived from a rate rather than an observed
// wallet delta (mail fills — the wallet changed at fill time, not when the mail is read).
//
// MailID is the natural key for mail trades; instant trades use a synthesized TradeID.
type Trade struct {
	TradeID       string  `json:"tradeId"`   // unique key (mail:<id> or inst:<seq>) — dedup
	Direction     string  `json:"direction"` // "sold" (income) | "bought" (expense)
	Source        string  `json:"source"`    // "mail" | "instant" | "quicksell"
	ItemID        string  `json:"itemId"`    // wire uniqueName (with @enchant), may be "" (unknown)
	ItemName      string  `json:"itemName"`  // catalog display name (read-time; falls back to ItemID)
	ItemIndex     int     `json:"itemIndex"` // catalog index when known (instant sell), else 0
	PartialAmount int     `json:"partialAmount"`
	TotalAmount   int     `json:"totalAmount"`
	Gross         int64   `json:"gross"`    // pre-fee value
	SetupFee      int64   `json:"setupFee"` // order listing fee (0 for instant)
	SalesTax      int64   `json:"salesTax"` // market tax on a sale (0 on a buy)
	Net           int64   `json:"net"`      // wallet delta actual (+sold / −bought)
	TaxEstimated  bool    `json:"taxEstimated"`
	UnitSilver    float64 `json:"unitSilver"`
	Received      int64   `json:"received"`   // opaque wire timestamp — ordering only, 0 = unknown
	LocationID    string  `json:"locationId"` // market location id (may be empty)
}

// TradeSummary is the read-time rollup over captured trades (Principle V — derived, not
// persisted). Each fee component is summed SEPARATELY so the player sees where silver
// went (FR-008). Net = income − expense (the actual wallet movement). Scope is an honest
// coverage label ("opened mails only" for the mail leg).
type TradeSummary struct {
	GrossIncome  int64  `json:"grossIncome"`  // Σ gross of sold
	GrossExpense int64  `json:"grossExpense"` // Σ gross of bought
	SalesTax     int64  `json:"salesTax"`     // Σ tax withheld
	SetupFee     int64  `json:"setupFee"`     // Σ order-listing fees
	Net          int64  `json:"net"`          // Σ net (income − expense)
	Count        int    `json:"count"`
	Scope        string `json:"scope"`
	WindowStart  int64  `json:"windowStart"` // earliest received-ms in scope (0 = all time); the ledger table filters to this so the hero and the rows always agree (018)
}

// MailInfo is a persisted mail-type record (017): the id→type map from GetMailInfos,
// kept across sessions so a later ReadMail can be decoded even when the game didn't
// re-send the (client-cached) list. Type is the raw wire string.
type MailInfo struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	LocationID string `json:"locationId"`
	Received   int64  `json:"received"`
}

// Trade directions and sources.
const (
	TradeSold        = "sold"
	TradeBought      = "bought"
	TradeSourceMail  = "mail"
	TradeSourceInst  = "instant"
	TradeSourceQuick = "quicksell"
	TradeSourceSetup = "setup"
)
