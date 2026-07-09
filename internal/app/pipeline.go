// Package app owns the capture-time pipeline: parser callbacks → classification →
// per-category handlers → the UI/persistence sink. Before feature 009 this glue lived
// as package-level globals and a monolithic switch in cmd/albion-ledger — every
// feature grew that file and nothing was testable without the entrypoint
// (ADR-022/024/025, Constitution Principle II).
//
// Concurrency model (unchanged from pre-009): the whole Pipeline runs on the single
// capture goroutine. objMu exists only because resolveObjects/registerNewItem also
// serialize the object registry against the pending queues — the same discipline the
// globals had.
package app

import (
	"fmt"
	"log"
	"math"
	"sort"
	"sync"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/holdings"
	"github.com/epaprat/albion-ledger/internal/locations"
	"github.com/epaprat/albion-ledger/internal/loot"
	"github.com/epaprat/albion-ledger/internal/pending"
	"github.com/epaprat/albion-ledger/internal/specboard"
	"github.com/epaprat/albion-ledger/internal/specenum"
	"github.com/epaprat/albion-ledger/internal/tradecalc"
)

// Fixed container ids for the player's own bag + equipped sets, which arrive in
// own-state slot arrays without a wire GUID. They group under the inventory city as
// separate tabs.
const (
	SelfBagGUID   = "self-bag"
	SelfEquipGUID = "self-equipped"
)

// silverScale: every silver amount on the wire is fixed-point ×10000 (EMV, vault
// totals, market unit prices — one constant, one truth).
const silverScale = 10000

// emvScale: legacy alias for declaration EMV scaling.
const emvScale = silverScale

const objRegCap = 50_000

// maxBagSlots bounds the live bag slot map. The largest real bag is well under 128
// slots; E:26 (key 1) and op-30 destination (key 3) slots come off the wire with no
// upper bound of their own, and an unbounded pad loop here would let a single
// hostile/corrupt packet allocate gigabytes (Principle IV/XI).
const maxBagSlots = 256

// Sink is the narrow consumer-side view of the UI/persistence service — only the
// methods the pipeline actually calls. The wails adapter satisfies it; handlers stay
// testable against any implementation (Principle I: app depends on the interface).
type Sink interface {
	SetSelf(objID int, name string)
	SetZone(zone string)
	SetCurrentCity(city string)
	IngestEMV(index, quality int, value, asOf int64)
	IngestContainer(containerGUID, ownerGUID string, slots []holdings.SlotItem)
	IngestSelfContainer(containerGUID, tab string, slots []holdings.SlotItem)
	IngestPutItem(containerGUID string, objID int, ref holdings.ItemRef) bool
	IngestDeleteItem(objID int)
	IngestBankVault(owners, tabNames []string)
	IngestVaultSummaryTab(tabGUID, city, tabName string, rows []holdings.ItemRef)
	IngestCityVaultValues(values map[string]int64)
	IngestMarketPrice(uniqueName string, quality int, silver int64)
	IngestSilver(id string, net int64, ts int64, source string)
	IngestLoot(id string, index, quality, count int, ts int64, source string)
	IngestGather(id string, index, quality, count int, ts int64, source string)
	IngestFame(id string, fame int64, ts int64)
	SetSpec(spec model.CharacterSpec)
	SetSpecUnlocked(ids []int)
	SetSpecEnum(ids []int)
	SetWallet(silver int64, ts int64)
	AddTrade(t model.Trade)
	SaveMailInfo(id int64, typ, location string, received int64)
}

// SpecResolver maps a Destiny Board node id to a readable name + category (011).
type SpecResolver interface {
	Resolve(id int) (name, category, subcategory string, ok bool)
	All() []model.SpecNodeCatalog
}

// Pipeline holds every piece of capture-time state that used to be a package global
// in cmd/albion-ledger. One instance per capture session, driven by the parser
// callbacks (OnRequest/OnResponse/OnEvent).
type Pipeline struct {
	sink  Sink
	clf   *probe.Classifier
	locs  *locations.Locations // cluster-id → zone-name resolver (nil = raw ids)
	nowMS func() int64
	debug bool // -debugflow: log flow attribution to stderr

	// self identity for own-earning attribution (005); set on every Join op-2.
	selfObjID int
	selfName  string
	flowSeq   int64 // monotonic nonce for flow events lacking a natural unique wire id

	// lootTracker correlates own move requests with loot sources (007).
	lootTracker *loot.Tracker

	// objReg maps in-world object ids to their item type+quality (New*Item, codes
	// 30-37); container slots reference object ids. objMu also serializes the pending
	// queues against the registry (see package comment).
	objMu    sync.Mutex
	objReg   map[int]holdings.ItemRef
	objOrder []int

	// pending queues (internal/pending: cap+TTL+counted drops; loss logs stay here).
	pendingInv             *pending.Map[string] // own-state slot → its self container (no TTL)
	pendingLootResolve     *pending.Map[string] // loot hit → source name
	pendingPuts            *pending.Map[string] // container put → target container
	lastPendingLootDropLog int64
	lastPendingPutDropLog  int64

	// Holdings freshness glue (008): own-container GUID bridge + live bag slot map.
	selfContainerGUIDs map[string]string
	bagSlots           []int

	// K bank-overview bridges (010): vault guid → city, tab guid → (city, name).
	// vaultCity is REBUILT per R:516 (full list); tabMeta upserts, capped.
	vaultCity map[string]string
	tabMeta   map[string]tabInfo
	// lastBankTabGUID: tab guid from the most recent op-518 request, so the default/open
	// tab's GUID-LESS content response can be attributed to it (010 fix, 2026-07-08).
	lastBankTabGUID string

	// Destiny Board (011): live skill-tree state + node-name resolver. The snapshot
	// arrives as several E:154 packets per Join; specReplacePending (armed on every
	// op-2 Join) makes the FIRST packet of a burst clear and the rest merge.
	board              *specboard.Board
	specNames          SpecResolver
	specReplacePending bool
	specUnlocked       map[int]bool // E:155 full unlocked set; ids not in board = maxed (011)
	specUnlockedSeen   bool         // true once E:155 (or a persisted seed) is known — data is complete
	specSnapshotSeen   bool         // true once an E:154 burst arrived — REQUIRED before maxed
	//                                 classification (unlocked ∖ empty-board would mark ALL 100)
	specEnum      *specenum.Enum // E:1 board enumeration (012): position→id for warm logins
	specFullBoard bool           // true once E:1 gave the complete board — it's the authority

	// Mail P&L correlation (017): R:174 GetMailInfos populates id→{type,location,received};
	// R:176 ReadMail looks the type up to parse the body. Bounded (oldest evicted) so a
	// large mailbox can't grow it without limit (Principle XI). Session-transient — the
	// resulting Trade is what persists, not this cache.
	mailInfo      map[int64]mailInfoEntry
	mailInfoOrder []int64

	// Instant-trade wallet correlation (017 expansion): an instant sell/buy/quicksell
	// request (op 315/83/485) carries the item+amount but NOT the silver; the silver is
	// the wallet delta (E:81) that follows. armInstant records the pending context;
	// correlateWallet attributes the next matching delta(s) to it (within a window). A
	// wallet change with no fresh trade context (loot, repair) is ignored for trades.
	walletSilver int64 // last observed wallet balance (real silver), for delta
	walletSeen   bool
	instant      *instantCtx
	instantSeq   int64

	// offerCache maps a marketplace order id → its item name + unit price + side (from
	// R:81/82), so an instant buy (op 83, order id only) can name its item AND an instant
	// sell/buy can build an EXPECTED value (price × amount) to gate the wallet delta (018).
	// Both offers (sell) and requests (buy) are cached; valuation is still fed offers only.
	// Bounded.
	offerCache map[int64]orderInfo
	offerOrder []int64
}

// orderInfo is one cached marketplace order: item, unit price (×10000), and side (018).
type orderInfo struct {
	name    string
	unitRaw int64
	isOffer bool // true = sell offer, false = buy request
}

// offerCacheCap bounds the order-id→info cache (Principle XI).
const offerCacheCap = 8192

// instantCtx is a pending instant trade awaiting its wallet delta(s) (017).
type instantCtx struct {
	direction string // sold | bought
	source    string // instant | quicksell
	tradeID   string
	itemIndex int    // instant sell (item type index); 0 otherwise
	itemID    string // resolved uniqueName, else ""
	amount    int    // units traded
	count     int    // items in a quicksell batch
	net       int64  // accumulated wallet delta (signed: + sold, − bought)
	single    bool   // true = one delta then disarm (instant sell/buy); false = accumulate a quicksell burst
	deltas    int    // wallet deltas attributed so far (a quicksell disarms at count)
	expiresMS int64  // correlation window end
	// Expected-value correlation (018): when the order unit price is known, expectedGross =
	// unit price × amount lets a wallet delta be gated on MAGNITUDE, not just sign — so
	// unrelated income (loot, escrow, refund) in the window is not mis-attributed. Only the
	// single instant buy/sell path uses it; quicksell has no per-order quantity on the wire.
	expectedGross int64 // 0 when unknown
	expectedKnown bool  // false → fall back to 017 sign-only correlation (no regression)
}

// instantWindowMS bounds how long after a trade request a wallet delta may be attributed
// to it — long enough for a quicksell burst, short enough to exclude unrelated income.
const instantWindowMS = 2000

// Expected-value tolerance (018, research D3). A buy pays the order price EXACTLY, so its
// delta may differ only by rounding. A sell's net is the price minus an unknown (premium-
// dependent) tax, so the true net is bracketed in [gross×(1−maxTax), gross]. Both bands are
// far tighter than sign-only: unrelated income almost never lands inside them.
const (
	instantBuyEpsilonRate = 0.02  // buy: |delta| within ±2% of expected spend (rounding)
	instantSellMaxTaxRate = 0.08  // sell: net floor = gross×(1−0.08) (base 4% + safety margin)
	instantSellUpperPad   = 0.005 // sell: a sale net can't exceed gross; only a small pad for our own rounding
)

// mailInfoEntry is one GetMailInfos row cached for a later ReadMail (017).
type mailInfoEntry struct {
	typ      string
	location string
	received int64
}

// mailInfoCap bounds the id→info correlation cache (Principle XI).
const mailInfoCap = 4096

// New wires a Pipeline. locs may be nil (zones stay raw cluster ids).
func New(sink Sink, clf *probe.Classifier, locs *locations.Locations, specNames SpecResolver, nowMS func() int64, debug bool) *Pipeline {
	return &Pipeline{
		sink:               sink,
		clf:                clf,
		locs:               locs,
		nowMS:              nowMS,
		debug:              debug,
		lootTracker:        loot.New(),
		objReg:             map[int]holdings.ItemRef{},
		pendingInv:         pending.New[string](1024, 0),
		pendingLootResolve: pending.New[string](256, 10_000),
		pendingPuts:        pending.New[string](256, 10_000),
		selfContainerGUIDs: map[string]string{},
		vaultCity:          map[string]string{},
		tabMeta:            map[string]tabInfo{},
		board:              specboard.New(),
		specNames:          specNames,
		specEnum:           specenum.New(),
		mailInfo:           map[int64]mailInfoEntry{},
		offerCache:         map[int64]orderInfo{},
	}
}

// putOrder caches an order id → item name + unit price + side (bounded, oldest evicted).
// Re-inserting a known id refreshes it (newest-wins price) without growing the order list.
func (p *Pipeline) putOrder(id int64, uniqueName string, unitRaw int64, isOffer bool) {
	if id <= 0 || uniqueName == "" || unitRaw <= 0 {
		return
	}
	if _, exists := p.offerCache[id]; !exists {
		if len(p.offerCache) >= offerCacheCap && len(p.offerOrder) > 0 {
			delete(p.offerCache, p.offerOrder[0])
			p.offerOrder = p.offerOrder[1:]
		}
		p.offerOrder = append(p.offerOrder, id)
	}
	p.offerCache[id] = orderInfo{name: uniqueName, unitRaw: unitRaw, isOffer: isOffer}
}

// orderInfoFor returns the cached order for an id (false if never browsed).
func (p *Pipeline) orderInfoFor(id int64) (orderInfo, bool) {
	oi, ok := p.offerCache[id]
	return oi, ok
}

// orderValue returns the expected gross (unit price × amount, real silver) for a cached
// order and whether the price was known — the reference for the delta bracket (018).
func (p *Pipeline) orderValue(orderID int64, amount int) (int64, bool) {
	oi, ok := p.offerCache[orderID]
	if !ok || oi.unitRaw <= 0 || amount <= 0 {
		return 0, false
	}
	// Overflow guard: amount comes straight off the wire (unbounded); a corrupt/hostile
	// amount × a high unit price would wrap int64 before the divide → a garbage bracket.
	if oi.unitRaw > math.MaxInt64/int64(amount) {
		return 0, false
	}
	return int64(amount) * oi.unitRaw / silverScale, true
}

// putMailInfo caches a GetMailInfos row for a later ReadMail, evicting the oldest entry
// when the cache is full (bounded, Principle XI). Re-inserting a known id refreshes it
// without growing the order list.
func (p *Pipeline) putMailInfo(id int64, e mailInfoEntry) {
	if _, exists := p.mailInfo[id]; !exists {
		if len(p.mailInfo) >= mailInfoCap && len(p.mailInfoOrder) > 0 {
			delete(p.mailInfo, p.mailInfoOrder[0])
			p.mailInfoOrder = p.mailInfoOrder[1:]
		}
		p.mailInfoOrder = append(p.mailInfoOrder, id)
	}
	p.mailInfo[id] = e
}

// getMailInfo returns the cached info for a mail id (false if its GetMailInfos row was
// never seen — the ReadMail is then dropped, the honest passive limit, FR-004).
func (p *Pipeline) getMailInfo(id int64) (mailInfoEntry, bool) {
	e, ok := p.mailInfo[id]
	return e, ok
}

// SeedMailInfos restores the persisted mail-type map at startup so a mail whose
// GetMailInfos list the game client-cached (and never re-sent) can still be decoded (017).
func (p *Pipeline) SeedMailInfos(infos []model.MailInfo) {
	for _, mi := range infos {
		p.putMailInfo(mi.ID, mailInfoEntry{typ: mi.Type, location: mi.LocationID, received: mi.Received})
	}
}

// armInstant records a pending instant trade so the following wallet delta(s) attribute
// to it. Replaces any prior context (the previous trade's deltas have already been
// applied incrementally, so nothing is lost).
func (p *Pipeline) armInstant(c *instantCtx) {
	p.instantSeq++
	prefix := "inst"
	if c.source == model.TradeSourceQuick {
		prefix = "quick"
	}
	// Id includes the wall-clock time so a per-run counter can't collide with a persisted
	// trade after a restart (the counter resets, the clock doesn't).
	c.tradeID = fmt.Sprintf("%s:%d-%d", prefix, p.nowMS(), p.instantSeq)
	c.expiresMS = p.nowMS() + instantWindowMS
	p.instant = c
	if p.debug {
		log.Printf("[trade] armed %s %s item=%d amount=%d count=%d", c.source, c.direction, c.itemIndex, c.amount, c.count)
	}
}

// correlateWallet processes a new wallet balance: it attributes the delta to a fresh
// instant-trade context (right sign, within window) and emits/updates that trade, then
// records the balance for the next delta. A delta with no matching context is ignored
// for trades (it's other income — loot, repair refund). When the order price is known,
// the delta is also gated on MAGNITUDE (expected-value bracket, 018) so unrelated income
// in the window is not mis-attributed; when it is not, the 017 sign-only path applies.
func (p *Pipeline) correlateWallet(silver int64) {
	now := p.nowMS()
	// Drop a context whose window elapsed without a clean match — an unverified guess is
	// worse than an honest omission, so nothing is fabricated.
	if p.instant != nil && now > p.instant.expiresMS {
		p.instant = nil
	}
	if p.walletSeen && p.instant != nil && now <= p.instant.expiresMS {
		delta := silver - p.walletSilver
		sold := p.instant.direction == model.TradeSold
		if (sold && delta > 0) || (!sold && delta < 0) {
			p.attributeDelta(delta, now)
		}
	}
	p.walletSilver = silver
	p.walletSeen = true
}

// attributeDelta applies one correct-sign wallet delta to the pending instant context. With
// a known order price the delta must fall in the expected-value bracket to BE the trade; an
// out-of-bracket delta is unrelated income (loot/escrow/refund) and is ignored while the
// context stays armed for the real proceeds. Without a known price the 017 sign-only path
// applies — quicksell always takes it (per-order fill quantities aren't on the wire).
func (p *Pipeline) attributeDelta(delta, now int64) {
	c := p.instant
	if c.expectedKnown && c.single {
		if !p.deltaInBracket(delta) {
			return // unrelated income — keep waiting for the real proceeds
		}
		c.net = delta
		c.deltas++
		p.emitInstantTrade()
		p.instant = nil
		return
	}
	// Sign-only (unknown price + all quicksell): 017 behavior, unchanged (no regression).
	c.net += delta
	c.deltas++
	p.emitInstantTrade()
	p.disarmOrExtend(now)
}

// disarmOrExtend applies the 017 sign-only disarm rule (single → one delta; quicksell →
// capped at its item count) or extends the window for the next burst delta.
func (p *Pipeline) disarmOrExtend(now int64) {
	c := p.instant
	if c.single || (c.count > 0 && c.deltas >= c.count) {
		p.instant = nil
	} else {
		c.expiresMS = now + instantWindowMS
	}
}

// deltaInBracket reports whether a correct-sign delta matches the expected order value for
// the current SINGLE instant trade. A buy pays the price exactly (±ε rounding). A sell nets
// the price minus an unknown (premium-dependent) tax, so its net is bracketed
// [gross×(1−maxTax), gross]; only a small rounding pad sits above gross (our expected value
// can round down) — never the full buy ε, which would admit unrelated income above gross.
func (p *Pipeline) deltaInBracket(delta int64) bool {
	c := p.instant
	g := float64(c.expectedGross)
	mag := float64(abs64(delta))
	if c.direction == model.TradeBought {
		return mag >= g*(1-instantBuyEpsilonRate) && mag <= g*(1+instantBuyEpsilonRate)
	}
	return mag >= g*(1-instantSellMaxTaxRate) && mag <= g*(1+instantSellUpperPad)
}

// abs64 returns the absolute value of x.
func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// seedWalletBaseline records the login wallet (R:2 Join) as the delta baseline, but only
// before any live E:81 — so an instant trade whose proceeds are the session's FIRST E:81
// still correlates. Later zone-change re-seeds must not overwrite a fresher live balance.
func (p *Pipeline) seedWalletBaseline(silver int64) {
	if !p.walletSeen {
		p.walletSilver = silver
		p.walletSeen = true
	}
}

// clearInstant drops any pending instant context — used when an order-placement (op
// 79/80) fires so its escrow/setup-fee wallet delta can't be mis-attributed to a prior
// instant trade (017).
func (p *Pipeline) clearInstant() { p.instant = nil }

// emitSetupTrade records one order-listing setup fee as its own ledger row (Source
// "setup", a pure expense: gross 0, net = −fee). The fee comes from the order value, not
// a wallet delta (a buy order's delta also holds refundable escrow).
func (p *Pipeline) emitSetupTrade(itemIndex, qty int, fee int64) {
	p.instantSeq++
	p.sink.AddTrade(model.Trade{
		TradeID:       fmt.Sprintf("setup:%d-%d", p.nowMS(), p.instantSeq),
		Direction:     model.TradeBought,
		Source:        model.TradeSourceSetup,
		ItemIndex:     itemIndex,
		PartialAmount: qty,
		TotalAmount:   qty,
		SetupFee:      fee,
		Net:           -fee,
		Received:      p.nowMS(),
	})
}

// emitInstantTrade builds the trade from the accumulated instant context and upserts it
// (same trade id → a quicksell burst accumulates rather than duplicating). Net is the
// real wallet delta; gross/tax are reconstructed from the sales-tax rate.
func (p *Pipeline) emitInstantTrade() {
	c := p.instant
	var bd tradecalc.Breakdown
	if c.direction == model.TradeSold {
		bd = tradecalc.SoldFromNet(c.net, tradecalc.DefaultSalesTaxRate)
	} else {
		bd = tradecalc.Bought(-c.net) // c.net is negative for a buy
	}
	amount := c.amount
	if c.source == model.TradeSourceQuick {
		amount = c.count
	}
	p.sink.AddTrade(model.Trade{
		TradeID:       c.tradeID,
		Direction:     c.direction,
		Source:        c.source,
		ItemID:        c.itemID,
		ItemIndex:     c.itemIndex,
		PartialAmount: amount,
		TotalAmount:   amount,
		Gross:         bd.Gross,
		SalesTax:      bd.SalesTax,
		Net:           bd.Net,
		TaxEstimated:  bd.Estimated,
		Received:      p.nowMS(),
	})
	if p.debug {
		log.Printf("[trade] %s %s net=%d gross=%d (%s)", c.source, c.direction, bd.Net, bd.Gross, c.tradeID)
	}
}

// ── Parser callbacks ─────────────────────────────────────────────────────────

// OnRequest handles the player's own OUTGOING operation requests (passively
// observed). The op code lives in param 253; loot correlation needs the item-move
// requests (007). The REAL key-253 code is required — the raw-opByte fallback can
// collide on partially-decoded requests and feed bogus guids into the loot tracker.
func (p *Pipeline) OnRequest(_ byte, params map[byte]interface{}) {
	code, ok := capture.IntParam(params, 253)
	if !ok {
		return
	}
	if p.debug && (code == 30 || code == 39) {
		log.Printf("[flow] request: code=%d keys=%v", code, paramKeys(params))
	}
	p.dispatch(probe.KindRequest, code, params)
}

// OnResponse handles operation responses. The op code lives in param 253 (opByte is
// the raw Photon opcode); own-state Join carries 253=2. Self-identity rides on EVERY
// Join op-2 (login AND zone changes, where the player's object id changes per map),
// so it is read here as a FIXED pre-dispatch step — not a registry handler — and is
// not gated by the key-55 own-state guard (login-only). See the ordering contract in
// dispatch.go.
func (p *Pipeline) OnResponse(opByte byte, _ int16, _ string, params map[byte]interface{}) {
	rc := codeFrom(params, 253, int(opByte))
	if fromKey, ok := capture.IntParam(params, 253); ok && fromKey == 2 {
		p.updateSelf(params)
	}
	p.dispatch(probe.KindResponse, rc, params)
}

// OnEvent handles server events. Declarations (New*Item 30-37) are registered BEFORE
// dispatch so containers referencing the new object ids resolve — ordering contract.
func (p *Pipeline) OnEvent(evByte byte, params map[byte]interface{}) {
	code := codeFrom(params, 252, int(evByte))
	p.registerNewItem(code, params)
	p.dispatch(probe.KindEvent, code, params)
}

// ── Self identity + zone (fixed pre-dispatch step) ───────────────────────────

// updateSelf refreshes the local player's own object id + name from a Join op-2
// response (key 0 objId, key 2 name). Requires BOTH fields — a partial match (e.g. a
// stray int at key 0 of a non-Join op-2 variant) must never overwrite a good identity.
func (p *Pipeline) updateSelf(params map[byte]interface{}) {
	objID, name, ok := capture.SelfIdentity(params)
	if !ok || objID <= 0 || name == "" {
		return
	}
	p.selfObjID = objID
	p.selfName = name
	p.specReplacePending = true // a Join re-sends the whole Destiny Board (011)
	p.sink.SetSelf(objID, name)
	// The Join also carries the current location/cluster (key 8) — stamp it as the zone
	// so flow events know where they happened (per-zone analytics, 006). Open-world zones
	// only surface here (event 163 covers cities); raw cluster id is fine, named later.
	if zone, zok := params[8].(string); zok && zone != "" {
		name := p.zoneName(zone)
		p.sink.SetZone(name)
		// Mid-session starts miss the city-entry notification (event 163), leaving
		// physically-opened bank tabs city-less ("Bank" ghost group, live-seen
		// 2026-07-05 twice — the second time from a Lounge sub-cluster whose name
		// only STARTS with the city). Any cluster name that maps to a royal city
		// (exact, "Bank of X", or "<City> <suffix>" like Lounge/Markets) feeds the
		// current city.
		if city := cityOf(name); city != "" {
			p.sink.SetCurrentCity(city)
		}
	}
	// Own-container GUID bridge (008): bag (key 54, confirmed) + equipped candidate
	// (key 51). Entries update INDEPENDENTLY — a Join variant carrying only one of the
	// keys must not skip (or wipe) the other's bridge. Each virtual id keeps exactly
	// ONE wire guid: when a key's guid changes (character switch, hostile stream), the
	// stale reverse mapping is pruned, so the map is hard-bounded at 2 entries and an
	// old character's bag guid can't keep bridging to self-bag (XI, 009 review).
	if bagGUID, eqGUID, _ := capture.SelfContainers(params); bagGUID != "" || eqGUID != "" {
		if bagGUID != "" {
			p.setSelfContainerGUID(bagGUID, SelfBagGUID)
		}
		if eqGUID != "" {
			p.setSelfContainerGUID(eqGUID, SelfEquipGUID)
		}
		if p.debug {
			log.Printf("[hold] self containers: bag=%s equipped-candidate=%s", bagGUID, eqGUID)
		}
	}
	if p.debug {
		log.Printf("[flow] self set: objID=%d name=%q (op-2)", p.selfObjID, p.selfName)
	}
}

// isSelfObj reports whether an object id is the local player. Until self is known
// (selfObjID==0) it returns false so we never count another player's earnings.
func (p *Pipeline) isSelfObj(objID int) bool { return p.selfObjID != 0 && objID == p.selfObjID }

// zoneName resolves a raw cluster id to a readable zone name when the map is loaded.
func (p *Pipeline) zoneName(clusterID string) string {
	if p.locs != nil {
		return p.locs.Resolve(clusterID)
	}
	return clusterID
}

// nextFlowSeq returns a per-event unique nonce (capture runs on one goroutine, so a
// plain increment is race-free) for building dedup ids of events like harvest ticks
// that legitimately repeat with identical field values.
func (p *Pipeline) nextFlowSeq() int64 { p.flowSeq++; return p.flowSeq }

// ── Object registry + declaration drains ─────────────────────────────────────

// registerNewItem records objectId → {itemIndex, quality, count} from a New*Item
// event and feeds the item's server EstimatedMarketValue into valuation. Field map
// (reference client NewItem): key 1 = itemIndex, key 2 = quantity, key 4 = EMV
// (scaled ×10000), key 6 = quality, key 7 = durability.
func (p *Pipeline) registerNewItem(code int, params map[byte]interface{}) {
	if code < 30 || code > 37 { // NewEquipmentItem..NewEquipmentItemLegendarySoul
		return
	}
	objID, ok1 := capture.IntParam(params, 0)
	idx, ok2 := capture.IntParam(params, 1)
	if !ok1 || !ok2 {
		return
	}
	count := 1
	if c, ok := capture.IntParam(params, 2); ok && c > 0 {
		count = c
	}
	quality, _ := capture.IntParam(params, 6)
	if quality < 0 || quality > 5 { // furniture etc. put non-quality data here
		quality = 0
	}
	p.objMu.Lock()
	if _, exists := p.objReg[objID]; !exists {
		if len(p.objReg) >= objRegCap && len(p.objOrder) > 0 {
			delete(p.objReg, p.objOrder[0])
			p.objOrder = p.objOrder[1:]
		}
		p.objOrder = append(p.objOrder, objID)
	}
	ref := holdings.ItemRef{Index: idx, Quality: quality, Count: count}
	p.objReg[objID] = ref
	now := p.nowMS()
	pendGUID, invPending := p.pendingInv.Take(objID, now)        // own-state slot awaiting this declaration
	source, lootPending := p.pendingLootResolve.Take(objID, now) // loot hit awaiting it (TTL-guarded:
	target, putPending := p.pendingPuts.Take(objID, now)         // ids are reused across zones — a stale
	p.objMu.Unlock()                                             // drain would fabricate phantom events)

	// An own-state bag/equipped object declared after the fact: place it into its
	// self-container now that it resolves.
	if invPending {
		p.sink.IngestPutItem(pendGUID, objID, ref)
	}
	// A loot pickup whose declaration arrived after the move: emit it now (007).
	if lootPending {
		p.ingestLootObj(objID, ref, source)
	}
	// A holdings put whose declaration arrived late (008): place it now. An untracked
	// target still drops the item from its old spot (the put was authoritative — it left).
	if putPending {
		if !p.sink.IngestPutItem(target, objID, ref) {
			p.sink.IngestDeleteItem(objID)
			p.bagSlotClear(objID)
		}
	}

	// The item's own EstimatedMarketValue (key 4, a scalar int64) is the value the game
	// shows when you open it — feed it to valuation so held items are valued without a
	// market capture.
	if emv, ok := capture.IntParam(params, 4); ok && emv > 0 {
		p.sink.IngestEMV(idx, quality, int64(emv)/emvScale, p.nowMS())
	}
}

// resolveObjects maps container object ids to slot items (objId + ref), skipping
// unresolved ones. The objId is kept so incremental moves can target the item.
func (p *Pipeline) resolveObjects(objIDs []int) []holdings.SlotItem {
	p.objMu.Lock()
	defer p.objMu.Unlock()
	slots := make([]holdings.SlotItem, 0, len(objIDs))
	for _, id := range objIDs {
		if r, ok := p.objReg[id]; ok {
			slots = append(slots, holdings.SlotItem{ObjID: id, Ref: r})
		}
	}
	return slots
}

// resolveObj returns the ref for a single object id (for incremental Put).
func (p *Pipeline) resolveObj(objID int) (holdings.ItemRef, bool) {
	p.objMu.Lock()
	defer p.objMu.Unlock()
	r, ok := p.objReg[objID]
	return r, ok
}

// ── Holdings freshness glue (008) ────────────────────────────────────────────

// virtualContainer maps a wire container GUID to its holdings virtual id ("self-bag"
// / "self-equipped") when it is one of the player's own containers.
func (p *Pipeline) virtualContainer(guid string) (string, bool) {
	v, ok := p.selfContainerGUIDs[guid]
	return v, ok
}

// isSelfBag reports whether a wire GUID is the player's own bag — the ONE predicate
// behind the loot-suppression gate (a bag move is never a loot pickup, but the
// unconfirmed-candidate equipped guid must never suppress loot resolution).
func (p *Pipeline) isSelfBag(guid string) bool {
	v, ok := p.selfContainerGUIDs[guid]
	return ok && v == SelfBagGUID
}

// setSelfContainerGUID binds one wire guid to a virtual container id, pruning any
// previous guid bound to the same virtual id — the map never exceeds one entry per
// virtual container regardless of what the wire sends (XI).
func (p *Pipeline) setSelfContainerGUID(guid, virtual string) {
	for g, v := range p.selfContainerGUIDs {
		if v == virtual && g != guid {
			delete(p.selfContainerGUIDs, g)
		}
	}
	p.selfContainerGUIDs[guid] = virtual
}

func (p *Pipeline) bagSlotItem(slot int) (int, bool) {
	if slot < 0 || slot >= len(p.bagSlots) || p.bagSlots[slot] <= 0 {
		return 0, false
	}
	return p.bagSlots[slot], true
}

func (p *Pipeline) bagSlotSet(slot, objID int) {
	if slot < 0 || slot >= maxBagSlots {
		return
	}
	for slot >= len(p.bagSlots) { // bag grew (bigger bag equipped) — pad up to maxBagSlots
		p.bagSlots = append(p.bagSlots, 0)
	}
	p.bagSlots[slot] = objID
}

func (p *Pipeline) bagSlotClear(objID int) {
	for i, v := range p.bagSlots {
		if v == objID {
			p.bagSlots[i] = 0
			return
		}
	}
}

// logDrops emits the rate-limited (1/min) counted-loss line — losses must be
// observable without a debug flag (FR-004), shared by both logging consumers so the
// guard can never drift between copies (009 review). NOTE: the cumulative-counter
// guard re-logs a long-stable count once per minute — pre-009 behavior, preserved
// verbatim; a since-last-report delta is recorded 010 polish.
func (p *Pipeline) logDrops(dropped int, lastLogMS *int64, nowMS int64, format string) {
	if dropped > 0 && nowMS-*lastLogMS > 60_000 {
		*lastLogMS = nowMS
		log.Printf(format, dropped)
	}
}

func (p *Pipeline) queuePendingPut(objID int, target string) {
	now := p.nowMS()
	p.objMu.Lock()
	p.pendingPuts.Queue(objID, target, now)
	dropped := p.pendingPuts.Dropped()
	p.objMu.Unlock()
	p.logDrops(dropped, &p.lastPendingPutDropLog, now, "holdings: %d pending puts dropped so far (declaration never arrived)")
}

// clearSelfPendingPuts drops pending puts targeting the player's own containers —
// a fresh own-state snapshot (which carries BOTH bag and equipped) is authoritative
// and must not be overridden by a late drain (008 US3, symmetric for equipped).
func (p *Pipeline) clearSelfPendingPuts() {
	p.objMu.Lock()
	p.pendingPuts.Clear(func(target string) bool {
		return target == SelfBagGUID || target == SelfEquipGUID
	})
	p.objMu.Unlock()
}

// applyMoveToHoldings applies a single-item move request (op-30) to the holdings
// view: resolve the source slot to an item object, then relocate or drop it.
func (p *Pipeline) applyMoveToHoldings(srcGUID string, srcSlot int, dstGUID string, dstSlot int, hasDst bool) {
	var itemObj int
	if v, bridged := p.virtualContainer(srcGUID); bridged && v == SelfBagGUID {
		if id, ok := p.bagSlotItem(srcSlot); ok {
			itemObj = id
			p.bagSlots[srcSlot] = 0
		}
	} else if id, ok := p.lootTracker.SlotItem(srcGUID, srcSlot); ok {
		itemObj = id
	}
	if itemObj == 0 {
		return // source unknown/empty — nothing to apply (snapshot reconciles later)
	}
	p.applyMovedObject(itemObj, srcGUID, dstGUID, dstSlot, hasDst)
}

// applyMovedObject relocates a known item object: the destination is tried against
// holdings itself (bridged virtual id or any container holdings has seen — bank tabs
// stay known there long after the loot tracker's 10-minute TTL would have swept them);
// an untracked destination (market, sale, never-opened bank) drops the item from view
// (008 contract rules 3-4). The bag slot map tracks bag-side changes.
func (p *Pipeline) applyMovedObject(itemObj int, srcGUID, dstGUID string, dstSlot int, hasDst bool) {
	if v, bridged := p.virtualContainer(srcGUID); bridged && v == SelfBagGUID {
		p.bagSlotClear(itemObj)
	}
	target := ""
	if hasDst {
		if v, bridged := p.virtualContainer(dstGUID); bridged {
			target = v
			if v == SelfBagGUID {
				p.bagSlotSet(dstSlot, itemObj)
			}
		} else {
			target = dstGUID // let holdings decide — it knows every snapshotted container
		}
	}
	if target == "" {
		p.sink.IngestDeleteItem(itemObj) // leaves every tracked view; reappears via snapshots
		if p.debug {
			log.Printf("[hold] move → no dst, dropped from view: obj=%d", itemObj)
		}
		return
	}
	if ref, ok := p.resolveObj(itemObj); ok {
		if !p.sink.IngestPutItem(target, itemObj, ref) {
			p.sink.IngestDeleteItem(itemObj)
			p.bagSlotClear(itemObj)
			if p.debug {
				log.Printf("[hold] move → untracked dst, dropped from view: obj=%d", itemObj)
			}
			return
		}
	} else {
		p.queuePendingPut(itemObj, target)
	}
	if p.debug {
		log.Printf("[hold] move applied: obj=%d → %s", itemObj, target)
	}
}

// ── Loot flow emission (007) ─────────────────────────────────────────────────

// ingestLootObj emits one loot flow event from a resolved object registry entry —
// the single place the loot dedup id ("lt:<itemObjID>") and argument order live, so
// the fast path and the late-declaration path can never drift.
func (p *Pipeline) ingestLootObj(itemObjID int, ref holdings.ItemRef, source string) {
	p.sink.IngestLoot(fmt.Sprintf("lt:%d", itemObjID), ref.Index, ref.Quality, ref.Count, p.nowMS(), source)
}

// emitLootHits turns tracker hits into flow loot events: the item identity, quality
// and stack count come from the object registry (New*Item declaration) — quality-keyed
// valuation works (closes the ADR-022 quality-0 gap for loot). Undeclared objects wait
// in pendingLootResolve until their declaration arrives or the TTL expires.
func (p *Pipeline) emitLootHits(hits []loot.Hit) {
	for _, h := range hits {
		if p.debug {
			log.Printf("[flow] loot hit: itemObj=%d source=%q", h.ItemObjID, h.Source)
		}
		if ref, ok := p.resolveObj(h.ItemObjID); ok {
			p.ingestLootObj(h.ItemObjID, ref, h.Source)
			continue
		}
		now := p.nowMS()
		p.objMu.Lock()
		p.pendingLootResolve.Queue(h.ItemObjID, h.Source, now)
		dropped := p.pendingLootResolve.Dropped()
		p.objMu.Unlock()
		p.logDrops(dropped, &p.lastPendingLootDropLog, now, "loot: %d pending pickups dropped so far (declaration never arrived)")
	}
}

// ── Own-state self containers ────────────────────────────────────────────────

// ingestSelf sets one own-state self-container (bag or equipped) from its slot object
// ids: already-declared objects are placed now, the rest queue in pendingInv keyed to
// this container. Re-runs replace the container wholesale (own-state is a full list).
func (p *Pipeline) ingestSelf(guid, tab string, objIDs []int) {
	slots := p.resolveObjects(objIDs)
	if p.debug {
		idxs := make([]int, len(slots))
		for i, s := range slots {
			idxs[i] = s.Ref.Index
		}
		log.Printf("[self] %s objIDs=%v resolvedItemIdx=%v (%d/%d resolved)", tab, objIDs, idxs, len(slots), len(objIDs))
	}
	p.sink.IngestSelfContainer(guid, tab, slots)
	resolved := make(map[int]bool, len(slots))
	for _, s := range slots {
		resolved[s.ObjID] = true
	}
	now := p.nowMS()
	p.objMu.Lock()
	p.pendingInv.Clear(func(g string) bool { return g == guid }) // stale entries for this container
	for _, id := range objIDs {
		if !resolved[id] {
			p.pendingInv.Queue(id, guid, now)
		}
	}
	p.objMu.Unlock()
}

// ── Small helpers ────────────────────────────────────────────────────────────

// paramKeys returns the sorted parameter keys of a message (debug aid).
func paramKeys(params map[byte]interface{}) []int {
	ks := make([]int, 0, len(params))
	for k := range params {
		ks = append(ks, int(k))
	}
	sort.Ints(ks)
	return ks
}

// extractEMV pulls (index, quality, silver value) from the two EMV layouts.
func extractEMV(params map[byte]interface{}) (index, quality int, value int64, ok bool) {
	if id, okId := firstInt(params[0]); okId {
		if v, okV := firstInt64(params[1]); okV {
			return id, 0, v / emvScale, true
		}
	}
	if id, okId := firstInt(params[2]); okId {
		v, _ := firstInt64(params[4])
		q, _ := firstInt(params[3])
		return id, q, v / emvScale, true
	}
	return 0, 0, 0, false
}

func codeFrom(params map[byte]interface{}, key byte, fallback int) int {
	if v, ok := params[key]; ok {
		if n, ok := firstInt(v); ok {
			return n
		}
		switch n := v.(type) {
		case int16:
			return int(n)
		case int32:
			return int(n)
		}
	}
	return fallback
}

func firstInt(v interface{}) (int, bool) {
	switch a := v.(type) {
	case []int16:
		if len(a) > 0 {
			return int(a[0]), true
		}
	case []int32:
		if len(a) > 0 {
			return int(a[0]), true
		}
	case []byte:
		if len(a) > 0 {
			return int(a[0]), true
		}
	case int16:
		return int(a), true
	case int32:
		return int(a), true
	}
	return 0, false
}

func firstInt64(v interface{}) (int64, bool) {
	switch a := v.(type) {
	case []int32:
		if len(a) > 0 {
			return int64(a[0]), true
		}
	case []int64:
		if len(a) > 0 {
			return a[0], true
		}
	}
	return 0, false
}

// SeedSpecUnlocked restores the persisted unlocked-node set at startup so maxed
// (level-100) branches show immediately, before any E:155 arrives this session
// (E:155 only fires on completion, not login — 011 live finding). Emits if a board
// snapshot is already present.
// SeedSpecEnum restores the persisted E:1 board enumeration at startup (012) so a warm
// login (E:1 without k2) decodes before any cold login this session.
func (p *Pipeline) SeedSpecEnum(ids []int) {
	if len(ids) > 0 {
		p.specEnum.Restore(ids)
	}
}

func (p *Pipeline) SeedSpecUnlocked(ids []int) {
	if len(ids) == 0 {
		return
	}
	p.specUnlocked = make(map[int]bool, len(ids))
	for _, id := range ids {
		p.specUnlocked[id] = true
	}
	p.specUnlockedSeen = true
	p.emitSpec()
}
