package capture

import "testing"

func TestWalletBalance(t *testing.T) {
	// k1 = 8,470,000,000 (×10000) → 847,000 silver. Large value arrives as int64.
	if s, ok := WalletBalance(map[byte]interface{}{1: int64(8_470_000_000)}); !ok || s != 847_000 {
		t.Fatalf("E:81 wallet decode wrong: %d ok=%v", s, ok)
	}
	// Width-free: a small balance may come as int32.
	if s, ok := WalletBalance(map[byte]interface{}{1: int32(5_826_0000)}); !ok || s != 5_826 {
		t.Fatalf("int32 wallet decode wrong: %d ok=%v", s, ok)
	}
	// Negative or wrong type → rejected.
	if _, ok := WalletBalance(map[byte]interface{}{1: int64(-1)}); ok {
		t.Fatal("negative wallet must be rejected")
	}
	if _, ok := WalletBalance(map[byte]interface{}{1: "nope"}); ok {
		t.Fatal("non-integer wallet must be rejected")
	}
	if _, ok := WalletBalance(map[byte]interface{}{}); ok {
		t.Fatal("missing k1 must be rejected")
	}
}

func TestJoinWallet(t *testing.T) {
	// R:2 k33 = login wallet ×10000.
	if s, ok := JoinWallet(map[byte]interface{}{33: int64(1_234_560_000)}); !ok || s != 123_456 {
		t.Fatalf("R:2 k33 wallet decode wrong: %d ok=%v", s, ok)
	}
	if _, ok := JoinWallet(map[byte]interface{}{55: []int{1}}); ok {
		t.Fatal("Join without k33 must be rejected")
	}
}
