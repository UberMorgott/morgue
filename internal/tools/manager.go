package tools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
)

const updateCheckInterval = 6 * time.Hour

// InstallCallbacks holds optional progress callbacks for Install.
type InstallCallbacks struct {
	OnProgress func(tool string, bytesDown, bytesTotal int64)
	OnExtract  func(tool string)
}

// Manager handles tool installation and resolution.
type Manager struct {
	baseDir string
	cfg     config.Config
}

// NewManager creates a Manager that stores tools under baseDir.
func NewManager(baseDir string, cfg config.Config) *Manager {
	// Route downloads through a configured mirror (offline / firewalled installs).
	setAssetMirror(cfg.ToolsMirror)
	return &Manager{baseDir: baseDir, cfg: cfg}
}

// preSeededGhidraHome returns the ghidraRun.bat path of a pre-existing, offline
// Ghidra install honoring, in order: $GHIDRA_HOME env → cfg.GhidraHome. A home
// is accepted when it contains support/analyzeHeadless(.bat) OR ghidraRun(.bat).
// Returns "" when none is configured or valid. No network is touched.
func preSeededGhidraHome(cfg config.Config) string {
	for _, home := range []string{os.Getenv("GHIDRA_HOME"), cfg.GhidraHome} {
		home = strings.TrimSpace(home)
		if home == "" {
			continue
		}
		if p := validateGhidraHome(home); p != "" {
			return p
		}
	}
	return ""
}

// validateGhidraHome returns the launcher path (home/ghidraRun.bat) if home
// looks like a Ghidra install, else "". runGhidra only uses Dir(launcher) to
// locate support/analyzeHeadless, so a home holding either marker is usable.
func validateGhidraHome(home string) string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".bat"
	}
	run := filepath.Clean(filepath.Join(home, "ghidraRun"+ext))
	analyze := filepath.Clean(filepath.Join(home, "support", "analyzeHeadless"+ext))
	if _, err := os.Stat(run); err == nil {
		return run
	}
	if _, err := os.Stat(analyze); err == nil {
		return run
	}
	return ""
}

// Resolve returns the full path to a tool's binary, or an error if not installed.
func (m *Manager) Resolve(name string) (string, error) {
	tool, ok := FindByName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	// Offline unblock: a pre-seeded Ghidra ($GHIDRA_HOME / cfg.GhidraHome) wins
	// over the managed install so no download is ever needed.
	if name == "ghidra" {
		if p := preSeededGhidraHome(m.cfg); p != "" {
			return p, nil
		}
	}

	path := filepath.Join(m.baseDir, name, tool.Binary)
	if _, err := os.Stat(path); err != nil {
		if found := findBinaryRecursive(filepath.Join(m.baseDir, name), tool.Binary); found != "" {
			return found, nil
		}
		return "", fmt.Errorf("tool %s not installed (expected at %s)", name, path)
	}
	return path, nil
}

// findBinaryRecursive walks a directory tree looking for a file by name.
func findBinaryRecursive(dir, binaryName string) string {
	var result string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // unreadable entry: skip it, keep searching the rest of the tree
		}
		if !d.IsDir() && d.Name() == binaryName {
			result = path
			return filepath.SkipAll
		}
		return nil
	})
	return result
}

// Check returns the installation status of a named tool.
func (m *Manager) Check(name string) ToolStatus {
	tool, ok := FindByName(name)
	if !ok {
		return ToolStatus{Name: name}
	}

	path := filepath.Join(m.baseDir, name, tool.Binary)
	_, err := os.Stat(path)

	if err != nil {
		toolDir := filepath.Join(m.baseDir, name)
		if found := findBinaryRecursive(toolDir, tool.Binary); found != "" {
			path = found
			err = nil
		}
	}

	// A pre-seeded offline Ghidra counts as installed so the pipeline never tries
	// to download it.
	if err != nil && name == "ghidra" {
		if p := preSeededGhidraHome(m.cfg); p != "" {
			path = p
			err = nil
		}
	}

	// Read version from .version file
	var version string
	versionBytes, readErr := os.ReadFile(filepath.Clean(filepath.Join(m.baseDir, name, ".version")))
	if readErr == nil {
		version = strings.TrimSpace(string(versionBytes))
	}

	return ToolStatus{
		Name:        name,
		Installed:   err == nil,
		Path:        path,
		Version:     version,
		Category:    tool.Category.String(),
		Description: tool.Description,
		Optional:    tool.Optional,
		RuntimeDeps: tool.RuntimeDeps,
	}
}

// CheckAll returns the status of every tool in the registry.
func (m *Manager) CheckAll() []ToolStatus {
	statuses := make([]ToolStatus, 0, len(Registry))
	for _, t := range Registry {
		statuses = append(statuses, m.Check(t.Name))
	}
	return statuses
}

// ToolsNeeded returns names of tools that are not yet installed from the given list.
func (m *Manager) ToolsNeeded(names []string) []string {
	var needed []string
	for _, name := range names {
		if !m.IsInstalled(name) {
			needed = append(needed, name)
		}
	}
	return needed
}

// IsInstalled returns true if the tool is present on disk.
func (m *Manager) IsInstalled(name string) bool {
	return m.Check(name).Installed
}

// Install downloads and installs a tool. Returns the installed version and an error if any.
// Callbacks are optional — pass nil for no progress reporting.
// Cancelling ctx aborts an in-flight download and removes the partial file.
func (m *Manager) Install(ctx context.Context, name string, cb *InstallCallbacks) (string, error) {
	tool, ok := FindByName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	// Install into a fresh staging dir next to the live one, validate it, then
	// swap it in. A failed or cancelled (re)install therefore never destroys a
	// working tool. Staging lives in baseDir so findDotnetBin / release cache
	// resolve exactly as for the final dir.
	destDir := filepath.Join(m.baseDir, name)
	stageDir := filepath.Join(m.baseDir, "."+name+".staging")
	_ = os.RemoveAll(stageDir)
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	version, err := m.installInto(ctx, tool, stageDir, cb)
	if err == nil {
		err = validateBinaryInstalled(tool, stageDir)
	}
	if err != nil {
		return "", err
	}
	if err := swapDir(stageDir, destDir); err != nil {
		return "", fmt.Errorf("activate %s: %w", name, err)
	}
	_ = m.RecordInstall(name, version)
	return version, nil
}

// swapDir replaces dest with staged. The previous dest is moved aside first and
// restored if the final rename fails, so dest is never left missing.
func swapDir(staged, dest string) error {
	backup := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".old")
	_ = os.RemoveAll(backup)
	hadOld := false
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			return fmt.Errorf("move old install aside (in use?): %w", err)
		}
		hadOld = true
	}
	if err := os.Rename(staged, dest); err != nil {
		if hadOld {
			if rerr := os.Rename(backup, dest); rerr != nil {
				return fmt.Errorf("install new version: %w; restore old: %v (old copy kept at %s)", err, rerr, backup)
			}
		}
		return fmt.Errorf("install new version: %w", err)
	}
	if hadOld {
		_ = os.RemoveAll(backup)
	}
	return nil
}

// installInto runs the tool's install method into destDir and returns the
// version to record. It writes destDir/.version but does not touch the lock.
func (m *Manager) installInto(ctx context.Context, tool ToolDef, destDir string, cb *InstallCallbacks) (string, error) {
	name := tool.Name
	var progressCb func(bytesDown, bytesTotal int64)
	if cb != nil && cb.OnProgress != nil {
		progressCb = func(bytesDown, bytesTotal int64) {
			cb.OnProgress(name, bytesDown, bytesTotal)
		}
	}

	var extractCb func()
	if cb != nil && cb.OnExtract != nil {
		extractCb = func() {
			cb.OnExtract(name)
		}
	}

	var onExtractNuGet func(string)
	if cb != nil && cb.OnExtract != nil {
		onExtractNuGet = func(tool string) {
			cb.OnExtract(tool)
		}
	}

	var onProgressGitBuild func(string, int64, int64)
	if cb != nil && cb.OnProgress != nil {
		onProgressGitBuild = cb.OnProgress
	}

	switch tool.Method {
	case MethodGitHubRelease:
		return installFromGitHub(ctx, tool, destDir, m.cfg.GitHubToken, progressCb, extractCb)
	case MethodDirectURL:
		var err error
		if len(tool.DownloadURLs) > 0 {
			err = installFromURLs(ctx, tool.DownloadURLs, destDir, progressCb, extractCb)
		} else {
			err = installFromURL(ctx, tool, destDir, progressCb, extractCb)
		}
		if err != nil {
			return "", err
		}
		ver := tool.Version
		if ver == "" && tool.URL != "" {
			// Unversioned asset (e.g. Sysinternals strings): record the
			// server's Last-Modified date, the same value update checks
			// compare against.
			ver = directURLDate(ctx, tool.URL)
		}
		if ver == "" {
			ver = "latest"
		}
		versionFile := filepath.Join(destDir, ".version")
		_ = os.WriteFile(versionFile, []byte(ver), 0644)
		return ver, nil
	case MethodDotnetTool:
		err := installDotnetTool(ctx, tool, destDir)
		if err != nil {
			return "", err
		}
		ver := tool.DotnetVersion
		if ver == "" {
			ver = "latest"
		}
		versionFile := filepath.Join(destDir, ".version")
		_ = os.WriteFile(versionFile, []byte(ver), 0644)
		return ver, nil
	case MethodNuGet:
		ver, err := installFromNuGet(ctx, tool, destDir, progressCb, onExtractNuGet)
		if err != nil {
			return "", err
		}
		versionFile := filepath.Join(destDir, ".version")
		_ = os.WriteFile(versionFile, []byte(ver), 0644)
		return ver, nil
	case MethodGitBuild:
		return installFromGitBuild(ctx, tool, destDir, onProgressGitBuild, extractCb)
	default:
		return "", fmt.Errorf("unsupported install method for %s", name)
	}
}

// cleanVersionTag normalizes a release tag for display.
func cleanVersionTag(tag string) string {
	tag = strings.TrimPrefix(tag, "v")
	tag = strings.TrimPrefix(tag, "V")
	// Ghidra-specific: "Ghidra_12.1_build" → "12.1"
	if after, found := strings.CutPrefix(tag, "Ghidra_"); found {
		tag = strings.TrimSuffix(after, "_build")
	}
	if strings.EqualFold(tag, "latest") || tag == "releases" || tag == "" {
		return ""
	}
	return tag
}

// pinnedVersion reports the version a pinned tool will always install, and
// whether the tool is pinned at all. Install resolves a non-empty tool.Version
// verbatim (a fixed release tag for GitHub tools, a fixed asset URL for direct
// downloads) and a non-empty DotnetVersion as the exact NuGet / dotnet-tool
// version, so for those tools there is no upstream question to ask: the
// answer would only ever advertise an update the installer cannot perform.
// Unpinned tools keep their real upstream lookup below.
func pinnedVersion(t ToolDef) (string, bool) {
	if t.Version != "" {
		return cleanVersionTag(t.Version), true
	}
	if t.DotnetVersion != "" && (t.Method == MethodNuGet || t.Method == MethodDotnetTool) {
		return cleanVersionTag(t.DotnetVersion), true
	}
	return "", false
}

// normalizeVersion canonicalizes a version for equality checks: display
// cleanup (cleanVersionTag), lower case, build metadata ("+sha") dropped and
// trailing ".0" components of the numeric core trimmed, so "v1.3.14",
// "1.3.14.0" and "1.3.14+abc" all compare equal.
func normalizeVersion(v string) string {
	v = strings.ToLower(cleanVersionTag(strings.TrimSpace(v)))
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	core = strings.Join(parts, ".")
	if hasPre {
		return core + "-" + pre
	}
	return core
}

// isUpdate reports whether a tool installed at `installed` should be offered
// `latest`. Unknown values on either side ("", "latest", "–") never count.
func isUpdate(installed, latest string) bool {
	a, b := normalizeVersion(installed), normalizeVersion(latest)
	if a == "" || b == "" || a == "–" || b == "–" {
		return false
	}
	return a != b
}

// directURLDate returns the Last-Modified date (2006.01.02) of a direct
// download, or "" when the server does not report one.
func directURLDate(ctx context.Context, url string) string {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpDo(ctx, client, http.MethodHead, rewriteMirror(url, currentAssetMirror()))
	if err != nil {
		return ""
	}
	_ = resp.Body.Close()
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if parsed, err := http.ParseTime(lm); err == nil {
			return parsed.UTC().Format("2006.01.02")
		}
	}
	return ""
}

// latestVersion asks upstream for a tool's newest version ("" on failure,
// "–" for a direct download that reports no date). Pinned tools answer with
// their pin.
func latestVersion(ctx context.Context, t ToolDef) string {
	if pin, ok := pinnedVersion(t); ok {
		return pin
	}
	switch {
	case t.Method == MethodGitHubRelease && t.Repo != "":
		if tag, err := fetchLatestVersion(ctx, t.Repo); err == nil {
			return cleanVersionTag(tag)
		}
	case t.Method == MethodDotnetTool && t.DotnetID != "",
		t.Method == MethodNuGet && t.DotnetID != "":
		if ver, err := fetchNuGetLatestVersion(ctx, t.DotnetID); err == nil {
			return ver
		}
	case t.Method == MethodGitBuild && t.Repo != "":
		if commit, err := fetchLatestCommit(ctx, t.Repo); err == nil {
			return commit
		}
	case t.Method == MethodDirectURL && t.URL != "":
		if d := directURLDate(ctx, t.URL); d != "" {
			return d
		}
		return "–"
	}
	return ""
}

// CheckAllWithUpdates returns status of all tools including latest GitHub versions.
// Uses HTTP redirect to check versions — no GitHub API calls, no rate limit.
func (m *Manager) CheckAllWithUpdates() []ToolStatus {
	// Version probes are short, self-bounded by the client Timeout, and have no
	// caller context (Wails binding / periodic startup check).
	ctx := context.Background()
	statuses := make([]ToolStatus, 0, len(Registry))
	for _, t := range Registry {
		st := m.Check(t.Name)
		// Clean up stored version for display
		st.Version = cleanVersionTag(st.Version)
		st.LatestVersion = latestVersion(ctx, t)
		st.UpdateAvailable = st.Installed && isUpdate(st.Version, st.LatestVersion)
		statuses = append(statuses, st)
	}
	return statuses
}

// CheckLatestVersionSingle checks latest version for one tool by name.
// Returns the latest version string and whether an update is available.
func (m *Manager) CheckLatestVersionSingle(name string) (latest string, updateAvailable bool) {
	tool, ok := FindByName(name)
	if !ok {
		return "", false
	}
	ctx := context.Background() // no caller context; probes are Timeout-bounded
	st := m.Check(name)
	latest = latestVersion(ctx, tool)
	return latest, st.Installed && isUpdate(st.Version, latest)
}

// RuntimeEnv returns environment variables for local runtimes (PATH prepend, JAVA_HOME).
func (m *Manager) RuntimeEnv() []string {
	var env []string

	dotnetDir := m.localRuntimeDir(RuntimeDotnet)
	if _, err := os.Stat(filepath.Join(dotnetDir, runtimeBinary(RuntimeDotnet))); err == nil {
		env = append(env, fmt.Sprintf("PATH=%s%c%s", dotnetDir, os.PathListSeparator, os.Getenv("PATH")))
	}

	javaDir := m.localRuntimeDir(RuntimeJava)
	if _, err := os.Stat(filepath.Join(javaDir, runtimeBinary(RuntimeJava))); err == nil {
		env = append(env, fmt.Sprintf("JAVA_HOME=%s", javaDir))
		javaBinDir := filepath.Join(javaDir, "bin")
		for i, e := range env {
			if len(e) > 5 && e[:5] == "PATH=" {
				env[i] = fmt.Sprintf("PATH=%s%c%s", javaBinDir, os.PathListSeparator, e[5:])
				break
			}
		}
		if len(env) == 1 {
			env = append(env, fmt.Sprintf("PATH=%s%c%s", javaBinDir, os.PathListSeparator, os.Getenv("PATH")))
		}
	}

	return env
}

// ShouldCheckUpdates returns true if enough time has passed since the last update check.
func (m *Manager) ShouldCheckUpdates() bool {
	data, err := os.ReadFile(filepath.Join(m.baseDir, ".last-check"))
	if err != nil {
		return true // no file = never checked
	}

	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return true
	}

	return time.Since(time.Unix(ts, 0)) > updateCheckInterval
}

// MarkUpdateChecked writes the current timestamp to the last-check file.
func (m *Manager) MarkUpdateChecked() {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	_ = os.WriteFile(filepath.Join(m.baseDir, ".last-check"), []byte(ts), 0644)
}

// Delete removes a tool's directory from disk.
func (m *Manager) Delete(name string) error {
	_, ok := FindByName(name)
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}
	return os.RemoveAll(filepath.Join(m.baseDir, name))
}
