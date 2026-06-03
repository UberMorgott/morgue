//go:build !windows

package util

import "os/exec"

// applyBreakaway is a no-op on non-Windows platforms: Job Objects and
// CREATE_BREAKAWAY_FROM_JOB are Windows-specific.
func applyBreakaway(cmd *exec.Cmd) {}
