//go:build !unix

package wailsadapter

import "os/user"

// consoleUser has no meaning off unix (the app targets macOS); the SUDO_* paths
// in realUser cover elevated launches elsewhere.
func consoleUser() (*user.User, bool) { return nil, false }
