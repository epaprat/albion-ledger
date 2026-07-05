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
	"strings"
)

type node struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`    // top breakdown (Combat, Gathering…)
	Subcategory string `json:"subcategory,omitempty"` // mid breakdown (Axes, Fiber…)
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
func subcategoryOf(id string) string {
	parts := strings.Split(id, "_")
	if len(parts) < 2 {
		return ""
	}
	return humanize(parts[1])
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
	var defs []struct{ id, category string }
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
			var id, cat string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "id":
					id = a.Value
				case "category":
					cat = a.Value
				}
			}
			if id != "" {
				defs = append(defs, struct{ id, category string }{id, cat})
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
	for i, d := range defs {
		out.Nodes = append(out.Nodes, node{
			ID:          i,
			Name:        humanize(d.id),
			Category:    categoryDisplay(d.category),
			Subcategory: subcategoryOf(d.id),
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
