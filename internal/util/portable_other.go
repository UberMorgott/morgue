//go:build !windows

package util

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with this PID is still running.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// trackEula is a no-op off Windows (Sysinternals EULA keys are Windows-only).
func trackEula(string, []string) func() { return func() {} }
