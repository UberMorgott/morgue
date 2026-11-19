package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/util"
)

// RuntimeKind identifies a runtime dependency.
type RuntimeKind string

const (
	RuntimeDotnet RuntimeKind = "dotnet"
	RuntimeJava   RuntimeKind = "java"
	RuntimeAspNet RuntimeKind = "dotnet-aspnet10"
)

// RuntimeStatus holds the detected state of a runtime.
type RuntimeStatus struct {
	Kind      RuntimeKind `json:"Kind"`
	Available bool        `json:"Available"`
	Version   string      `json:"Version"`
	Path      string      `json:"Path"`
	Local     bool        `json:"Local"`
	Required  bool        `json:"Required"`
}

// RuntimeNeeded returns which runtimes are needed based on registered tools.
func RuntimeNeeded() []RuntimeKind {
	needs := map[RuntimeKind]bool{}
	for _, t := range Registry {
		if t.Method == MethodDotnetTool {
			needs[RuntimeDotnet] = true
		}
		if t.Name == "ghidra" {
			needs[RuntimeJava] = true
		}
		for _, rk := range t.RuntimeDeps {
			needs[rk] = true
		}
	}
	var result []RuntimeKind
	for k := range needs {
		result = append(result, k)
	}
	return result
}

// runtimeBinary returns the expected binary name for the runtime.
func runtimeBinary(kind RuntimeKind) string {
	switch kind {
	case RuntimeDotnet:
		if runtime.GOOS == "windows" {
			return "dotnet.exe"
		}
		return "dotnet"
	case RuntimeJava:
		if runtime.GOOS == "windows" {
			return filepath.Join("bin", "java.exe")
		}
		return filepath.Join("bin", "java")
	case RuntimeAspNet:
		if runtime.GOOS == "windows" {
			return "dotnet.exe"
		}
		return "dotnet"
	}
	return ""
}

// runtimeSystemName returns the name to look up in PATH.
func runtimeSystemName(kind RuntimeKind) string {
	switch kind {
	case RuntimeDotnet:
		return "dotnet"
	case RuntimeJava:
		return "java"
	case RuntimeAspNet:
		return "dotnet"
	}
	return ""
}

// localRuntimeDir returns the directory for a portable runtime.
func (m *Manager) localRuntimeDir(kind RuntimeKind) string {
	return filepath.Join(m.baseDir, "runtimes", string(kind))
}

// localRuntimeBin returns the full path to a portable runtime binary.
func (m *Manager) localRuntimeBin(kind RuntimeKind) string {
	return filepath.Join(m.localRuntimeDir(kind), runtimeBinary(kind))
}

// CheckRuntimes detects available runtimes (local first, then system).
func (m *Manager) CheckRuntimes() []RuntimeStatus {
	needed := RuntimeNeeded()
	neededSet := map[RuntimeKind]bool{}
	for _, k := range needed {
		neededSet[k] = true
	}

	allKinds := []RuntimeKind{RuntimeDotnet, RuntimeJava, RuntimeAspNet}
	statuses := make([]RuntimeStatus, 0, len(allKinds))

	for _, kind := range allKinds {
		st := RuntimeStatus{
			Kind:     kind,
			Required: neededSet[kind],
		}

		// Check local portable first
		localBin := m.localRuntimeBin(kind)
		if _, err := os.Stat(localBin); err == nil {
			st.Available = true
			st.Path = localBin
			st.Local = true
			st.Version = detectRuntimeVersion(kind, localBin)
			// Too old or a JRE: shown, but not available.
			st.Available = kind != RuntimeJava || m.javaSatisfies(localBin)
			statuses = append(statuses, st)
			continue
		}

		// Fall back to system only with the prefer_system_tools opt-in.
		sysName := runtimeSystemName(kind)
		if sysPath, err := exec.LookPath(sysName); err == nil && m.cfg.PreferSystemTools {
			st.Available = true
			st.Path = sysPath
			st.Local = false
			st.Version = detectRuntimeVersion(kind, sysPath)
			st.Available = kind != RuntimeJava || m.javaSatisfies(sysPath)
		}

		statuses = append(statuses, st)
	}
	return statuses
}

// detectRuntimeVersion runs the binary to get its version string.
func detectRuntimeVersion(kind RuntimeKind, binPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch kind {
	case RuntimeDotnet:
		cmd = exec.CommandContext(ctx, binPath, "--version")
	case RuntimeJava:
		cmd = exec.CommandContext(ctx, binPath, "-version")
	case RuntimeAspNet:
		cmd = exec.CommandContext(ctx, binPath, "--list-runtimes")
	default:
		return ""
	}

	util.HideCmdWindow(cmd)
	util.ApplyChildEnv(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	text := strings.TrimSpace(string(out))
	if kind == RuntimeAspNet {
		var versions []string
		for line := range strings.SplitSeq(text, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "Microsoft.AspNetCore.App" && strings.HasPrefix(fields[1], "10.") {
				versions = append(versions, fields[1])
			}
		}
		return latestStableVersion(versions)
	}
	// java -version outputs multi-line; take first line, strip quotes
	if kind == RuntimeJava {
		lines := strings.Split(text, "\n")
		if len(lines) > 0 {
			line := strings.TrimSpace(lines[0])
			// e.g. openjdk version "21.0.3" 2024-04-16
			if idx := strings.Index(line, "\""); idx >= 0 {
				end := strings.Index(line[idx+1:], "\"")
				if end >= 0 {
					return line[idx+1 : idx+1+end]
				}
			}
			return line
		}
	}
	return text
}

// RuntimePath returns the path to a runtime binary, preferring local.
//
// RuntimeAspNet is version-sensitive: InspectorRedux targets net10.0 and a stray
// older system dotnet (e.g. 8.x) on PATH would otherwise be accepted and then fail
// at launch with "You must install or update .NET". So for RuntimeAspNet a system
// dotnet only counts when it actually hosts an ASP.NET Core 10.x runtime; otherwise
// the caller is told the runtime is missing and installs the bundled portable copy.
func (m *Manager) RuntimePath(kind RuntimeKind) (string, error) {
	if m.selection != nil {
		s := m.selection
		s.work.Lock()
		defer s.work.Unlock()
		if err := s.ctx.Err(); err != nil {
			return "", err
		}
		entry, err := s.selectRuntime(s.ctx, kind, nil)
		return entry.path, err
	}
	return m.runtimePathUnbound(kind)
}

func (m *Manager) runtimePathUnbound(kind RuntimeKind) (string, error) {
	if kind == RuntimeJava {
		// Pick the installed JDK that matches the installed Ghidra's
		// application.java.min (floor managedJavaFeature): the managed
		// portable JDK (JAVA_HOME/PATH only with prefer_system_tools). A JRE
		// or older Java is skipped, so selection installs the managed JDK
		// instead of freezing a runtime Ghidra would reject.
		req := m.javaRequirement()
		if bin := m.selectJava(req); bin != "" {
			return bin, nil
		}
		return "", fmt.Errorf("runtime %s not found — no installed JDK %d+ in Morgue's runtimes folder; install via Tools page", kind, req)
	}
	localBin := m.localRuntimeBin(kind)
	if _, err := os.Stat(localBin); err == nil {
		return localBin, nil
	}
	if !m.cfg.PreferSystemTools {
		return "", fmt.Errorf("runtime %s not found in Morgue's runtimes folder — install via Tools page (system runtimes are used only with prefer_system_tools)", kind)
	}
	sysName := runtimeSystemName(kind)
	if sysPath, err := exec.LookPath(sysName); err == nil {
		if kind == RuntimeAspNet && !systemDotNetHasAspNet10(sysPath) {
			return "", fmt.Errorf("runtime %s not found — system dotnet lacks .NET 10 ASP.NET Core", kind)
		}
		return sysPath, nil
	}
	return "", fmt.Errorf("runtime %s not found — install via Tools page", kind)
}

// managedJavaFeature is the Java feature release Morgue installs and the
// minimum it accepts from PATH. Ghidra 12.2 requires JDK 25
// (application.java.min=25) and its headless launcher demands a JDK, not a
// JRE; an older system Java would be selected, frozen for the run and then
// rejected by Ghidra after an automatic update.
const managedJavaFeature = 25

// javaVersionOf is the Java version probe (a seam for tests).
var javaVersionOf = func(bin string) string { return detectRuntimeVersion(RuntimeJava, bin) }

// javaMajor returns the feature release of a `java -version` string:
// "21.0.10" -> 21, "25" -> 25, legacy "1.8.0_402" -> 8; 0 when unparsable.
func javaMajor(v string) int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "1.")
	end := 0
	for end < len(v) && v[end] >= '0' && v[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(v[:end])
	if err != nil {
		return 0
	}
	return n
}

// systemDotNetHasAspNet10 reports whether the dotnet at dotnetPath hosts an
// ASP.NET Core 10.x runtime, by parsing `dotnet --list-runtimes`.
func systemDotNetHasAspNet10(dotnetPath string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, dotnetPath, "--list-runtimes")
	util.HideCmdWindow(cmd)
	util.ApplyChildEnv(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return listRuntimesHasAspNet10(string(out))
}

// listRuntimesHasAspNet10 is the pure parser for `dotnet --list-runtimes` output:
// it returns true when a Microsoft.AspNetCore.App 10.x runtime line is present.
// Lines look like: "Microsoft.AspNetCore.App 10.0.9 [C:\Program Files\dotnet\...]".
func listRuntimesHasAspNet10(listRuntimesOutput string) bool {
	for line := range strings.SplitSeq(listRuntimesOutput, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "Microsoft.AspNetCore.App" {
			continue
		}
		if strings.HasPrefix(fields[1], "10.") {
			return true
		}
	}
	return false
}

// InstallRuntime downloads and installs a portable runtime.
func (m *Manager) InstallRuntime(kind RuntimeKind, cb *InstallCallbacks) error {
	return m.InstallRuntimeContext(context.Background(), kind, cb)
}

func (m *Manager) InstallRuntimeContext(ctx context.Context, kind RuntimeKind, cb *InstallCallbacks) error {
	if m.selection != nil {
		s := m.selection
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(s.ctx, cancel) //nolint:contextcheck // ctx still derives from the caller; s.ctx (selection lifetime) only adds a second cancel source
		defer stop()
		defer cancel()
		s.work.Lock()
		defer s.work.Unlock()
		if err := s.ctx.Err(); err != nil {
			return err
		}
		_, err := s.selectRuntime(ctx, kind, cb)
		return err
	}
	lease, err := acquireToolLock(ctx, m.baseDir, "runtime:"+string(kind), true, false)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	return m.installRuntimeLocked(ctx, kind, cb)
}

func (m *Manager) installRuntimeInto(ctx context.Context, kind RuntimeKind, cb *InstallCallbacks) error {
	switch kind {
	case RuntimeDotnet:
		return m.installDotnetSDK(ctx, cb)
	case RuntimeJava:
		return m.installJavaJDK(ctx, cb)
	case RuntimeAspNet:
		return m.installAspNetRuntime(ctx, cb)
	default:
		return fmt.Errorf("unknown runtime: %s", kind)
	}
}

// detectRequiredDotnetVersion scans NuGet-installed tools for their target
// framework (e.g. tools/net10.0/any/) and returns the highest version found.
// Falls back to "10.0" if no framework folder is detected.
func detectRequiredDotnetVersion(toolsBaseDir string) string {
	fallback := "10.0"
	var versions []float64

	for _, t := range Registry {
		if t.Method != MethodNuGet {
			continue
		}
		toolsSubdir := filepath.Join(toolsBaseDir, t.Name, "tools")
		entries, err := os.ReadDir(toolsSubdir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name() // e.g. "net10.0"
			if !strings.HasPrefix(name, "net") {
				continue
			}
			verStr := strings.TrimPrefix(name, "net")
			if v, err := strconv.ParseFloat(verStr, 64); err == nil {
				versions = append(versions, v)
			}
		}
	}

	if len(versions) == 0 {
		return fallback
	}
	sort.Float64s(versions)
	highest := versions[len(versions)-1]
	// Format: preserve one decimal (10.0, 12.0, etc.)
	return strconv.FormatFloat(highest, 'f', 1, 64)
}

// installDotnetSDK downloads the .NET SDK portable zip, version auto-detected.
func (m *Manager) installDotnetSDK(ctx context.Context, cb *InstallCallbacks) error {
	destDir := m.localRuntimeDir(RuntimeDotnet)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dotnet dir: %w", err)
	}

	toolBase := m.baseDir
	if m.runtimeToolBase != "" {
		toolBase = m.runtimeToolBase
	}
	dotnetVersion := detectRequiredDotnetVersion(toolBase)
	zipPath := filepath.Join(m.baseDir, "runtimes", "dotnet-sdk.zip")
	url := fmt.Sprintf("https://aka.ms/dotnet/%s/dotnet-sdk-win-x64.zip", dotnetVersion)

	var progressCb func(bytesDown, bytesTotal int64)
	if cb != nil && cb.OnProgress != nil {
		progressCb = func(bytesDown, bytesTotal int64) {
			cb.OnProgress("dotnet-sdk", bytesDown, bytesTotal)
		}
	}

	if err := downloadFile(ctx, url, zipPath, progressCb); err != nil {
		return fmt.Errorf("download .NET SDK: %w", err)
	}
	defer func() { _ = os.Remove(zipPath) }()

	if err := extractZip(zipPath, destDir); err != nil {
		return fmt.Errorf("extract .NET SDK: %w", err)
	}

	// Verify
	bin := m.localRuntimeBin(RuntimeDotnet)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf(".NET SDK binary not found after extraction: %s", bin)
	}
	return nil
}

// adoptiumAsset represents a single asset from the Adoptium API response.
type adoptiumAsset struct {
	Binary struct {
		Package struct {
			Link string `json:"link"`
			Name string `json:"name"`
		} `json:"package"`
	} `json:"binary"`
}

// installJavaJDK downloads the Adoptium Temurin JDK feature release the run
// requires (javaRequirement: installed Ghidra's application.java.min, floor
// managedJavaFeature). A JDK: Ghidra's headless launcher rejects a JRE, and
// Native AOT recompiles need javac.
func (m *Manager) installJavaJDK(ctx context.Context, cb *InstallCallbacks) error {
	destDir := m.localRuntimeDir(RuntimeJava)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create java dir: %w", err)
	}

	// Query Adoptium API for download URL
	apiURL := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?os=windows&architecture=x64&image_type=jdk", m.javaRequirement())
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create adoptium request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch adoptium API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("adoptium API: HTTP %d", resp.StatusCode)
	}

	var assets []adoptiumAsset
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return fmt.Errorf("decode adoptium response: %w", err)
	}

	// Find the zip asset
	var downloadURL, fileName string
	for _, a := range assets {
		if strings.HasSuffix(a.Binary.Package.Name, ".zip") {
			downloadURL = a.Binary.Package.Link
			fileName = a.Binary.Package.Name
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no zip asset found in adoptium response")
	}

	zipPath := filepath.Join(m.baseDir, "runtimes", fileName)

	var progressCb func(bytesDown, bytesTotal int64)
	if cb != nil && cb.OnProgress != nil {
		progressCb = func(bytesDown, bytesTotal int64) {
			cb.OnProgress("java-jdk", bytesDown, bytesTotal)
		}
	}

	if err := downloadFile(ctx, downloadURL, zipPath, progressCb); err != nil {
		return fmt.Errorf("download Java JDK: %w", err)
	}
	defer func() { _ = os.Remove(zipPath) }()

	// Extract to temp dir first — Adoptium zips have a top-level dir
	tmpDir := filepath.Join(m.baseDir, "runtimes", "java-tmp")
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("create java tmp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := extractZip(zipPath, tmpDir); err != nil {
		return fmt.Errorf("extract Java JDK: %w", err)
	}

	// Find the top-level extracted directory (e.g. jdk-25.0.1+8)
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("read java tmp dir: %w", err)
	}

	var topDir string
	for _, e := range entries {
		if e.IsDir() {
			topDir = filepath.Join(tmpDir, e.Name())
			break
		}
	}
	if topDir == "" {
		return fmt.Errorf("no directory found in Java JDK archive")
	}

	// Move contents to destDir
	_ = os.RemoveAll(destDir)
	if err := os.Rename(topDir, destDir); err != nil {
		return fmt.Errorf("move java JDK: %w", err)
	}

	// Verify
	bin := m.localRuntimeBin(RuntimeJava)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("java JDK binary not found after extraction: %s", bin)
	}
	return nil
}

// aspNetRuntimeURL returns the .NET 10 ASP.NET Core runtime (win-x64) zip URL.
// The ASP.NET bundle is a superset of the base runtime, de-risking InspectorRedux
// which targets net10.0 framework-dependent.
func aspNetRuntimeURL() string {
	return "https://aka.ms/dotnet/10.0/aspnetcore-runtime-win-x64.zip"
}

// installAspNetRuntime downloads the portable .NET 10 ASP.NET runtime zip into
// baseDir/runtimes/dotnet-aspnet10/. Mirrors installDotnetSDK.
func (m *Manager) installAspNetRuntime(ctx context.Context, cb *InstallCallbacks) error {
	destDir := m.localRuntimeDir(RuntimeAspNet)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create aspnet dir: %w", err)
	}

	zipPath := filepath.Join(m.baseDir, "runtimes", "aspnetcore-runtime.zip")
	url := aspNetRuntimeURL()

	var progressCb func(bytesDown, bytesTotal int64)
	if cb != nil && cb.OnProgress != nil {
		progressCb = func(bytesDown, bytesTotal int64) {
			cb.OnProgress("dotnet-aspnet10", bytesDown, bytesTotal)
		}
	}

	if err := downloadFile(ctx, url, zipPath, progressCb); err != nil {
		return fmt.Errorf("download .NET ASP.NET runtime: %w", err)
	}
	defer func() { _ = os.Remove(zipPath) }()

	if err := extractZip(zipPath, destDir); err != nil {
		return fmt.Errorf("extract .NET ASP.NET runtime: %w", err)
	}

	bin := m.localRuntimeBin(RuntimeAspNet)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf(".NET ASP.NET runtime binary not found after extraction: %s", bin)
	}
	return nil
}
