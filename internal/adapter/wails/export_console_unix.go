//go:build unix

package wailsadapter

// consoleUser resolves the logged-in GUI user as the owner of /dev/console. On
// macOS (and other unixes) the active console/session device is owned by the
// human at the keyboard, so when the app runs as root without any SUDO_* context
// this recovers the real user for export paths + chown. Owner root (no human
// session) or any lookup failure → (nil,false), and the caller degrades safely.

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func consoleUser() (*user.User, bool) {
	fi, err := os.Stat("/dev/console")
	if err != nil {
		return nil, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil, false
	}
	u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return nil, false
	}
	return u, true
}
