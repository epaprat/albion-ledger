// Package probe classifies decoded Photon messages into target categories and
// rolls them up into per-category coverage with a confidence verdict.
package probe

import "github.com/epaprat/albion-ledger/internal/domain/model"

// Kind is the Photon message kind a code belongs to. Event and operation codes
// share a numeric space but are routed separately, so the kind disambiguates.
type Kind int

const (
	KindEvent Kind = iota
	KindRequest
	KindResponse
)

// Classified is the result of classifying one message.
type Classified struct {
	Category       model.Category
	Code           int
	FieldsPresent  int
	FieldsExpected int
}

// Albion event codes (from AFM EventCodes.cs).
const (
	evMove                       = 3
	evNewCharacter               = 29
	evNewEquipmentItem           = 30
	evNewSimpleHarvestableObject = 38
	evHarvestFinished            = 61
	evTakeSilver                 = 62
	evUpdateFame                 = 82
	evNewLoot                    = 98
	evAttachItemContainer        = 99
	evNewMob                     = 123
	evCharacterStats             = 143
	evRewardGranted              = 267
	evRecordCameraMove           = 335
	evBankVaultInfo              = 414
	evEstimatedMarketValue       = 466
)

// Albion operation codes (from AFM OperationCodes.cs).
// Operation codes are taken from LIVE payload shapes, not the AFM enum, which is
// version-skewed (e.g. it calls code 2 "Ping" though it carries character state).
const (
	opMove             = 23
	opAuctionGetOffers = 82 // R: []str sell orders (item-specific query)
	// opAuctionGetRequests: code 81 returns []str orders for a broad query — the
	// buy-orders view. Provisional: buy vs sell may ultimately need order-content
	// parsing; for now the code separates them.
	opAuctionGetRequests = 81
	// opAuctionGetItemAverageStats: R:95 carries price + timestamp arrays for the
	// requested item (price history). Enum says 96; live is 95.
	opAuctionGetItemAverageStats = 95
	// opGoldMarketGetAverageInfo: R:248 carries the 4 current gold prices. Enum
	// says 251; live is 248.
	opGoldMarketGetAverageInfo = 248
	// opPlayerState is the own-character full-state response (login/zone change)
	// carrying the masteries array at key 55. Enum mislabels code 2 as "Ping".
	opPlayerState = 2
)

// eventCategory / opCategory are the classifier registries (Principle II): one
// entry per handled code. Adding a category is a new registry entry — no edit to
// the Classify dispatch. Position/radar codes are deliberately ABSENT so they
// fall through to "unhandled" and are never attributed to a category (FR-004, V).
var eventCategory = map[int]model.Category{
	evNewEquipmentItem:           model.CatEquipment,
	evNewSimpleHarvestableObject: model.CatGatherFishing,
	evHarvestFinished:            model.CatGatherFishing,
	evRewardGranted:              model.CatGatherFishing,
	evTakeSilver:                 model.CatSilver,
	evUpdateFame:                 model.CatFame,
	evNewLoot:                    model.CatLoot,
	evAttachItemContainer:        model.CatInventory,
	evCharacterStats:             model.CatCharacterSpec,
	evBankVaultInfo:              model.CatBank,
	evEstimatedMarketValue:       model.CatItemValueEMV,
}

var opResponseCategory = map[int]model.Category{
	opAuctionGetOffers:           model.CatMarketSellOrders,
	opAuctionGetRequests:         model.CatMarketBuyOrders,
	opAuctionGetItemAverageStats: model.CatMarketHistory,
	opGoldMarketGetAverageInfo:   model.CatGoldPrice,
	opPlayerState:                model.CatCharacterSpec,
}

// responseGuard / eventGuard require a discriminator key to be present before a
// message is classified — prevents look-alike/empty variants from being counted.
// Key absent → treated as unhandled.
var responseGuard = map[int]byte{
	opPlayerState: 55, // only the real own-state blob carries the masteries array
}

// PositionCodes are movement/position codes explicitly EXCLUDED from
// classification (ToS-safe, Constitution V). Exposed for the exclusion test.
var PositionCodes = struct {
	Events []int
	Ops    []int
}{
	Events: []int{evMove, evNewCharacter, evNewMob, evRecordCameraMove},
	Ops:    []int{opMove},
}

// Classifier maps a decoded message to a category + field-completeness.
type Classifier struct{}

// New returns a Classifier.
func New() *Classifier { return &Classifier{} }

// Classify returns the classification and ok=false when the code is unhandled
// (unknown OR deliberately excluded position/radar data).
func (c *Classifier) Classify(kind Kind, code int, params map[byte]interface{}) (Classified, bool) {
	var cat model.Category
	var ok bool
	switch kind {
	case KindEvent:
		cat, ok = eventCategory[code]
	case KindResponse:
		cat, ok = opResponseCategory[code]
	default:
		ok = false
	}
	if !ok {
		return Classified{}, false
	}
	// EstimatedMarketValueUpdate has two wire layouts: {0=id,1=value} and
	// {2=id,3=quality,4=value}. Handle both; an update with neither value key
	// carries no valuation and is treated as unhandled.
	if cat == model.CatItemValueEMV {
		return classifyEMV(code, params)
	}

	if guardKey, has := responseGuard[code]; has && kind == KindResponse {
		if _, present := params[guardKey]; !present {
			return Classified{}, false // discriminator missing → not this category
		}
	}
	expected := model.ExpectedFields[cat]
	present := 0
	for _, k := range expected {
		if _, has := params[k]; has {
			present++
		}
	}
	return Classified{
		Category:       cat,
		Code:           code,
		FieldsPresent:  present,
		FieldsExpected: len(expected),
	}, true
}

// classifyEMV handles the two EstimatedMarketValueUpdate layouts:
//   - A: key 0 = item ids,  key 1 = values
//   - B: key 2 = item ids,  key 4 = values (key 3 = quality)
//
// We expect id + value (2 fields). An update with neither value key is unhandled.
func classifyEMV(code int, params map[byte]interface{}) (Classified, bool) {
	var idKey, valKey byte
	switch {
	case has(params, 1):
		idKey, valKey = 0, 1
	case has(params, 4):
		idKey, valKey = 2, 4
	default:
		return Classified{}, false // no value array → not a valuation
	}
	present := 0
	if has(params, idKey) {
		present++
	}
	if has(params, valKey) {
		present++
	}
	return Classified{Category: model.CatItemValueEMV, Code: code, FieldsPresent: present, FieldsExpected: 2}, true
}

func has(params map[byte]interface{}, k byte) bool {
	_, ok := params[k]
	return ok
}
