package capture

// Destiny Board extractors (feature 011). Live-decoded from join + long-session
// pcaps (2026-07-05; E:153 confirmed over 1914 samples, E:152 over 68):
//
//	E:154 FullAchievementProgressInfo (every Join, full self snapshot):
//	  k0 = self object id, k1 = node ids, k2 = levels, k3 = next-level progress
//	  (float 0-1), k4 = "[[<totalFame>]]" strings — parallel arrays.
//	E:153 AchievementProgressInfo (live delta): k0 self, k1 node id, k2 level
//	  (OPTIONAL — absent means unchanged), k3 progress, k4 fame string.
//	E:152 FinishedAchievement (level-up): k0 self, k1 node id, k2 new level.
//
// Every integer array is read width-free (the Photon trap hit five keys in 010);
// short parallel arrays leave the missing field zero rather than rejecting the row.

import (
	"strconv"
	"strings"
)

const maxSpecNodes = 2048

// SpecNodeUpdate is one parsed node row (level -1 = "not provided").
type SpecNodeUpdate struct {
	ID          int
	Level       int
	HasLevel    bool
	Progress    float64
	HasProgress bool
	Fame        int64
	HasFame     bool
}

// SpecSelf pulls the self object id shared by all three messages (k0). ok=false
// gates the self-filter upstream.
func SpecSelf(params map[byte]interface{}) (int, bool) {
	return toIntVal(params[0])
}

// AchievementSnapshot parses E:154. k1 (node ids) is required and capped; the other
// parallel arrays may be shorter (missing field → zero).
func AchievementSnapshot(params map[byte]interface{}) ([]SpecNodeUpdate, bool) {
	ids := byteOrIntSlice(params[1])
	if len(ids) == 0 || len(ids) > maxSpecNodes {
		return nil, false
	}
	levels := byteOrIntSlice(params[2])
	progress := float32Slice(params[3])
	fames := stringSlice(params[4])
	out := make([]SpecNodeUpdate, len(ids))
	for i, id := range ids {
		u := SpecNodeUpdate{ID: id}
		if i < len(levels) {
			u.Level, u.HasLevel = levels[i], true
		}
		if i < len(progress) {
			u.Progress, u.HasProgress = clamp01(float64(progress[i])), true
		}
		if i < len(fames) {
			u.Fame, u.HasFame = parseFameWrapper(fames[i]), true
		}
		out[i] = u
	}
	return out, true
}

// AchievementDelta parses E:153 (single node). Level is optional.
func AchievementDelta(params map[byte]interface{}) (SpecNodeUpdate, bool) {
	id, ok := toIntVal(params[1])
	if !ok {
		return SpecNodeUpdate{}, false
	}
	u := SpecNodeUpdate{ID: id}
	if lvl, ok := toIntVal(params[2]); ok {
		u.Level, u.HasLevel = lvl, true
	}
	if p, ok := firstFloat(params[3]); ok {
		u.Progress, u.HasProgress = clamp01(float64(p)), true
	}
	switch f := params[4].(type) {
	case string:
		u.Fame, u.HasFame = parseFameWrapper(f), true
	case []string:
		if len(f) > 0 {
			u.Fame, u.HasFame = parseFameWrapper(f[0]), true
		}
	}
	return u, true
}

// SpecUnlockedIDs pulls the full unlocked-node id list from E:155 (k1). A node here
// but NOT in the current E:154 in-progress snapshot is MAXED (level 100).
func SpecUnlockedIDs(params map[byte]interface{}) []int {
	ids := byteOrIntSlice(params[1])
	if len(ids) > maxSpecNodes {
		return nil
	}
	return ids
}

// AchievementDone parses E:152 (level-up): node id + new level, both required.
func AchievementDone(params map[byte]interface{}) (id, level int, ok bool) {
	id, iok := toIntVal(params[1])
	level, lok := toIntVal(params[2])
	return id, level, iok && lok
}

// parseFameWrapper extracts the number from the game's "[[<fame>]]" localization
// wrapper; anything else yields 0 (the row still lists — spec rule 6).
func parseFameWrapper(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// float32Slice reads a float array tolerantly across wire widths (progress has been
// sampled as []float32, but the Photon double type would arrive []float64).
func float32Slice(v interface{}) []float32 {
	switch a := v.(type) {
	case []float32:
		return a
	case []float64:
		out := make([]float32, len(a))
		for i, f := range a {
			out[i] = float32(f)
		}
		return out
	}
	return nil
}

func firstFloat(v interface{}) (float32, bool) {
	switch a := v.(type) {
	case float32:
		return a, true
	case float64:
		return float32(a), true
	case []float32:
		if len(a) > 0 {
			return a[0], true
		}
	case []float64:
		if len(a) > 0 {
			return float32(a[0]), true
		}
	}
	return 0, false
}

func stringSlice(v interface{}) []string {
	if a, ok := v.([]string); ok {
		return a
	}
	return nil
}
