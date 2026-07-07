// Command gen-spec-nodes turns the community game-data achievements dump into the
// embedded Destiny Board node-name catalog (feature 011).
//
// Input:  achievements.xml from ao-data/ao-bin-dumps (fetch it first):
//
//	curl -sL https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/achievements.xml -o /tmp/achievements.xml
//	go run ./scripts/gen-spec-nodes /tmp/achievements.xml > data/specnodes.json
//
// ID-ALIGNMENT ASSUMPTION (recorded, live-verified in the 011 quickstart): the
// numeric node ids on the wire (E:154 k1) index the achievements in DOCUMENT ORDER
// (0-based). If live verification disproves this, names fall back to "Node #N" and
// the alignment becomes tracked work — the catalog format itself stays valid.
//
// Names are humanized from the achievement id ("GATHER_FIBER_ADEPT" → "Gather Fiber
// Adept"); the game's localized display names live in a separate dump and can
// replace these later without a format change.
package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type node struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`    // top breakdown (Combat, Gathering…)
	Subcategory string `json:"subcategory,omitempty"` // mid breakdown (Axes, Fiber…)
	Slot        string `json:"slot,omitempty"`        // gear slot (Weapon/Off-Hand/Head/Chest/Shoes…)
	Base        bool   `json:"base,omitempty"`        // true = whole-line "Fighter" aggregate node (012)
	FameToMax   int64  `json:"fameToMax,omitempty"`   // total fame from 0 to level 100 (011)
}

func humanize(s string) string {
	words := strings.Split(strings.ToLower(s), "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// categoryDisplay maps the raw category code to the in-game top-breakdown name.
// humanizeAfterFirst humanizes an id after dropping its first (category) token:
// GATHER_FIBER_T5 → "Fiber T5", CRAFT_ARCANESTAFFS_ARCANE → "Arcanestaffs Arcane".
func humanizeAfterFirst(id string) string {
	parts := strings.SplitN(id, "_", 2)
	if len(parts) < 2 {
		return humanize(id)
	}
	return humanize(parts[1])
}

func categoryDisplay(cat string) string {
	switch cat {
	case "fighting":
		return "Combat"
	case "gathering":
		return "Gathering"
	case "crafting":
		return "Crafting"
	case "farming":
		return "Farming"
	case "tracking":
		return "Tracking"
	case "main":
		return "Adventurer"
	default:
		return humanize(cat)
	}
}

// subcategoryOf derives the mid-breakdown from the id's second token (the weapon /
// resource line): GATHER_FIBER_T3 → "Fiber", COMBAT_ARCANESTAFFS_ARCANE →
// "Arcanestaffs", FARM_ALCHEMIST_ACID → "Alchemist". Single-token ids → "".
// isCombatBase reports whether a fighting id is a base/aggregate "Fighter" node — the
// whole weapon line (COMBAT_<LINE>) or an armor base (COMBAT_<TYPE>_ARMORS/HEADS/SHOES)
// — rather than a specific item variant. Bases share their first variant's itemforsprite.
func isCombatBase(id string) bool {
	parts := strings.Split(id, "_")
	if len(parts) == 2 {
		return true // COMBAT_<LINE> weapon fighter
	}
	if len(parts) == 3 {
		switch parts[2] {
		case "ARMORS", "HEADS", "SHOES":
			return true // COMBAT_<TYPE>_<PIECE> armor base
		}
	}
	return false
}

// slotOf classifies a node into a gear slot for the at-a-glance Spec view (012).
// Armor lines split by piece (ARMORS→Chest, HEADS→Head, SHOES→Shoes); off-hand lines
// (Torches, Shields, Books/tomes) group together; every other combat line is a Weapon.
// Non-combat nodes take their category as the slot (Gathering/Crafting/Farming…).
func slotOf(id, catDisplay string) string {
	if catDisplay != "Combat" {
		return catDisplay
	}
	parts := strings.Split(id, "_")
	if len(parts) >= 3 {
		switch parts[2] {
		case "ARMORS":
			return "Chest"
		case "HEADS":
			return "Head"
		case "SHOES":
			return "Shoes"
		}
	}
	sub := ""
	if len(parts) >= 2 {
		sub = parts[1]
	}
	switch sub {
	case "TORCHES", "SHIELDS", "BOOKS":
		return "Off-Hand"
	case "CLOTH", "LEATHER", "PLATE":
		return "Armor" // base armor fighter node (no piece) — rare
	default:
		return "Weapon"
	}
}

func subcategoryOf(id string) string {
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return ""
	}
	return humanize(parts[1])
}

var tierPrefix = regexp.MustCompile(`^T\d+_`)

// lineName resolves a combat itemforsprite to its GENERIC line name by preferring the
// LOWEST tier of the item family. Most weapons keep one name across tiers (Battleaxe),
// but some T8 items carry a special flavour name (T8_2H_AXE = "The Hand of Khor") while
// the line node is really "Greataxe" (T4_2H_AXE = "Adept's Greataxe"). Lower tiers hold
// the generic name, so we scan T2..T8 of the same family and take the first hit; the
// exact sprite is the fallback. Returns "" if nothing resolves.
func lineName(item string, names map[string]string) string {
	fam := tierPrefix.ReplaceAllString(item, "")
	for t := 2; t <= 8; t++ {
		if n, ok := names[fmt.Sprintf("T%d_%s", t, fam)]; ok {
			return n
		}
	}
	return names[item]
}

// loadItemNames reads the item catalog (2nd CLI arg, default data/items.json) and
// maps uniqueName → base display name (the "Xxx's " tier/enchant prefix stripped),
// which is what the in-game Destiny Board shows for combat/gather/craft nodes.
func loadItemNames(args []string) map[string]string {
	path := "data/items.json"
	if len(args) > 2 {
		path = args[2]
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: items.json unavailable, using humanized ids:", err)
		return map[string]string{}
	}
	var f struct {
		Items []struct {
			UniqueName string `json:"uniqueName"`
			Name       string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		fmt.Fprintln(os.Stderr, "warning: items.json parse:", err)
		return map[string]string{}
	}
	m := make(map[string]string, len(f.Items))
	for _, it := range f.Items {
		name := it.Name
		if idx := strings.Index(name, "'s "); idx != -1 {
			name = name[idx+3:]
		}
		m[it.UniqueName] = name
	}
	return m
}

// loadTemplateFame sums the per-level Fame column of every <template>'s <baselevels>
// block → total fame from 0 to level 100 (multiplied by the node's famemultiplier at
// use). The Fame column is the FIRST in the ";"-separated structure (011).
func loadTemplateFame(raw []byte) map[string]int64 {
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	out := map[string]int64{}
	var curTemplate string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "template" {
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						curTemplate = a.Value
					}
				}
			}
		case xml.CharData:
			if curTemplate == "" {
				continue
			}
			var sum int64
			for _, line := range strings.Split(string(t), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				fields := strings.Split(line, ";")
				if n, err := strconv.ParseInt(strings.TrimSpace(fields[0]), 10, 64); err == nil {
					sum += n
				}
			}
			if sum > 0 {
				out[curTemplate] = sum
			}
		case xml.EndElement:
			if t.Name.Local == "template" {
				curTemplate = ""
			}
		}
	}
	return out
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-spec-nodes <achievements.xml>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Stream in DOCUMENT ORDER: both <achievement> and <templateachievement> are node
	// definitions (carry id+category); ids nested under <parentachievements> are refs,
	// not defs — skip that subtree. Two-struct unmarshalling would lose document order,
	// which the wire-index alignment assumption depends on.
	dec := xml.NewDecoder(strings.NewReader(string(raw)))
	var version string
	var defs []struct{ id, category, item, tmpl string; fameMult float64 }
	depthUnderParent := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			if ee, ok := tok.(xml.EndElement); ok && ee.Name.Local == "parentachievements" && depthUnderParent > 0 {
				depthUnderParent--
			}
			continue
		}
		switch se.Name.Local {
		case "achievements":
			for _, a := range se.Attr {
				if a.Name.Local == "Version" {
					version = a.Value
				}
			}
		case "parentachievements":
			depthUnderParent++
		case "achievement", "templateachievement":
			if depthUnderParent > 0 {
				continue // a reference, not a definition
			}
			var id, cat, item, tmpl string
			fameMult := 1.0
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "id":
					id = a.Value
				case "category":
					cat = a.Value
				case "itemforsprite":
					item = a.Value
				case "usetemplate":
					tmpl = a.Value
				case "famemultiplier":
					if v, err := strconv.ParseFloat(a.Value, 64); err == nil {
						fameMult = v
					}
				}
			}
			if id != "" {
				defs = append(defs, struct{ id, category, item, tmpl string; fameMult float64 }{id, cat, item, tmpl, fameMult})
			}
		}
	}
	out := struct {
		Comment string `json:"_comment"`
		Nodes   []node `json:"nodes"`
	}{
		Comment: fmt.Sprintf("Destiny Board node names from ao-bin-dumps achievements.xml (version %s). "+
			"id = DOCUMENT-ORDER index (0-based) over <achievement>+<templateachievement> defs — the wire "+
			"E:154 alignment assumption, live-verified per 011 quickstart. Regenerate: scripts/gen-spec-nodes.", version),
	}
	tmplFame := loadTemplateFame(raw)
	itemNames := loadItemNames(os.Args)
	for i, d := range defs {
		// Combat weapons/armor: itemforsprite IS the node's item ("Scholar Robe",
		// "Arcane Staff"). Gathering/crafting/farming point itemforsprite at the TOOL
		// (a sickle for every fiber tier), so there the id — minus its redundant
		// category prefix — is the readable, tier-distinct name ("Fiber T5").
		name := humanizeAfterFirst(d.id)
		// Base/aggregate combat nodes (the whole-line "Fighter" node — COMBAT_<LINE>,
		// and the armor bases COMBAT_<TYPE>_ARMORS/HEADS/SHOES) carry the itemforsprite
		// of their first variant, so itemforsprite naming collides them with a real
		// item (the "Books Fighter" base rendered as "Diary"). Name those from the id
		// with a "(Fighter)" tag instead; only the SPECIFIC variant nodes use the item.
		if d.category == "fighting" && d.item != "" && !isCombatBase(d.id) {
			if n := lineName(d.item, itemNames); n != "" {
				name = n
			}
		} else if d.category == "fighting" && isCombatBase(d.id) {
			name = humanizeAfterFirst(d.id) + " (Fighter)"
		}
		var fameToMax int64
		if total, ok := tmplFame[d.tmpl]; ok {
			fameToMax = int64(float64(total) * d.fameMult)
		}
		catDisp := categoryDisplay(d.category)
		out.Nodes = append(out.Nodes, node{
			ID:          i,
			Name:        name,
			Category:    catDisp,
			Subcategory: subcategoryOf(d.id),
			Slot:        slotOf(d.id, catDisp),
			Base:        catDisp == "Combat" && isCombatBase(d.id),
			FameToMax:   fameToMax,
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
