// Package locations resolves a raw Photon cluster id (Join key 8, e.g. "4207") to a
// readable zone name (e.g. "Pen Gent") from the bundled cluster map. Pure domain, no
// infrastructure deps (Principle I). Overridable/regeneratable via scripts/gen-clusters.
package locations

import "encoding/json"

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

// Resolve returns the zone name for a cluster id. Unknown ids return the raw id (so a
// missing mapping still yields a stable, groupable label — named later via a patch).
func (l *Locations) Resolve(clusterID string) string {
	if name, ok := l.clusters[clusterID]; ok && name != "" {
		return name
	}
	return clusterID
}
