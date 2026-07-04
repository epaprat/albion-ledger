package app

// Market-value handler: the standalone EMV event (466) feeding valuation.

import (
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatItemValueEMV, handleEMV)
}

func handleEMV(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if idx, quality, value, ok := extractEMV(params); ok {
		p.sink.IngestEMV(idx, quality, value, p.nowMS())
	}
}
