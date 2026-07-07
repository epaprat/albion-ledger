// Command gen-catalog transforms ao-bin-dumps formatted/items.txt
// (lines "INDEX: UNIQUENAME : LOCALIZED_NAME") into data/items.json.
//
// Regenerate after a game patch:
//
//	curl -sL https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/formatted/items.txt -o /tmp/items.txt
//	go run ./scripts/gen-catalog /tmp/items.txt data/items.json
//
// This is the "game patched, names changed" runbook step (FR-012).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type entry struct {
	Index      int    `json:"index"`
	UniqueName string `json:"uniqueName"`
	Name       string `json:"name"`
}

var tierPrefix = regexp.MustCompile(`^[Tt]\d+_`)

// prettyName turns a bare unique id into a readable display name when the dump has
// no localized name: drop the "@<n>" enchant suffix and the "T<n>_" tier prefix
// (tier/enchant are shown separately), swap underscores for spaces, Title Case.
// e.g. "T6_RANDOM_DUNGEON_SOLO_TOKEN_D1@1" → "Random Dungeon Solo Token D1".
func prettyName(unique string) string {
	s := unique
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	s = tierPrefix.ReplaceAllString(s, "")
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
	if s == "" {
		return unique // nothing left to show → keep the raw id
	}
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

// Two line shapes in items.txt: "<idx>: <UNIQUENAME> : <Localized Name>" and
// "<idx>: <UNIQUENAME>" (no localized name — e.g. dungeon tokens). The localized
// part is optional; when absent the uniquename becomes the display name (below),
// so those items still resolve instead of dropping to "Unknown item #N".
var line = regexp.MustCompile(`^\s*(\d+):\s*(\S+)(?:\s*:\s*(.+?))?\s*$`)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen-catalog <items.txt> <out.json>")
		os.Exit(2)
	}
	in, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer in.Close()

	var items []entry
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		m := line.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		idx, _ := strconv.Atoi(m[1])
		name := m[3]
		if name == "" {
			name = prettyName(m[2]) // no localized name → readable name from the unique id
		}
		items = append(items, entry{Index: idx, UniqueName: m[2], Name: name})
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	out, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer out.Close()
	enc := json.NewEncoder(out)
	enc.SetIndent("", "")
	wrapped := struct {
		Comment string  `json:"_comment"`
		Items   []entry `json:"items"`
	}{
		Comment: "Generated from ao-bin-dumps formatted/items.txt by scripts/gen-catalog. Numeric packet index -> item identity. Regenerate after a patch (see scripts/gen-catalog).",
		Items:   items,
	}
	if err := enc.Encode(wrapped); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d items to %s\n", len(items), os.Args[2])
}
