package tools

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/config"
)

// Manager handles tool installation and resolution.
type Manager struct {
	baseDir    string
	cfg        config.Config
	OnProgress func(tool string, bytesDown, bytesTotal int64)
}

// NewManager creates a Manager that stores tools under baseDir.
func NewManager(baseDir string, cfg config.Config) *Manager {
	return &Manager{baseDir: baseDir, cfg: cfg}
}

// Resolve returns the full path to a tool's binary, or an error if not installed.
func (m *Manager) Resolve(name string) (string, error) {
	tool, ok := FindByName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
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
			return nil
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

	// Read version from .version file
	var version string
	versionBytes, readErr := os.ReadFile(filepath.Join(m.baseDir, name, ".version"))
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
func (m *Manager) Install(name string) (string, error) {
	tool, ok := FindByName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	destDir := filepath.Join(m.baseDir, name)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("create tool dir: %w", err)
	}

	var progressCb func(bytesDown, bytesTotal int64)
	if m.OnProgress != nil {
		progressCb = func(bytesDown, bytesTotal int64) {
			m.OnProgress(name, bytesDown, bytesTotal)
		}
	}

	switch tool.Method {
	case MethodGitHubRelease:
		version, err := installFromGitHub(tool, destDir, m.cfg.GitHubToken, progressCb)
		return version, err
	case MethodDirectURL:
		err := installFromURL(tool, destDir, progressCb)
		if err != nil {
			return "", err
		}
		versionFile := filepath.Join(destDir, ".version")
		os.WriteFile(versionFile, []byte("latest"), 0644)
		return "latest", nil
	case MethodDotnetTool:
		err := installDotnetTool(tool, destDir)
		if err != nil {
			return "", err
		}
		versionFile := filepath.Join(destDir, ".version")
		os.WriteFile(versionFile, []byte("dotnet"), 0644)
		return "dotnet", nil
	case MethodGitBuild:
		version, err := installFromGitBuild(tool, destDir, m.OnProgress)
		return version, err
	default:
		return "", fmt.Errorf("unsupported install method for %s", name)
	}
}

// cleanVersionTag normalizes a release tag for display.
func cleanVersionTag(tag string) string {
	tag = strings.TrimPrefix(tag, "v")
	tag = strings.TrimPrefix(tag, "V")
	// Ghidra-specific: "Ghidra_12.1_build" → "12.1"
	if strings.HasPrefix(tag, "Ghidra_") {
		tag = strings.TrimPrefix(tag, "Ghidra_")
		tag = strings.TrimSuffix(tag, "_build")
	}
	if strings.EqualFold(tag, "latest") || tag == "releases" || tag == "" {
		return ""
	}
	return tag
}

// CheckAllWithUpdates returns status of all tools including latest GitHub versions.
// Uses HTTP redirect to check versions — no GitHub API calls, no rate limit.
func (m *Manager) CheckAllWithUpdates() []ToolStatus {
	statuses := make([]ToolStatus, 0, len(Registry))
	for _, t := range Registry {
		st := m.Check(t.Name)

		// Clean up stored version for display
		st.Version = cleanVersionTag(st.Version)

		switch {
		case t.Method == MethodGitHubRelease && t.Repo != "":
			tagName, err := fetchLatestVersion(t.Repo)
			if err == nil {
				tagName = cleanVersionTag(tagName)
				st.LatestVersion = tagName
				if st.Installed && st.Version != "" && st.Version != tagName {
					st.UpdateAvailable = true
				}
			}
			// On error: LatestVersion stays empty, frontend shows "–"
		case t.Method == MethodDotnetTool && t.DotnetID != "":
			ver, err := fetchNuGetLatestVersion(t.DotnetID)
			if err == nil {
				st.LatestVersion = ver
			}
		case t.Method == MethodGitBuild && t.Repo != "":
			commit, err := fetchLatestCommit(t.Repo)
			if err == nil && commit != "" {
				st.LatestVersion = commit
				if st.Installed && st.Version != "" && st.Version != commit {
					st.UpdateAvailable = true
				}
			}
		case t.Method == MethodDirectURL && t.URL != "":
			resp, err := http.Head(t.URL)
			if err == nil {
				resp.Body.Close()
				if lm := resp.Header.Get("Last-Modified"); lm != "" {
					if parsed, err := time.Parse(time.RFC1123, lm); err == nil {
						st.LatestVersion = parsed.Format("2006.01.02")
					}
				}
			}
			if st.LatestVersion == "" {
				st.LatestVersion = "–"
			}
		}
		statuses = append(statuses, st)
	}
	return statuses
}

// CheckLatestVersionSingle checks latest version for one tool by name.
// Returns the latest version string and whether an update is available.
func (m *Manager) CheckLatestVersionSingle(name string) (latestVersion string, updateAvailable bool) {
	tool, ok := FindByName(name)
	if !ok {
		return "", false
	}

	st := m.Check(name)
	installedVersion := cleanVersionTag(st.Version)

	switch {
	case tool.Method == MethodGitHubRelease && tool.Repo != "":
		ver, err := fetchLatestVersion(tool.Repo)
		if err == nil {
			latestVersion = cleanVersionTag(ver)
		}
	case tool.Method == MethodGitBuild && tool.Repo != "":
		commit, err := fetchLatestCommit(tool.Repo)
		if err == nil {
			latestVersion = commit
		}
	case tool.Method == MethodDotnetTool && tool.DotnetID != "":
		ver, err := fetchNuGetLatestVersion(tool.DotnetID)
		if err == nil {
			latestVersion = ver
		}
	case tool.Method == MethodDirectURL && tool.URL != "":
		resp, err := http.Head(tool.URL)
		if err == nil {
			resp.Body.Close()
			if lm := resp.Header.Get("Last-Modified"); lm != "" {
				if parsed, err := time.Parse(time.RFC1123, lm); err == nil {
					latestVersion = parsed.Format("2006.01.02")
				}
			}
		}
		if latestVersion == "" {
			latestVersion = "–"
		}
	}

	if st.Installed && installedVersion != "" && latestVersion != "" && installedVersion != latestVersion {
		updateAvailable = true
	}
	return
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

// Delete removes a tool's directory from disk.
func (m *Manager) Delete(name string) error {
	_, ok := FindByName(name)
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}
	return os.RemoveAll(filepath.Join(m.baseDir, name))
}
