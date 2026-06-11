//go:build windows

package util

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// HideCmdWindow prevents a spawned console process from popping a console
// window (CREATE_NO_WINDOW) and hides any window it would otherwise show. This
// stops the flurry of terminal windows that would appear when the GUI shells out
// to CLI tools (version probes, decompilers, dotnet, …). Existing SysProcAttr
// fields are preserved; flags are OR'd into CreationFlags. No-op on non-Windows.
func HideCmdWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
