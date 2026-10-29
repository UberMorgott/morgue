//go:build windows

package util

import (
	"os/exec"
	"syscall"
	"unsafe"

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

// killTree is a Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE (and no
// memory limit) for a breakaway child. Its descendants (cmd.exe -> java.exe
// for Ghidra) inherit it, so cancelling the step, the step ending, or morgue
// itself dying (the OS closes the handle) terminates the whole tree instead of
// leaving an orphaned multi-GB JVM. A descendant spawned in the instant before
// assign() runs is not covered; cmd.exe batch start-up makes that window far
// shorter than the JVM launch.
type killTree struct{ job windows.Handle }

func newKillTree() *killTree {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return &killTree{}
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	//nolint:gosec // G103: SetInformationJobObject takes the struct as a raw pointer + size.
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return &killTree{}
	}
	return &killTree{job: job}
}

func (k *killTree) assign(pid int) {
	if k.job == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid)) //nolint:gosec // G115: a Windows PID fits in uint32
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.AssignProcessToJobObject(k.job, h)
}

func (k *killTree) terminate() {
	if k.job != 0 {
		_ = windows.TerminateJobObject(k.job, 1)
	}
}

func (k *killTree) close() {
	if k.job != 0 {
		_ = windows.CloseHandle(k.job) // KILL_ON_JOB_CLOSE: ends any leftover descendant
		k.job = 0
	}
}
