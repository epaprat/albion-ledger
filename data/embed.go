// Package data provides the bundled default catalog and code map, embedded into
// the binary. Both are overridable at runtime (FR-012: game-change agility).
package data

import _ "embed"

//go:embed items.json
var ItemsJSON []byte

//go:embed codes.json
var CodesJSON []byte

//go:embed clusters.json
var ClustersJSON []byte

//go:embed specnodes.json
var SpecNodesJSON []byte
