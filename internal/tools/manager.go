package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// Manager handles tool installation and resolution.
type Manager struct {
	baseDir string
}

// NewManager creates a Manager that stores tools under baseDir.
func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
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
		Name:      name,
		Installed: err == nil,
		Path:      path,
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

	switch tool.Method {
	case MethodGitHubRelease:
		return installFromGitHub(tool, destDir)
	case MethodDirectURL:
		return installFromURL(tool, destDir)
	case MethodDotnetTool:
		return installDotnetTool(tool, destDir)
	default:
		return fmt.Errorf("unsupported install method for %s", name)
	}
}
