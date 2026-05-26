package tools

import (
	"fmt"
	"os"
	"path/filepath"

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
		return "", fmt.Errorf("tool %s not installed (expected at %s)", name, path)
	}
	return path, nil
}

// Check returns the installation status of a named tool.
func (m *Manager) Check(name string) ToolStatus {
	tool, ok := FindByName(name)
	if !ok {
		return ToolStatus{Name: name}
	}

	path := filepath.Join(m.baseDir, name, tool.Binary)
	_, err := os.Stat(path)
	return ToolStatus{
		Name:        name,
		Installed:   err == nil,
		Path:        path,
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

// Install downloads and installs a tool. Returns an error if the tool is unknown.
func (m *Manager) Install(name string) error {
	tool, ok := FindByName(name)
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}

	destDir := filepath.Join(m.baseDir, name)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create tool dir: %w", err)
	}

	opts := DownloadOptions{
		Retries:    m.cfg.DownloadRetries,
		TimeoutMin: m.cfg.DownloadTimeoutMinutes,
	}
	if m.OnProgress != nil {
		opts.OnProgress = func(bytesDown, bytesTotal int64) {
			m.OnProgress(name, bytesDown, bytesTotal)
		}
	}

	switch tool.Method {
	case MethodGitHubRelease:
		return installFromGitHub(opts, tool, destDir)
	case MethodDirectURL:
		return installFromURL(opts, tool, destDir)
	case MethodDotnetTool:
		return installDotnetTool(tool, destDir)
	default:
		return fmt.Errorf("unsupported install method for %s", name)
	}
}

// CheckAllWithUpdates returns status of all tools including latest GitHub versions.
func (m *Manager) CheckAllWithUpdates() []ToolStatus {
	statuses := make([]ToolStatus, 0, len(Registry))
	for _, t := range Registry {
		st := m.Check(t.Name)
		st.Category = t.Category.String()
		st.Description = t.Description
		st.Optional = t.Optional

		if t.Method == MethodGitHubRelease && t.Repo != "" {
			release, err := fetchLatestRelease(t.Repo)
			if err == nil {
				st.LatestVersion = release.TagName
				if st.Installed && st.Version != "" && st.Version != release.TagName {
					st.UpdateAvailable = true
				}
			}
		}
		statuses = append(statuses, st)
	}
	return statuses
}

// RuntimeEnv returns environment variables for local runtimes (PATH prepend, JAVA_HOME).
// These should be passed to RunCmdWithEnv when executing tools that need runtimes.
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
		// Prepend java bin to PATH (after dotnet if present)
		for i, e := range env {
			if len(e) > 5 && e[:5] == "PATH=" {
				env[i] = fmt.Sprintf("PATH=%s%c%s", javaBinDir, os.PathListSeparator, e[5:])
				break
			}
		}
		if len(env) == 1 {
			// No PATH entry yet, add one
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
