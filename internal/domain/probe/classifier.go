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
const (
	opMove                       = 23
	opAuctionGetOffers           = 82
	opAuctionGetRequests         = 83
	opAuctionGetItemAverageStats = 96
	opGoldMarketGetAverageInfo   = 251
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
