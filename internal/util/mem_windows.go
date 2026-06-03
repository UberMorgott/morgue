//go:build windows

package util

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX structure.
// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/ns-sysinfoapi-memorystatusex
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
)

// TotalPhysicalMemoryBytes returns total physical RAM in bytes, or 0 if it
// cannot be determined.
func TotalPhysicalMemoryBytes() uint64 {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return 0
	}
	return ms.TotalPhys
}

// memLimitJob holds the Job Object handle for the process memory cap. It is kept
// alive for the lifetime of the process so the cap stays in force and the handle
// is not garbage-collected/closed. We deliberately never set
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so the cap simply lapses if the handle is
// ever released — it must never kill the morgue process on handle close.
//
// It is intentionally write-only: nothing reads it back; its sole job is to
// retain the handle. Hence the lint suppression.
//
//lint:ignore U1000 retained solely to keep the Job Object handle alive
var memLimitJob windows.Handle

// LimitProcessMemory caps the committed memory of the current process at the
// given number of bytes using a Windows Job Object
// (JOB_OBJECT_LIMIT_PROCESS_MEMORY). Once applied, an allocation that would push
// the process over the cap fails (the allocator returns an error) instead of the
// machine thrashing/freezing on a runaway allocation — this is the guard against
// the 107GB-alloc class of bug that froze the user's machine.
//
// bytes must be > 0; callers are expected to pass a value clamped to a sane
// fraction of physical RAM (never above it). Applying a second time replaces the
// previous job association.
func LimitProcessMemory(bytes uintptr) error {
	if bytes == 0 {
		return windows.ERROR_INVALID_PARAMETER
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	// PROCESS_MEMORY caps each process in the job individually. BREAKAWAY_OK lets
	// a child that is launched with CREATE_BREAKAWAY_FROM_JOB escape the cap; we
	// use it ONLY for Ghidra's JVM, which sizes its heap to ~70% of physical RAM
	// (tens of GB) and would OOM-crash under the per-process cap. It is NOT
	// SILENT_BREAKAWAY_OK: breakaway is explicit per child, so every other tool
	// (retoc, de4dot, strings, …) stays capped.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY |
		windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	info.ProcessMemoryLimit = bytes

	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return err
	}

	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		return err
	}

	// Retain the handle for the process lifetime so the limit stays in force.
	memLimitJob = job
	return nil
}
