//go:build windows

package util

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// applyBreakaway sets CREATE_BREAKAWAY_FROM_JOB on cmd so the spawned process
// (and its descendants, e.g. Ghidra's JVM) are NOT assigned to morgue's Job
// Object and therefore escape the per-process memory cap. It requires the job
// to have been created with JOB_OBJECT_LIMIT_BREAKAWAY_OK (see LimitProcessMemory),
// otherwise CreateProcess fails with ERROR_ACCESS_DENIED. Existing SysProcAttr
// fields are preserved; the flag is OR'd into CreationFlags.
func applyBreakaway(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_BREAKAWAY_FROM_JOB
}
