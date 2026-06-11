//go:build !windows

package util

import "os/exec"

// HideCmdWindow is a no-op on non-Windows platforms: there is no console window
// to suppress.
func HideCmdWindow(cmd *exec.Cmd) {}
