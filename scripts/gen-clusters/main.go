// Command gen-clusters transforms ao-bin-dumps formatted/world.json (a list of
// {Index, UniqueName}) into data/clusters.json — a cluster-id → zone-name map used
// to turn raw Join key-8 cluster ids (e.g. "4207") into readable zone names
// (e.g. "Pen Gent") for the flow zone label (feature 005 / analytics 006).
//
// Regenerate after a game patch:
//
//	curl -sL https://raw.githubusercontent.com/ao-data/ao-bin-dumps/master/formatted/world.json -o /tmp/world.json
//	go run ./scripts/gen-clusters /tmp/world.json data/clusters.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type location struct {
	Index      string `json:"Index"`
	UniqueName string `json:"UniqueName"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: gen-clusters <world.json> <out.json>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var locs []location
	if err := json.Unmarshal(raw, &locs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	clusters := make(map[string]string, len(locs))
	for _, l := range locs {
		if l.Index == "" || l.UniqueName == "" {
			continue
		}
		clusters[l.Index] = l.UniqueName
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
		Comment  string            `json:"_comment"`
		Clusters map[string]string `json:"clusters"`
	}{
		Comment:  "Generated from ao-bin-dumps formatted/world.json by scripts/gen-clusters. Cluster id -> zone name (Join key 8). Regenerate after a patch.",
		Clusters: clusters,
	}
	if err := enc.Encode(wrapped); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d clusters to %s\n", len(clusters), os.Args[2])
}
