package app

// K bank-overview handlers (feature 010): the location list (R:516), per-vault tab
// lists (R:517) and tab content summaries (R:1 — NO key 253, opByte fallback +
// extractor shape lock — and its named variant R:518) flow into holdings as
// city-tagged bank tabs with their REAL in-game names, without opening any bank.

import (
	"log"

	"github.com/epaprat/albion-ledger/internal/adapter/capture"
	"github.com/epaprat/albion-ledger/internal/domain/model"
	"github.com/epaprat/albion-ledger/internal/domain/probe"
	"github.com/epaprat/albion-ledger/internal/holdings"
)

func init() {
	register(model.CatBankLocations, handleBankLocations)
	register(model.CatBankTabs, handleBankTabs)
	register(model.CatBankTabContent, handleBankTabContent)
}

// Bridge caps (XI): a game account has a bounded set of vault locations and tabs;
// anything past these is hostile/garbage, dropped counted via debug visibility.
const (
	maxVaultCityEntries = 64
	maxTabMetaEntries   = 512
)

// vaultValueScale: R:516 k5 carries silver ×10000 (same fixed-point as every other
// silver field on the wire).
const vaultValueScale = 10000

type tabInfo struct {
	city string // resolved city display name
	name string // REAL tab name from the overview
}

// handleBankLocations — R:516: rebuild the vault→city bridge and publish per-city
// totals. REBUILD (not amend): each K opening carries the full location list, so a
// wholesale replace both stays bounded and drops stale cities (contract rule 8).
func handleBankLocations(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	locs, ok := capture.BankLocations(params)
	if !ok || len(locs) > maxVaultCityEntries {
		return
	}
	p.vaultCity = make(map[string]string, len(locs))
	values := make(map[string]int64, len(locs))
	for _, l := range locs {
		city := p.zoneName(l.ClusterID)
		p.vaultCity[l.VaultGUID] = city
		if l.RawValue > 0 {
			values[city] = l.RawValue / vaultValueScale
		}
	}
	p.sink.IngestCityVaultValues(values)
	if p.debug {
		log.Printf("[bank] locations n=%d", len(locs))
	}
}

// handleBankTabs — R:517: record tab guid → (city, real name) so content packets
// can land under the right city with the right name.
func handleBankTabs(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	vault, tabs, ok := capture.BankTabs(params)
	if !ok {
		return
	}
	city := p.vaultCity[vault] // "" tolerated → content falls back to the generic city
	for _, t := range tabs {
		if _, exists := p.tabMeta[t.TabGUID]; !exists && len(p.tabMeta) >= maxTabMetaEntries {
			if p.debug {
				log.Printf("[bank] tabMeta full, dropped tab %s", t.TabGUID)
			}
			continue
		}
		p.tabMeta[t.TabGUID] = tabInfo{city: city, name: t.Name}
	}
	if p.debug {
		log.Printf("[bank] tabs vault=%s city=%q n=%d", vault, city, len(tabs))
	}
}

// handleBankTabContent — R:1/R:518: apply one tab's type-based content summary.
// Meta not seen yet (content-first ordering) → tolerant fallback; a later 517 +
// re-browse corrects name and city (contract rules 2/7 — no permanent wrongness).
func handleBankTabContent(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	tabGUID, rows, ok := capture.BankTabContent(params)
	if !ok {
		return // shape lock: unrelated op-1 responses die here (contract rule 4)
	}
	meta, known := p.tabMeta[tabGUID]
	if !known {
		meta = tabInfo{city: "", name: "Vault " + tabGUID[:8]}
	}
	refs := make([]holdings.ItemRef, len(rows))
	for i, r := range rows {
		refs[i] = holdings.ItemRef{Index: r.ItemIndex, Count: r.Count} // quality 0: research D6
	}
	p.sink.IngestVaultSummaryTab(tabGUID, meta.city, meta.name, refs)
	if p.debug {
		log.Printf("[bank] content tab=%s city=%q name=%q rows=%d", tabGUID, meta.city, meta.name, len(rows))
	}
}
