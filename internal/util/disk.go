package util

import (
	"path/filepath"
	"sort"
)

// DriveInfo is a candidate output drive with its free space.
type DriveInfo struct {
	Letter    string // e.g. "D:"
	FreeBytes uint64
}

// pickMostFree returns the drive letter with the most free space, preferring
// drives not in excluded. If every candidate is excluded, it falls back to the
// absolute max-free drive so we never return "".
func pickMostFree(cands []DriveInfo, excluded map[string]bool) string {
	if len(cands) == 0 {
		return ""
	}
	byFree := make([]DriveInfo, len(cands))
	copy(byFree, cands)
	sort.SliceStable(byFree, func(i, j int) bool { return byFree[i].FreeBytes > byFree[j].FreeBytes })

	for _, d := range byFree {
		if !excluded[d.Letter] {
			return d.Letter
		}
	}
	return byFree[0].Letter // all excluded — max free overall
}

// OutputRoot returns the chosen output root. If explicit is non-empty it wins.
// Otherwise the most-free non-C:/non-E: drive is picked and "<letter>\morgue-out"
// is returned. When no drives are reported (non-Windows / restricted), it falls
// back to DefaultOutputDir().
//
// C: and E: are excluded by default: on the dev/target machine they are the OS
// and source disks and run out of space — IL2CPP/AssetRipper exports are tens of
// GB, so the implicit output must land elsewhere.
func OutputRoot(explicit string, drives []DriveInfo) string {
	if explicit != "" {
		return explicit
	}
	letter := pickMostFree(drives, map[string]bool{"C:": true, "E:": true})
	if letter == "" {
		return DefaultOutputDir()
	}
	return filepath.Join(letter+`\`, "morgue-out")
}

// AutoOutputRoot returns the most-free non-C:/non-E: output root, falling back to
// DefaultOutputDir() when no drives are reported (non-Windows / restricted env).
// This is the implicit default wired into the CLI/service entry points; an
// explicit user-provided output path always takes precedence upstream.
func AutoOutputRoot() string {
	return OutputRoot("", EnumerateDrives())
}
