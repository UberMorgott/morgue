//go:build !windows

package util

// EnumerateDrives returns no drives on non-Windows; OutputRoot falls back to
// the default output dir.
func EnumerateDrives() []DriveInfo { return nil }
