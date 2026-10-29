//go:build !windows

package util

import "os/exec"

// applyBreakaway is a no-op on non-Windows platforms: Job Objects and
// CREATE_BREAKAWAY_FROM_JOB are Windows-specific.
func applyBreakaway(cmd *exec.Cmd) {}

// killTree is a no-op off Windows (exec.CommandContext kills the direct child).
type killTree struct{}

func newKillTree() *killTree   { return &killTree{} }
func (k *killTree) assign(int) {}
func (k *killTree) terminate() {}
func (k *killTree) close()     {}
