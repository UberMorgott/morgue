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
	"strings"
	"time"
)

// RuntimeKind identifies a runtime dependency.
type RuntimeKind string

const (
	RuntimeDotnet RuntimeKind = "dotnet"
	RuntimeJava   RuntimeKind = "java"
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

	allKinds := []RuntimeKind{RuntimeDotnet, RuntimeJava}
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
			statuses = append(statuses, st)
			continue
		}

		// Fall back to system
		sysName := runtimeSystemName(kind)
		if sysPath, err := exec.LookPath(sysName); err == nil {
			st.Available = true
			st.Path = sysPath
			st.Local = false
			st.Version = detectRuntimeVersion(kind, sysPath)
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
	default:
		return ""
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	text := strings.TrimSpace(string(out))
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
func (m *Manager) RuntimePath(kind RuntimeKind) (string, error) {
	localBin := m.localRuntimeBin(kind)
	if _, err := os.Stat(localBin); err == nil {
		return localBin, nil
	}
	sysName := runtimeSystemName(kind)
	if sysPath, err := exec.LookPath(sysName); err == nil {
		return sysPath, nil
	}
	return "", fmt.Errorf("runtime %s not found — install via Tools page", kind)
}

// InstallRuntime downloads and installs a portable runtime.
func (m *Manager) InstallRuntime(kind RuntimeKind) error {
	switch kind {
	case RuntimeDotnet:
		return m.installDotnetSDK()
	case RuntimeJava:
		return m.installJavaJRE()
	default:
		return fmt.Errorf("unknown runtime: %s", kind)
	}
}

// installDotnetSDK downloads the .NET 8 SDK portable zip.
func (m *Manager) installDotnetSDK() error {
	destDir := m.localRuntimeDir(RuntimeDotnet)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dotnet dir: %w", err)
	}

	zipPath := filepath.Join(m.baseDir, "runtimes", "dotnet-sdk.zip")
	url := "https://aka.ms/dotnet/8.0/dotnet-sdk-win-x64.zip"

	opts := DownloadOptions{
		URL:        url,
		DestPath:   zipPath,
		Retries:    m.cfg.DownloadRetries,
		TimeoutMin: m.cfg.DownloadTimeoutMinutes,
	}
	if m.OnProgress != nil {
		opts.OnProgress = func(bytesDown, bytesTotal int64) {
			m.OnProgress("dotnet-sdk", bytesDown, bytesTotal)
		}
	}

	if err := downloadFile(opts); err != nil {
		return fmt.Errorf("download .NET SDK: %w", err)
	}
	defer os.Remove(zipPath)

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

// installJavaJRE downloads Adoptium Temurin JRE 21.
func (m *Manager) installJavaJRE() error {
	destDir := m.localRuntimeDir(RuntimeJava)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create java dir: %w", err)
	}

	// Query Adoptium API for download URL
	apiURL := "https://api.adoptium.net/v3/assets/latest/21/hotspot?os=windows&architecture=x64&image_type=jre"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create adoptium request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch adoptium API: %w", err)
	}
	defer resp.Body.Close()

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
	opts := DownloadOptions{
		URL:        downloadURL,
		DestPath:   zipPath,
		Retries:    m.cfg.DownloadRetries,
		TimeoutMin: m.cfg.DownloadTimeoutMinutes,
	}
	if m.OnProgress != nil {
		opts.OnProgress = func(bytesDown, bytesTotal int64) {
			m.OnProgress("java-jre", bytesDown, bytesTotal)
		}
	}

	if err := downloadFile(opts); err != nil {
		return fmt.Errorf("download Java JRE: %w", err)
	}
	defer os.Remove(zipPath)

	// Extract to temp dir first — Adoptium zips have a top-level dir
	tmpDir := filepath.Join(m.baseDir, "runtimes", "java-tmp")
	os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("create java tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractZip(zipPath, tmpDir); err != nil {
		return fmt.Errorf("extract Java JRE: %w", err)
	}

	// Find the top-level extracted directory (e.g. jdk-21.0.3+9-jre)
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
		return fmt.Errorf("no directory found in Java JRE archive")
	}

	// Move contents to destDir
	os.RemoveAll(destDir)
	if err := os.Rename(topDir, destDir); err != nil {
		return fmt.Errorf("move java JRE: %w", err)
	}

	// Verify
	bin := m.localRuntimeBin(RuntimeJava)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("Java JRE binary not found after extraction: %s", bin)
	}
	return nil
}
