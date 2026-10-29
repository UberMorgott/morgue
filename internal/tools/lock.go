package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const lockFile = "tools.lock.json"

// Lock is the legacy Go view. Disk writes always use InstalledLock v2.
type Lock struct {
	Tools map[string]string `json:"tools"`
}

func emptyInstalledLock() InstalledLock {
	return InstalledLock{SchemaVersion: InstalledLockVersion, Tools: map[string]ToolIdentity{}, OperatorVersions: map[string]string{}}
}

func ReadInstalledLock(baseDir string) (InstalledLock, error) {
	lk := emptyInstalledLock()
	data, err := os.ReadFile(filepath.Join(baseDir, lockFile))
	if os.IsNotExist(err) {
		return lk, nil
	}
	if err != nil {
		return lk, err
	}
	var raw struct {
		SchemaVersion    int                        `json:"schemaVersion"`
		Tools            map[string]json.RawMessage `json:"tools"`
		OperatorVersions map[string]string          `json:"operatorVersions"`
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		return lk, err
	}
	if raw.SchemaVersion != 0 && raw.SchemaVersion != InstalledLockVersion {
		return lk, fmt.Errorf("unsupported installed lock version %d", raw.SchemaVersion)
	}
	for name, value := range raw.Tools {
		var tag string
		if json.Unmarshal(value, &tag) == nil {
			lk.Tools[name] = ToolIdentity{Name: name, Version: observedVersion(tag), Source: "unknown", Managed: true, HashScope: "not-collected"}
			continue
		}
		var id ToolIdentity
		if err = json.Unmarshal(value, &id); err != nil {
			return emptyInstalledLock(), err
		}
		id.Name = name
		lk.Tools[name] = id
	}
	if raw.OperatorVersions != nil {
		lk.OperatorVersions = raw.OperatorVersions
	}
	return lk, nil
}

func ReadLock(baseDir string) (Lock, error) {
	installed, err := ReadInstalledLock(baseDir)
	lk := Lock{Tools: map[string]string{}}
	if err != nil {
		log.Printf("tools: corrupt %s (%v)", lockFile, err)
		return lk, err
	}
	for name, id := range installed.Tools {
		lk.Tools[name] = id.Version
	}
	return lk, nil
}

func writeInstalledLock(baseDir string, lk InstalledLock) error {
	lk.SchemaVersion = InstalledLockVersion
	data, err := json.MarshalIndent(lk, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(baseDir, lockFile+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	cerr := tmp.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	return os.Rename(name, filepath.Join(baseDir, lockFile))
}

func WriteLock(baseDir string, lk Lock) error {
	lease, err := acquireToolLock(context.Background(), baseDir, "installed-state", true, true)
	if err != nil {
		return err
	}
	defer lease.Close()
	next, _ := ReadInstalledLock(baseDir)
	for name, version := range lk.Tools {
		id := next.Tools[name]
		id.Name = name
		id.Version = observedVersion(version)
		if id.Source == "" {
			id.Source = "unknown"
			id.Managed = true
			id.HashScope = "not-collected"
		}
		next.Tools[name] = id
	}
	return writeInstalledLock(baseDir, next)
}

// recordIdentity holds a cross-process state lock independently of payload locks.
// The optional pin pointer means set/clear; nil preserves an operator decision.
func (m *Manager) recordIdentity(ctx context.Context, id ToolIdentity, pin *string) error {
	lease, err := acquireToolLock(ctx, m.baseDir, "installed-state", true, true)
	if err != nil {
		return err
	}
	defer lease.Close()
	lk, readErr := ReadInstalledLock(m.baseDir)
	if readErr != nil {
		var syntax *json.SyntaxError
		if !errors.As(readErr, &syntax) {
			return readErr
		}
	}
	lk.Tools[id.Name] = id
	if pin != nil {
		if *pin == "" {
			delete(lk.OperatorVersions, id.Name)
		} else {
			lk.OperatorVersions[id.Name] = *pin
		}
	}
	return writeInstalledLock(m.baseDir, lk)
}

func (m *Manager) RecordInstall(name, tag string) error {
	return m.recordIdentity(context.Background(), ToolIdentity{Name: name, Version: observedVersion(tag), Source: "unknown", Managed: true, HashScope: "not-collected"}, nil)
}
