package tools

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const lockFile = "tools.lock.json"

// lockMu serializes the read-modify-write of tools.lock.json so concurrent
// installs of different tools don't clobber each other's entries.
var lockMu sync.Mutex

// Lock records the exact installed tag per tool for reproducible setup.
type Lock struct {
	Tools map[string]string `json:"tools"`
}

// ReadLock loads tools.lock.json from baseDir. A missing file yields an empty
// (non-nil) lock with no error. A corrupt (unparseable) file is treated as
// recoverable: the parse error is logged and an empty lock is returned with the
// error surfaced so callers can distinguish corruption from "missing".
func ReadLock(baseDir string) (Lock, error) {
	lk := Lock{Tools: map[string]string{}}
	data, err := os.ReadFile(filepath.Clean(filepath.Join(baseDir, lockFile)))
	if err != nil {
		if os.IsNotExist(err) {
			return lk, nil
		}
		return lk, err
	}
	if err := json.Unmarshal(data, &lk); err != nil {
		log.Printf("tools: corrupt %s in %s (%v); treating as empty lock", lockFile, baseDir, err)
		return Lock{Tools: map[string]string{}}, err
	}
	if lk.Tools == nil {
		lk.Tools = map[string]string{}
	}
	return lk, nil
}

// WriteLock persists the lock to baseDir/tools.lock.json atomically: it writes a
// temp file in the same directory then renames it over the target, so a crash
// mid-write can't leave a truncated lock.
func WriteLock(baseDir string, lk Lock) error {
	if lk.Tools == nil {
		lk.Tools = map[string]string{}
	}
	data, err := json.MarshalIndent(lk, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(baseDir, lockFile)
	tmp, err := os.CreateTemp(baseDir, lockFile+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// RecordInstall updates the lock with a tool's installed tag and persists it.
// The read-modify-write is mutex-guarded and preserves all existing entries.
// A corrupt lock is recovered (ReadLock logs it and yields an empty map) rather
// than aborting the install record.
func (m *Manager) RecordInstall(name, tag string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	lk, _ := ReadLock(m.baseDir)
	lk.Tools[name] = tag
	return WriteLock(m.baseDir, lk)
}
