//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// ATTACH_PARENT_PROCESS tells AttachConsole to attach to the console of the
// parent process. Not exported by x/sys/windows, so we define it here.
// https://learn.microsoft.com/en-us/windows/console/attachconsole
const attachParentProcess = ^uint32(0) // (DWORD)-1

var (
	modkernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = modkernel32.NewProc("AttachConsole")
)

// attachParentConsole reattaches stdout/stderr/stdin to the parent terminal's
// console when morgue runs the CLI path.
//
// The release binary is linked with -H=windowsgui (no console subsystem), so a
// CLI invocation from an existing terminal would otherwise have no stdio and
// print nothing. AttachConsole(ATTACH_PARENT_PROCESS) borrows the calling
// terminal's console; we then re-open CONOUT$/CONIN$ and rewire both the Go
// os.Std* files and the Win32 standard handles so cobra output lands in that
// terminal.
//
// It is intentionally best-effort and never fatal:
//   - If stdout is ALREADY a valid handle the launcher redirected it to a
//     pipe/file (e.g. `morgue run --quiet > out.json`, or PowerShell
//     Start-Process -RedirectStandardOutput) or a newer Go runtime already
//     wired it up. We MUST NOT rebind it — pointing it at CONOUT$ would send the
//     output to the terminal instead of the redirect target and silently drop
//     the machine-readable result (the exact v0.4.5 regression). So we no-op.
//   - In a console-subsystem dev build the process already owns a console, so
//     AttachConsole fails (ERROR_ACCESS_DENIED) — we ignore it and keep the
//     console we already have.
//   - When launched detached (no parent console, e.g. double-click) it fails
//     (ERROR_INVALID_HANDLE) — output simply goes nowhere, which is acceptable.
func attachParentConsole() {
	if stdHandleValid(windows.STD_OUTPUT_HANDLE) {
		// Output is redirected to a pipe/file (or already wired by the runtime).
		// Leave the std handles untouched so the redirect target keeps the data.
		return
	}

	r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess))
	if r == 0 {
		// No parent console, or we already own one: nothing to rewire.
		return
	}

	// Reopen the freshly attached console for output and input and point both
	// the Go runtime's os.Std* and the Win32 standard handles at it.
	if out, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	); err == nil {
		windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, out)
		windows.SetStdHandle(windows.STD_ERROR_HANDLE, out)
		f := os.NewFile(uintptr(out), "CONOUT$")
		os.Stdout = f
		os.Stderr = f
	}

	if in, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONIN$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	); err == nil {
		windows.SetStdHandle(windows.STD_INPUT_HANDLE, in)
		os.Stdin = os.NewFile(uintptr(in), "CONIN$")
	}
}

// stdHandleValid reports whether the given standard handle (one of the
// STD_*_HANDLE ids) is already usable — i.e. the launcher redirected it to a
// pipe/file or the Go runtime synthesized one for the GUI-subsystem binary.
func stdHandleValid(id uint32) bool {
	h, err := windows.GetStdHandle(id)
	return err == nil && h != 0 && h != windows.InvalidHandle
}
