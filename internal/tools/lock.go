package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const lockFile = "tools.lock.json"

// Lock records the exact installed tag per tool for reproducible setup.
type Lock struct {
	Tools map[string]string `json:"tools"`
}

// ReadLock loads tools.lock.json from baseDir. A missing file yields an empty
// (non-nil) lock with no error.
func ReadLock(baseDir string) (Lock, error) {
	lk := Lock{Tools: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(baseDir, lockFile))
	if err != nil {
		if os.IsNotExist(err) {
			return lk, nil
		}
		return lk, err
	}
	if err := json.Unmarshal(data, &lk); err != nil {
		return Lock{Tools: map[string]string{}}, err
	}
	if lk.Tools == nil {
		lk.Tools = map[string]string{}
	}
	return lk, nil
}

// WriteLock persists the lock to baseDir/tools.lock.json.
func WriteLock(baseDir string, lk Lock) error {
	if lk.Tools == nil {
		lk.Tools = map[string]string{}
	}
	data, err := json.MarshalIndent(lk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(baseDir, lockFile), data, 0644)
}

// RecordInstall updates the lock with a tool's installed tag and persists it.
func (m *Manager) RecordInstall(name, tag string) error {
	lk, err := ReadLock(m.baseDir)
	if err != nil {
		return err
	}
	lk.Tools[name] = tag
	return WriteLock(m.baseDir, lk)
}
