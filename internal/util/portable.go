package util

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Morgue is a portable app: everything it (and every tool it spawns) writes
// must stay under BaseDir(). Runtime state lives in BaseDir()/data:
//
//	data/tmp/p<pid>  per-process temp dir (TEMP/TMP for morgue and children), removed on exit
//	data/home        HOME-like dir for tools (DOTNET_CLI_HOME → data/home/.dotnet)
//	data/config      XDG_CONFIG_HOME (Ghidra settings dir)
//	data/cache       XDG_CACHE_HOME (Ghidra cache), NuGet caches, .NET bundle extraction
//	data/share       XDG_DATA_HOME
//	data/state       XDG_STATE_HOME
//	data/api-token   per-launch GUI API token

// DataDir returns BaseDir()/data, the root of all morgue runtime state.
func DataDir() string { return filepath.Join(BaseDir(), "data") }

// TempRoot returns BaseDir()/data/tmp, the parent of the per-process temp dirs.
func TempRoot() string { return filepath.Join(DataDir(), "tmp") }

// APITokenPath returns BaseDir()/data/api-token.
func APITokenPath() string { return filepath.Join(DataDir(), "api-token") }

var (
	procTempMu sync.Mutex
	procTemp   string // set by InitPortable
)

// ProcTempDir returns this process's temp dir: data/tmp/p<pid> once
// InitPortable ran, else TempRoot() (tests, library use).
func ProcTempDir() string {
	procTempMu.Lock()
	defer procTempMu.Unlock()
	if procTemp != "" {
		return procTemp
	}
	return TempRoot()
}

// InitPortable prepares BaseDir()/data, verifies it is writable, sweeps temp
// dirs left by dead morgue processes, creates this process's temp dir and
// points TEMP/TMP/TMPDIR at it, so os.TempDir/os.MkdirTemp("") and every child
// process (which inherits the environment) stay inside BaseDir(). It never
// falls back to system dirs: an unwritable BaseDir is an error the caller must
// surface. cleanup removes the per-process temp dir; call it on exit.
func InitPortable() (cleanup func(), err error) {
	return initPortableAt(DataDir())
}

func initPortableAt(dataDir string) (func(), error) {
	tmpRoot := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return nil, notWritable(dataDir, err)
	}
	probe, err := os.CreateTemp(dataDir, ".write-probe-*")
	if err != nil {
		return nil, notWritable(dataDir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())

	sweepStaleTemp(tmpRoot)

	dir := filepath.Join(tmpRoot, "p"+strconv.Itoa(os.Getpid()))
	_ = os.RemoveAll(dir) // a reused PID's leftovers
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, notWritable(dataDir, err)
	}
	for _, k := range []string{"TEMP", "TMP", "TMPDIR"} {
		if err := os.Setenv(k, dir); err != nil {
			return nil, fmt.Errorf("portable: set %s: %w", k, err)
		}
	}
	procTempMu.Lock()
	procTemp = dir
	procTempMu.Unlock()

	return func() { _ = os.RemoveAll(dir) }, nil
}

func notWritable(dir string, err error) error {
	return fmt.Errorf("morgue is a portable app and keeps all its data next to the executable, "+
		"but %s is not writable (%w); move morgue to a writable folder", dir, err)
}

// sweepStaleTemp removes data/tmp/p<pid> dirs whose process is gone (a crash
// or kill skipped their cleanup). Dirs of live processes are left alone.
func sweepStaleTemp(tmpRoot string) {
	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "p") {
			continue
		}
		pid, err := strconv.Atoi(name[1:])
		if err != nil || pid <= 0 || pid == self || processAlive(pid) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(tmpRoot, name))
	}
}

// PortableEnv returns the KEY=VALUE overrides that keep spawned tools inside
// dataDir: temp, XDG dirs (Ghidra's settings/cache dirs honour them), .NET CLI
// home/telemetry/first-run, NuGet caches and single-file bundle extraction.
func PortableEnv(dataDir, tempDir string) []string {
	home := filepath.Join(dataDir, "home")
	cache := filepath.Join(dataDir, "cache")
	return []string{
		"TEMP=" + tempDir,
		"TMP=" + tempDir,
		"TMPDIR=" + tempDir,
		"XDG_CONFIG_HOME=" + filepath.Join(dataDir, "config"),
		"XDG_CACHE_HOME=" + cache,
		"XDG_DATA_HOME=" + filepath.Join(dataDir, "share"),
		"XDG_STATE_HOME=" + filepath.Join(dataDir, "state"),
		"DOTNET_CLI_HOME=" + home,
		"DOTNET_CLI_TELEMETRY_OPTOUT=1",
		"DOTNET_NOLOGO=1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
		"DOTNET_GENERATE_ASPNET_CERTIFICATE=false",
		"DOTNET_ADD_GLOBAL_TOOLS_TO_PATH=false",
		"DOTNET_CLI_WORKLOAD_UPDATE_NOTIFY_DISABLE=1",
		"DOTNET_BUNDLE_EXTRACT_BASE_DIR=" + filepath.Join(cache, "dotnet-bundle"),
		"NUGET_PACKAGES=" + filepath.Join(cache, "nuget", "packages"),
		"NUGET_HTTP_CACHE_PATH=" + filepath.Join(cache, "nuget", "http-cache"),
		"NUGET_PLUGINS_CACHE_PATH=" + filepath.Join(cache, "nuget", "plugins-cache"),
		"MSBUILDDISABLENODEREUSE=1",
	}
}

// ChildEnv returns the full environment for a spawned tool: os.Environ()
// without system runtime selectors (see sanitizeEnviron),
// then PortableEnv for BaseDir()/data, then extra (later entries win; exec.Cmd
// keeps the last value of a duplicated key, case-insensitively on Windows).
func ChildEnv(extra ...string) []string {
	env := PortableEnv(DataDir(), ProcTempDir())
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if filepath.IsAbs(v) && k != "TEMP" && k != "TMP" && k != "TMPDIR" {
			_ = os.MkdirAll(v, 0o755) // tools expect these to exist; best effort
		}
	}
	_ = os.MkdirAll(ProcTempDir(), 0o755)
	out := make([]string, 0, len(os.Environ())+len(env)+len(extra))
	// Inherited system runtime selectors (JAVA_HOME, DOTNET_ROOT, PYTHONHOME,
	// runtime dirs on PATH) are stripped unless prefer_system_tools is on.
	out = append(out, sanitizeEnviron(os.Environ())...)
	out = append(out, env...)
	return append(out, extra...)
}

// ApplyChildEnv sets cmd.Env to ChildEnv(extra...). Every exec.Cmd morgue
// spawns goes through this (directly or via the RunCmd* helpers).
func ApplyChildEnv(cmd *exec.Cmd, extra ...string) {
	cmd.Env = ChildEnv(extra...)
}
