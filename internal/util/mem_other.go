//go:build !windows

package util

// TotalPhysicalMemoryBytes returns 0 on non-Windows platforms, where physical
// RAM is not probed; callers fall back to the tool's default heap.
func TotalPhysicalMemoryBytes() uint64 { return 0 }

// LimitProcessMemory is a no-op on non-Windows platforms: the Job Object memory
// cap is Windows-specific. Callers treat a nil return as "no cap applied".
func LimitProcessMemory(bytes uintptr) error { return nil }
