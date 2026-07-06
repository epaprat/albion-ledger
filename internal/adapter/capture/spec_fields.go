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
//	E:1 FullAchievementInfo (COLD login/relog full board — the complete authority,
//	  012): k0 self, k1 anchors, k2 = []int16 NODE IDS (present on cold login;
//	  omitted on warm logins where k3 is position-indexed by the last k2 order),
//	  k3 = []uint8 LEVELS incl 100, k4 flags. k2[i] has level k3[i]. E:1 collides
//	  with chat-settings E:1 (k0 []string) — reject by shape.
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

// FullBoard is one E:1 board decode: paired node ids + levels. When the wire carried
// the id list (k2), Ids is populated and IdsFromWire is true (a fresh authoritative
// enumeration to persist). On a warm login k2 is absent — Ids is nil and the caller
// maps Levels by the persisted enumeration.
type FullBoard struct {
	Self        int
	Ids         []int // k2 node ids, position-aligned with Levels; nil if k2 absent
	IdsFromWire bool
	Levels      []int // k3 levels (incl 100 = maxed)
}

// minBoardNodes is the shape-lock floor for E:1. The real Destiny Board is ~495-697
// nodes; any code-1 event with a much shorter int array at k3 is NOT the board (a
// truncated/hostile packet, or some other event that happens to share event code 1).
// Rejecting them keeps a stray packet from collapsing the board and persisting a
// truncated enumeration (Principle IV/XI: hostile-input-safe).
const minBoardNodes = 256

// AchievementFullBoard parses E:1 (012). Shape-locked against the chat-settings E:1 and
// any other code-1 traffic: k0 must be an integer self id, k3 a level array of at least
// minBoardNodes entries (the board is always full-size), and k4 the board's []bool flag
// array (present on every real E:1). k2, when a []int16 of the same length as k3, is the
// authoritative node-id enumeration; any other length is ignored (warm-login shape).
func AchievementFullBoard(params map[byte]interface{}) (FullBoard, bool) {
	self, ok := toIntVal(params[0])
	if !ok {
		return FullBoard{}, false // chat-settings E:1 has k0 []string — rejected
	}
	if _, isFlags := params[4].([]bool); !isFlags {
		return FullBoard{}, false // real E:1 always carries the k4 []bool flag array
	}
	levels := byteOrIntSlice(params[3])
	if len(levels) < minBoardNodes || len(levels) > maxSpecNodes {
		return FullBoard{}, false // too short/long to be the board
	}
	fb := FullBoard{Self: self, Levels: levels}
	if ids := byteOrIntSlice(params[2]); len(ids) == len(levels) {
		fb.Ids, fb.IdsFromWire = ids, true
	}
	return fb, true
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
