package export

// Dataset producers: one per visible dataset, each mapping the UI-facing view
// model to a FIXED header (contract §2 — changing one is a contract change)
// plus fully formatted string rows. All value formatting happens here; Encode
// only escapes. Rules (research R-2): silver as plain integers with unknown as
// the empty cell, epoch ms as "2006-01-02 15:04:05" local time (zero → empty),
// bools as true/false, progress with 2 decimals.

import (
	"strconv"
	"time"

	"github.com/epaprat/albion-ledger/internal/domain/model"
)

const timeLayout = "2006-01-02 15:04:05"

func fmtTime(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(timeLayout)
}

func fmtInt(v int) string { return strconv.Itoa(v) }

func fmtInt64(v int64) string { return strconv.FormatInt(v, 10) }

func fmtBool(v bool) string { return strconv.FormatBool(v) }

// HoldingsRows renders the holdings dataset (contract §2 holdings).
func HoldingsRows(items []model.HoldingItem) ([]string, [][]string) {
	header := []string{"city", "group", "item", "uniqueName", "tier", "enchant", "quality",
		"count", "unitValue", "totalValue", "valueSource", "stale", "lastSeen"}
	rows := make([][]string, 0, len(items))
	for _, h := range items {
		unit, total := "", ""
		if h.Valuation.Amount > 0 {
			unit = fmtInt64(h.Valuation.Amount)
			total = fmtInt64(h.Valuation.Amount * int64(h.Count))
		}
		rows = append(rows, []string{
			h.City, h.Group, h.Item.DisplayName, h.Item.UniqueName,
			fmtInt(h.Item.Tier), fmtInt(h.Item.Enchant), fmtInt(h.Item.Quality),
			fmtInt(h.Count), unit, total, string(h.Valuation.Source),
			fmtBool(h.Valuation.Stale), fmtTime(h.LastSeen),
		})
	}
	return header, rows
}

// FlowRows renders the activity-flow dataset (contract §2 flow). Silver/fame
// stay literal integers — zero is meaningful on a flow event.
func FlowRows(events []model.FlowEventView) ([]string, [][]string) {
	header := []string{"time", "kind", "item", "uniqueName", "tier", "enchant", "quality",
		"count", "silver", "fame", "valued", "source", "zone"}
	rows := make([][]string, 0, len(events))
	for _, e := range events {
		rows = append(rows, []string{
			fmtTime(e.TS), string(e.Kind), e.ItemDisplayName, e.UniqueName,
			fmtInt(e.Tier), fmtInt(e.Enchant), fmtInt(e.Quality),
			fmtInt(e.Count), fmtInt64(e.Silver), fmtInt64(e.Fame),
			fmtBool(e.Valued), e.Source, e.Zone,
		})
	}
	return header, rows
}

// ZoneRows renders the zone-analytics dataset (contract §2 zones). ActiveMS
// becomes minutes with one decimal — the UI's own unit.
func ZoneRows(zones []model.ZoneStatView) ([]string, [][]string) {
	header := []string{"zone", "activeMinutes", "netSilver", "silverPerHour", "gatherValue",
		"gatherPerHour", "fame", "famePerHour", "events", "insufficientData"}
	rows := make([][]string, 0, len(zones))
	for _, z := range zones {
		rows = append(rows, []string{
			z.Zone, strconv.FormatFloat(float64(z.ActiveMS)/60000, 'f', 1, 64),
			fmtInt64(z.NetSilver), fmtInt64(z.SilverPerHour),
			fmtInt64(z.GatherValue), fmtInt64(z.GatherPerHour),
			fmtInt64(z.Fame), fmtInt64(z.FamePerHour),
			fmtInt(z.EventCount), fmtBool(z.InsufficientData),
		})
	}
	return header, rows
}

// MarketRows renders the valued-items dataset (contract §2 market).
func MarketRows(items []model.LiveViewItem) ([]string, [][]string) {
	header := []string{"item", "uniqueName", "tier", "enchant", "quality", "count",
		"value", "valueSource", "stale", "lastSeen"}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		value := ""
		if it.Valuation.Amount > 0 {
			value = fmtInt64(it.Valuation.Amount)
		}
		rows = append(rows, []string{
			it.Item.DisplayName, it.Item.UniqueName,
			fmtInt(it.Item.Tier), fmtInt(it.Item.Enchant), fmtInt(it.Item.Quality),
			fmtInt(it.Count), value, string(it.Valuation.Source),
			fmtBool(it.Valuation.Stale), fmtTime(it.LastSeen),
		})
	}
	return header, rows
}

// SpecRows renders the Destiny Board dataset (contract §2 spec).
func SpecRows(masteries []model.MasteryLevel) ([]string, [][]string) {
	header := []string{"id", "name", "category", "line", "slot", "base", "level",
		"progress", "fame", "fameToMax", "touched"}
	rows := make([][]string, 0, len(masteries))
	for _, m := range masteries {
		rows = append(rows, []string{
			fmtInt(m.Index), m.Name, m.Category, m.Subcategory, m.Slot, fmtBool(m.Base),
			fmtInt(m.Level), strconv.FormatFloat(m.Progress, 'f', 2, 64),
			fmtInt64(m.Fame), fmtInt64(m.FameToMax), fmtBool(m.Touched),
		})
	}
	return header, rows
}
