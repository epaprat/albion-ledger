package wailsadapter

// realUser resolution order (013 fix): SUDO_USER → SUDO_UID → (root-only console
// fallback). The bug that dropped exports into /var/root/Documents was realUser
// returning false when SUDO_USER was empty; these pin the env recovery paths.

import (
	"os/user"
	"testing"
)

func TestRealUserFromSudoUser(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip("no current user")
	}
	t.Setenv("SUDO_USER", me.Username)
	t.Setenv("SUDO_UID", "")
	u, ok := realUser()
	if !ok || u.Username != me.Username {
		t.Fatalf("SUDO_USER=%q must resolve, got ok=%v u=%+v", me.Username, ok, u)
	}
}

func TestRealUserFromSudoUID(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skip("no current user")
	}
	t.Setenv("SUDO_USER", "") // empty → fall through to SUDO_UID
	t.Setenv("SUDO_UID", me.Uid)
	u, ok := realUser()
	if !ok || u.Uid != me.Uid {
		t.Fatalf("SUDO_UID=%q must resolve, got ok=%v u=%+v", me.Uid, ok, u)
	}
}

func TestRealUserIgnoresRootSudoValues(t *testing.T) {
	// SUDO_USER=root / SUDO_UID=0 are not a real human; when not running as root
	// (the test process), realUser must report no override rather than "root".
	t.Setenv("SUDO_USER", "root")
	t.Setenv("SUDO_UID", "0")
	if u, ok := realUser(); ok {
		t.Fatalf("root sudo values must not resolve a real user, got %+v", u)
	}
}
