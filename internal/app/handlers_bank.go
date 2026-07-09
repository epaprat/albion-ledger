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
	register(model.CatBankTabRequest, handleBankTabRequest)
}

// handleBankTabRequest — Q:518: the client asks for a tab's content. Remember which tab
// so the default/open tab's GUID-LESS content response (which omits k0) can be attributed
// to it (its response always immediately follows this request).
func handleBankTabRequest(p *Pipeline, _ probe.Kind, _ int, params map[byte]interface{}) {
	if guid, ok := capture.BankTabRequest(params); ok {
		p.lastBankTabGUID = guid
	}
}

// Bridge caps (XI): a game account has a bounded set of vault locations and tabs;
// anything past these is hostile/garbage, dropped counted via debug visibility.
const (
	maxVaultCityEntries = 64
	maxTabMetaEntries   = 512
)

// vaultValueScale: R:516 k5 carries silver ×10000 (same fixed-point as every other
// silver field on the wire).
const vaultValueScale = silverScale

type tabInfo struct {
	city string // resolved city display name
	name string // REAL tab name from the overview
}

// royalCities are the only places with K-overview banks.
var royalCities = []string{"Thetford", "Fort Sterling", "Lymhurst", "Bridgewatch", "Martlock", "Caerleon", "Brecilien"}

// cityOf maps a cluster display name to its royal city: exact match, "Bank of X",
// or a sub-cluster prefix ("Fort Sterling Lounge" → "Fort Sterling"); "" otherwise.
func cityOf(name string) string {
	name = bankCityDisplay(name)
	for _, c := range royalCities {
		if name == c || (len(name) > len(c) && name[:len(c)] == c && name[len(c)] == ' ') {
			return c
		}
	}
	return ""
}

// bankCityDisplay normalizes a bank cluster's display name to the CITY name the
// physical-open path uses ("Bank of Fort Sterling" → "Fort Sterling"), so both
// sources group under ONE city instead of duplicating it (live-seen 2026-07-05).
func bankCityDisplay(clusterName string) string {
	const prefix = "Bank of "
	if len(clusterName) > len(prefix) && clusterName[:len(prefix)] == prefix {
		return clusterName[len(prefix):]
	}
	return clusterName
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
	// Vault/tab guids are EPHEMERAL — every K opening mints a fresh set (live-seen
	// 2026-07-05: three openings, three different guids for the same tab). A fresh
	// location list therefore also resets the tab bridge; stale ephemeral guids are
	// garbage that would only pile toward the cap.
	p.tabMeta = make(map[string]tabInfo)
	values := make(map[string]int64, len(locs))
	for _, l := range locs {
		city := bankCityDisplay(p.zoneName(l.ClusterID))
		p.vaultCity[l.VaultGUID] = city
		if l.RawValue > 0 {
			values[city] += l.RawValue / vaultValueScale // += : two clusters can share a display city
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
	city, cityKnown := p.vaultCity[vault]
	if !cityKnown {
		// A 517 whose vault is not in the CURRENT location list is a stale leftover
		// from a previous (ephemeral-guid) opening — recording its tabs with an
		// empty city would mint unreconcilable ghost containers. Skip; the next
		// 516+517 pair re-delivers everything (live/replay-seen 2026-07-05).
		p.dropf("[bank] tabs for unknown vault %s dropped", vault)
		return
	}
	for _, t := range tabs {
		if _, exists := p.tabMeta[t.TabGUID]; !exists && len(p.tabMeta) >= maxTabMetaEntries {
			p.dropf("[bank] tabMeta full, dropped tab %s", t.TabGUID)
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
	// The bulk mail-list sync (GetMailInfos, many mails) arrives as an op-1 response —
	// the same code as bank tab content. Its MARKETPLACE type-array signature routes it
	// to mail handling before the bank shape guard (017; live-decoded 2026-07-08).
	if p.ingestMailInfos(params) {
		return
	}
	tabGUID, rows, ok := capture.BankTabContent(params)
	if !ok {
		return // shape lock: unrelated op-1 responses die here (contract rule 4)
	}
	// The default/open tab's content arrives GUID-LESS — attribute it to the tab the
	// client just requested (op 518), else it (often 100+ items) is lost (010 fix).
	if tabGUID == "" {
		tabGUID = p.lastBankTabGUID
	}
	meta, known := p.tabMeta[tabGUID]
	if !known || meta.city == "" {
		// No tab meta: either an unrelated shape-passing op-1 or a content packet
		// racing ahead of its 517 (never observed live — 516/517 always precede).
		// Skipping is safer than inventing a fallback identity: ephemeral guids mean
		// a mislabeled container could never be reconciled later (live 2026-07-05).
		p.dropf("[bank] content for unknown tab %s dropped (no meta)", tabGUID)
		return
	}
	refs := make([]holdings.ItemRef, len(rows))
	valued := 0
	for i, r := range rows {
		refs[i] = holdings.ItemRef{Index: r.ItemIndex, Count: r.Count, Quality: r.Quality}
		// The overview carries per-row UNIT values for equipment (×10000, decoded
		// live 2026-07-05) — feed them to valuation so summary rows price without
		// any prior declaration. Resources arrive as 0 (not reported by the game);
		// they price via the quality-0 EMV fallback once seen elsewhere.
		if r.UnitValue > 0 {
			p.sink.IngestEMV(r.ItemIndex, r.Quality, r.UnitValue/vaultValueScale, p.nowMS())
			valued++
		}
	}
	// STABLE synthetic container id: the wire guid changes on every K opening, so
	// keying by it would grow one container per opening for the same tab (live-seen
	// duplication). City+name is the stable identity the player actually sees.
	stableID := "vault:" + meta.city + ":" + meta.name
	p.sink.IngestVaultSummaryTab(stableID, meta.city, meta.name, refs)
	if p.debug {
		log.Printf("[bank] content tab=%s city=%q name=%q rows=%d valued=%d", tabGUID, meta.city, meta.name, len(rows), valued)
	}
}
