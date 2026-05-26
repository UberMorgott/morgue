package util

import (
	"os"
	"path/filepath"
)

// BaseDir returns the directory containing the morgue executable.
func BaseDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// ToolDir returns the directory for a named tool under BaseDir/tools/.
func ToolDir(name string) string {
	return filepath.Join(BaseDir(), "tools", name)
}

// ToolPath returns the full path to an executable within a tool directory.
func ToolPath(name, exe string) string {
	return filepath.Join(ToolDir(name), exe)
}

// ConfigPath returns the default config file path.
func ConfigPath() string {
	return filepath.Join(BaseDir(), "morgue.yaml")
}
