// Package locations resolves a raw Photon cluster id (Join key 8, e.g. "4207") to a
// readable zone name (e.g. "Pen Gent") from the bundled cluster map, with a token
// fallback for GENERATED instance ids (corrupted dungeons, mists, hellgates … carry a
// per-instance GUID like "@CORRUPTEDDUNGEON@4d5f…" that no catalog can list — mirror
// of the reference client's GeneratedLocationTypes). Pure domain (Principle I).
package locations

import (
	"encoding/json"
	"strings"
)

// generatedTypes maps instance-id tokens to friendly names. ORDER MATTERS: more
// specific tokens first (MISTSDUNGEON before MISTS).
var generatedTypes = []struct{ token, friendly string }{
	{"HELLCLUSTER", "Hellgate"},
	{"RANDOMDUNGEON", "Dungeon"},
	{"CORRUPTEDDUNGEON", "Corrupted Dungeon"},
	{"EXPEDITION", "Expedition"},
	{"ARENA", "Arena"},
	{"MISTSDUNGEON", "Mists Dungeon"},
	{"MISTS", "Mists"},
	{"HELLDUNGEON", "Abyssal Depths"},
	{"ISLAND", "Island"},
}

// Friendly maps a generated instance id (contains '@') to its readable type name
// ("Corrupted Dungeon"); anything else returns unchanged. Also cleans labels that
// were persisted raw before this fallback existed, so it is safe at display time.
func Friendly(id string) string {
	if !strings.Contains(id, "@") {
		return id
	}
	upper := strings.ToUpper(id)
	for _, g := range generatedTypes {
		if strings.Contains(upper, g.token) {
			return g.friendly
		}
	}
	return id
}

// Locations is a bundled cluster-id → zone-name map.
type Locations struct {
	clusters map[string]string
}

// New parses a clusters.json document ({"clusters": {"4207": "Pen Gent", ...}}).
func New(data []byte) (*Locations, error) {
	var doc struct {
		Clusters map[string]string `json:"clusters"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Clusters == nil {
		doc.Clusters = map[string]string{}
	}
	return &Locations{clusters: doc.Clusters}, nil
}

// Resolve returns the zone name for a cluster id: exact catalog hit, else the raw id
// UNCHANGED. Generated instance ids ("@CORRUPTEDDUNGEON@<guid>") deliberately stay raw
// here — the raw id is what gets PERSISTED, and coarsening at write time would
// irreversibly merge distinct instance runs. Display/analytics layers apply Friendly
// at read time instead (lossless grouping).
func (l *Locations) Resolve(clusterID string) string {
	if name, ok := l.clusters[clusterID]; ok && name != "" {
		return name
	}
	return clusterID
}
