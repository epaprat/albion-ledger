package capture

// Wallet extractors (feature 016): the player's liquid silver balance. Two sources,
// newest-wins in the handler:
//
//	E:81 UpdateMoney (EVENT, live on every balance change): k1 = wallet total,
//	  fixed-point ×10000 like every other silver on the wire (vault K:5, EMV…).
//	R:2 Join (RESPONSE, login snapshot, guardKey 55): k33 = the login wallet value,
//	  same ×10000 scale — a seed until the first live E:81.
//
// E:81 (event) must not be confused with R:81 (response, market buy orders) — the
// classifier routes by (kind, code), so they stay separate.

// walletScale is the wire fixed-point for silver (mirrors app.silverScale). Kept local
// so the capture layer stays free of an app-package import.
const walletScale = 10000

// int64Val reads a wire integer of any width as int64 (Photon hands ints back as
// byte/int16/int32/int64 depending on magnitude; a large wallet arrives as int64).
func int64Val(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case byte:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	default:
		return 0, false
	}
}

// WalletBalance parses E:81 (event): k1 = wallet total ×10000 → real silver. Rejects a
// non-integer or negative value (a wallet is never negative; a bad packet is dropped).
func WalletBalance(params map[byte]interface{}) (int64, bool) {
	raw, ok := int64Val(params[1])
	if !ok || raw < 0 {
		return 0, false
	}
	return raw / walletScale, true
}

// JoinWallet parses the login wallet seed from the R:2 Join response: k33 = wallet
// value ×10000. Absent/invalid → ok=false (the Join simply carries no wallet).
func JoinWallet(params map[byte]interface{}) (int64, bool) {
	raw, ok := int64Val(params[33])
	if !ok || raw < 0 {
		return 0, false
	}
	return raw / walletScale, true
}
