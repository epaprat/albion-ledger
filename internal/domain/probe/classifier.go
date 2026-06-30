// Package probe classifies decoded Photon messages into target categories and
// rolls them up into per-category coverage with a confidence verdict.
package probe

import (
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/port"
)

// Kind is the Photon message kind a code belongs to. Event and operation codes
// share a numeric space but are routed separately, so the kind disambiguates.
type Kind int

const (
	KindEvent Kind = iota
	KindRequest
	KindResponse
)

func (k Kind) String() string {
	switch k {
	case KindResponse:
		return "response"
	case KindRequest:
		return "request"
	default:
		return "event"
	}
}

// Classified is the result of classifying one message.
type Classified struct {
	Category       model.Category
	Code           int
	FieldsPresent  int
	FieldsExpected int
}

// PositionCodes are movement/position codes that MUST stay unclassified (ToS-safe,
// Constitution V). They are simply absent from the code registry; this list exists
// so a test can assert they never resolve to a category.
var PositionCodes = struct {
	Events []int
	Ops    []int
}{
	Events: []int{3, 29, 123, 335}, // Move, NewCharacter, NewMob, RecordCameraMove
	Ops:    []int{23},              // Move
}

// evEstimatedMarketValue is special-cased (two wire layouts), so the classifier
// recognises it directly rather than via the generic field-count path.
const evEstimatedMarketValue = 466

// Classifier maps a decoded message to a category + field-completeness using the
// data-driven code registry (FR-012). The registry decides WHICH codes map where;
// only the EMV two-layout handling stays in code.
type Classifier struct {
	reg port.CodeRegistry
}

// New returns a Classifier backed by the given code registry.
func New(reg port.CodeRegistry) *Classifier { return &Classifier{reg: reg} }

// Classify returns the classification and ok=false when the code is unhandled
// (unknown OR a deliberately excluded position/radar code).
func (c *Classifier) Classify(kind Kind, code int, params map[byte]interface{}) (Classified, bool) {
	cat, guardKey, hasGuard, ok := c.reg.Lookup(kind.String(), code)
	if !ok {
		return Classified{}, false
	}

	// EMV has two wire layouts; handle directly (see classifyEMV).
	if cat == model.CatItemValueEMV {
		return classifyEMV(code, params)
	}

	if hasGuard {
		if _, present := params[byte(guardKey)]; !present {
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
	return Classified{Category: cat, Code: code, FieldsPresent: present, FieldsExpected: len(expected)}, true
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
		return Classified{}, false
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
