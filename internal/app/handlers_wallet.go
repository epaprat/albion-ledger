package app

// Wallet handler (feature 016): E:81 (event) carries the player's liquid silver balance
// on every change. It feeds the Service's wallet state, which combines with holdings
// value into net worth. The login seed (R:2 k33) is pulled in handleOwnState.

import (
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
)

func init() {
	register(model.CatWallet, handleWallet)
}

// handleWallet — E:81: the own wallet balance. The server only sends the local player's
// wallet, so no self-filter is needed; SetWallet is newest-wins.
func handleWallet(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	silver, ok := capture.WalletBalance(params)
	if !ok {
		return
	}
	p.sink.SetWallet(silver, p.nowMS())
	if p.debug {
		log.Printf("[wallet] balance=%d", silver)
	}
}
