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
)

type entry struct {
	Index      int    `json:"index"`
	UniqueName string `json:"uniqueName"`
	Name       string `json:"name"`
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
			name = m[2]
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
