//go:build !windows

package util

// TotalPhysicalMemoryBytes returns 0 on non-Windows platforms, where physical
// RAM is not probed; callers fall back to the tool's default heap.
func TotalPhysicalMemoryBytes() uint64 { return 0 }
