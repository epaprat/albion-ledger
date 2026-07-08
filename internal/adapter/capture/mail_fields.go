package capture

import "strings"

// Mail extractors (feature 017): the marketplace order-fill mail chain.
//
//	R:174 GetMailInfos — the mailbox list: parallel arrays (ids ∥ locations ∥ types ∥
//	  received). Feeds the id→type correlation cache.
//	R:176 ReadMail — one opened mail: k0 = mail id, k1 = body (pipe-delimited payload).
//
// KEY-INDEX SKEW: the two vendored reference clients disagree on the GetMailInfos array
// keys (C# locations=7/types=11/received=12 vs Go locations=6/types=10/received=11) —
// Photon key indices shift per patch. So we resolve by CONTENT SIGNATURE, not by index
// (Principle IV, the width/shape-lock discipline), and only the mail-id array is taken by
// its key (3 — the one index both references agree on). Live-confirm via `probe
// --dumpall 174,176` (quickstart) validates the signature resolution.

// MailInfo is one row of the GetMailInfos list. Type is the raw wire string (the app
// layer maps it to a mailtrade.MailType). Received is an opaque wire timestamp (.NET
// ticks on live traffic — used only for newest-first ordering, not date formatting).
type MailInfo struct {
	ID         int64
	Type       string
	LocationID string
	Received   int64
}

// mailTypePrefixes mark a marketplace/blackmarket trade mail in the Types array.
var mailTypePrefixes = []string{"MARKETPLACE_", "BLACKMARKET_"}

func isMailType(s string) bool {
	for _, p := range mailTypePrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// MailInfos decodes the GetMailInfos list, shape-tolerant to key-index skew. Returns
// ok=false when no marketplace-type array is present (a non-trade mail packet) or the
// id array is missing. Parallel arrays are aligned to their shortest common length so a
// truncated field never indexes out of range.
func MailInfos(params map[byte]interface{}) ([]MailInfo, bool) {
	// Locate the two string arrays by content: one holds mail-type strings, the other
	// location ids. typesKey lets us pick the *other* string array as locations.
	var types, locations []string
	var typesKey int = -1
	for k, v := range params {
		s, ok := v.([]string)
		if !ok || len(s) == 0 {
			continue
		}
		if typesKey == -1 && hasAnyMailType(s) {
			types, typesKey = s, int(k)
		}
	}
	if types == nil {
		return nil, false
	}
	for k, v := range params {
		if int(k) == typesKey {
			continue
		}
		if s, ok := v.([]string); ok && len(s) > 0 {
			locations = s
			break
		}
	}

	// Mail ids ride key 3 in both references (the one index they agree on).
	ids := int64Slice(params[3])
	if ids == nil {
		return nil, false
	}
	// Received/Expires: any OTHER int array whose values look like unix timestamps.
	received := timestampSlice(params, 3)

	n := min2(len(ids), len(types))
	out := make([]MailInfo, 0, n)
	for i := 0; i < n; i++ {
		mi := MailInfo{ID: ids[i], Type: types[i]}
		if i < len(locations) {
			mi.LocationID = locations[i]
		}
		if i < len(received) {
			mi.Received = received[i]
		}
		out = append(out, mi)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// ReadMail decodes an opened mail: k0 = id (width-free int), k1 = body string.
func ReadMail(params map[byte]interface{}) (id int64, body string, ok bool) {
	id, iok := int64Val(params[0])
	if !iok {
		return 0, "", false
	}
	b, bok := params[1].(string)
	if !bok || b == "" {
		return 0, "", false
	}
	return id, b, true
}

func hasAnyMailType(s []string) bool {
	for _, x := range s {
		if isMailType(x) {
			return true
		}
	}
	return false
}

// timestampFloor: only .NET-tick-scale values (~6e17) are treated as mail timestamps.
// This deliberately excludes a stray int64 array of ids/silver (which sits far below tick
// scale) so it can't be mistaken for the received time; a server that sends seconds/ms
// instead simply falls back to capture time (received is display/ordering only).
const timestampFloor = 1_000_000_000_000_000 // 1e15

// timestampSlice returns the received-time array: among the tick-scale int64 arrays (other
// than the id array), the one with the SMALLEST first value — a mail carries a received
// time and, sometimes, a later expiry; received is always the earlier of the two.
func timestampSlice(params map[byte]interface{}, excludeKey int) []int64 {
	var best []int64
	for k, v := range params {
		if int(k) == excludeKey {
			continue
		}
		s := int64Slice(v)
		if len(s) > 0 && s[0] > timestampFloor && (best == nil || s[0] < best[0]) {
			best = s
		}
	}
	return best
}

// netTicksEpoch is the number of .NET DateTime ticks (100ns) at the unix epoch.
const netTicksEpoch = 621355968000000000

// MailReceivedMs normalizes a mail's wire timestamp to unix milliseconds so it orders
// against live trades (which use wall-clock ms). Albion sends .NET DateTime ticks; older
// captures used unix seconds. Returns 0 when the value isn't a plausible timestamp (the
// caller then falls back to capture time).
func MailReceivedMs(raw int64) int64 {
	switch {
	case raw > timestampFloor: // .NET ticks (100ns since year 1) — same 1e15 threshold the
		return (raw - netTicksEpoch) / 10000 // read-time heal (normalizeTradeMs) uses
	case raw > 1e12: // already unix ms
		return raw
	case raw > 1e9: // unix seconds
		return raw * 1000
	default:
		return 0
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
