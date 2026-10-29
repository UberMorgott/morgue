package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// Morgue runs every runtime (Java/JDK, .NET runtime/SDK, Python) and tool
// from its own folder. System installs are used only when the operator opts in
// with prefer_system_tools; otherwise children must not inherit variables or
// PATH entries that point them at a system runtime.

var allowSystemRuntimes atomic.Bool

// SetAllowSystemRuntimes records the prefer_system_tools opt-in (default
// false). The tools manager sets it from config.
func SetAllowSystemRuntimes(allow bool) { allowSystemRuntimes.Store(allow) }

// AllowSystemRuntimes reports the prefer_system_tools opt-in.
func AllowSystemRuntimes() bool { return allowSystemRuntimes.Load() }

// leakedRuntimeKeys are inherited variables that select or configure a system
// runtime. JAVA_TOOL_OPTIONS/_JAVA_OPTIONS would also override the managed
// JVM heap cap.
var leakedRuntimeKeys = map[string]bool{
	"JAVA_HOME": true, "JDK_HOME": true, "JRE_HOME": true, "CLASSPATH": true,
	"JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true, "JDK_JAVA_OPTIONS": true,
	"DOTNET_ROOT": true, "DOTNET_ROOT(X86)": true, "DOTNET_ROOT_X64": true, "DOTNET_ROOT_X86": true,
	"DOTNET_HOST_PATH": true, "MSBUILD_EXE_PATH": true, "MSBUILDEXTENSIONSPATH": true,
	"PYTHONHOME": true, "PYTHONPATH": true, "PYTHONSTARTUP": true, "VIRTUAL_ENV": true, "CONDA_PREFIX": true,
	"GHIDRA_HOME": true,
}

// runtimeExecutables mark a PATH directory as a system runtime install.
var runtimeExecutables = []string{"java", "javaw", "javac", "dotnet", "python", "python3"}

// IsLeakedRuntimeKey reports whether an inherited variable is stripped from
// child environments by default.
func IsLeakedRuntimeKey(key string) bool { return leakedRuntimeKeys[strings.ToUpper(key)] }

// pathHasRuntime reports whether dir holds a runtime launcher. The Windows
// system directories are never dropped (they hold no Java/.NET/Python).
func pathHasRuntime(dir string) bool {
	if dir == "" {
		return false
	}
	if sys := os.Getenv("SystemRoot"); sys != "" {
		clean := strings.ToLower(filepath.Clean(dir))
		root := strings.ToLower(filepath.Clean(sys))
		if clean == root || strings.HasPrefix(clean, root+string(filepath.Separator)) {
			return false
		}
	}
	for _, name := range runtimeExecutables {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// SanitizePath removes PATH entries that contain a system Java/.NET/Python
// launcher, unless prefer_system_tools is on.
func SanitizePath(path string) string {
	if AllowSystemRuntimes() {
		return path
	}
	parts := filepath.SplitList(path)
	kept := parts[:0]
	for _, p := range parts {
		if !pathHasRuntime(p) {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// ChildPath is the inherited PATH a spawned tool may see (sanitized).
func ChildPath() string { return SanitizePath(os.Getenv("PATH")) }

// sanitizeEnviron drops leaked runtime variables and sanitizes PATH in an
// inherited environment, unless prefer_system_tools is on.
func sanitizeEnviron(environ []string) []string {
	if AllowSystemRuntimes() {
		return environ
	}
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			out = append(out, kv)
			continue
		}
		if IsLeakedRuntimeKey(k) {
			continue
		}
		if strings.EqualFold(k, "PATH") {
			kv = k + "=" + SanitizePath(v)
		}
		out = append(out, kv)
	}
	return out
}
