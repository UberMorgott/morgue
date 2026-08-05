package util

import (
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// MaxPath is the classic Windows path limit. Paths at or beyond it fail in any
// process that is not long-path aware (notably the JVM Ghidra runs in).
const MaxPath = 260

// NearMaxPath reports whether p is close enough to MaxPath that any tool
// without long-path support (external EXEs, the JVM Ghidra runs in) is likely
// to fail on it. The margin covers the file names such a tool appends.
func NearMaxPath(p string) bool {
	return runtime.GOOS == "windows" && len(p) >= MaxPath-40
}

// LongPath returns p in Windows extended-length form (\\?\C:\... or
// \\?\UNC\server\share\...), which lifts the 260-character MAX_PATH limit for
// Win32 file APIs. On non-Windows it is a no-op. Returns p unchanged when it
// cannot be normalized into a form \\?\ accepts (which requires an absolute,
// backslash-separated path with no "." or ".." elements).
func LongPath(p string) string {
	if runtime.GOOS != "windows" || p == "" {
		return p
	}
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		return p
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return longPathWin(abs)
}

// longPathWin does LongPath's pure string work on an already-absolute Windows
// path. Split out so it is testable on any platform (it makes no OS calls).
func longPathWin(p string) string {
	if p == "" || strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) {
		return p
	}
	s := strings.ReplaceAll(p, `\`, "/")
	unc := strings.HasPrefix(s, "//")
	s = strings.ReplaceAll(path.Clean(s), "/", `\`) // drops . / .. / dup separators
	switch {
	case unc:
		return `\\?\UNC` + s // s starts with a separator: \\?\UNC\server\share
	case len(s) >= 3 && s[1] == ':' && s[2] == '\\':
		return `\\?\` + s
	}
	return p // not a drive-absolute path — prefixing would only break it
}

// BaseDir returns the directory containing the morgue executable.
func BaseDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// ToolsBaseDir returns the root directory for all tools: BaseDir/tools/.
func ToolsBaseDir() string {
	return filepath.Join(BaseDir(), "tools")
}

// ToolDir returns the directory for a named tool under BaseDir/tools/.
func ToolDir(name string) string {
	return filepath.Join(ToolsBaseDir(), name)
}

// DefaultOutputDir returns the default output directory: BaseDir/output/.
func DefaultOutputDir() string {
	return filepath.Join(BaseDir(), "output")
}

// ToolPath returns the full path to an executable within a tool directory.
func ToolPath(name, exe string) string {
	return filepath.Join(ToolDir(name), exe)
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	return filepath.Join(BaseDir(), "morgue.yaml")
}
