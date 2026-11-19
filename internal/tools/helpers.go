package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// WithHelper serializes validation/build/publication and freezes the result.
func (m *Manager) WithHelper(ctx context.Context, name string, build func() (string, string, error)) (ToolIdentity, error) {
	if !KnownHelper(name) {
		return ToolIdentity{}, fmt.Errorf("unknown embedded helper: %s", name)
	}
	if m.selection == nil {
		return ToolIdentity{}, fmt.Errorf("helper identity requires a bound run manager")
	}
	s := m.selection
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel) //nolint:contextcheck // ctx still derives from the caller; s.ctx (selection lifetime) only adds a second cancel source
	defer stop()
	defer cancel()
	s.work.Lock()
	defer s.work.Unlock()
	if entry, ok, err := s.existing(name); ok || err != nil {
		return entry.id, err
	}
	// Runs can hold several helper leases in different discovery order. Waiting
	// for exclusive publication here would permit a cross-run lock cycle.
	lease, err := acquireToolLock(ctx, m.baseDir, "helper:"+name, true, false)
	if err != nil {
		return ToolIdentity{}, err
	}
	retained := false
	defer func() {
		if !retained {
			_ = lease.Close()
		}
	}()
	path, source, err := build()
	if err != nil {
		_ = lease.Close()
		return ToolIdentity{}, err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		_ = lease.Close()
		return ToolIdentity{}, fmt.Errorf("helper payload is not a regular file: %s (%w)", path, err)
	}
	if !withinRoot(m.baseDir, path) {
		_ = lease.Close()
		return ToolIdentity{}, fmt.Errorf("helper payload must be under tools directory")
	}
	if len(source) != 64 || strings.Trim(source, "0123456789abcdefABCDEF") != "" {
		_ = lease.Close()
		return ToolIdentity{}, fmt.Errorf("helper source identity must be SHA-256")
	}
	root := filepath.Dir(path)
	id, err := identityAt(ctx, ToolDef{Name: name}, root, applicationRevision()+"+source:"+source, true)
	if err != nil {
		_ = lease.Close()
		return ToolIdentity{}, err
	}
	id.Source = "embedded"
	s.attachRuntimeIdentity(&id, []RuntimeKind{RuntimeDotnet})
	if id.RuntimeSHA256 == "" {
		_ = lease.Close()
		return ToolIdentity{}, fmt.Errorf("select SDK runtime before publishing helper")
	}
	_ = lease.Close()
	lease, err = acquireToolLock(ctx, m.baseDir, "helper:"+name, false, true)
	if err != nil {
		return ToolIdentity{}, err
	}
	// Detect publication in the exclusive/shared gap; never record a stale hash.
	actual, err := inventoryHash(ctx, root)
	if err != nil || actual != id.PayloadSHA256 {
		_ = lease.Close()
		return ToolIdentity{}, fmt.Errorf("helper changed during lease acquisition: %s (%w)", name, err)
	}
	entry := selectedPayload{id: id, path: path, root: root, lease: lease}
	if err = s.put(entry); err != nil {
		return ToolIdentity{}, err
	}
	retained = true
	return id, nil
}

func applicationRevision() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return setting.Value
			}
		}
	}
	return "unknown"
}

func (m *Manager) RecordHelper(ctx context.Context, name, path, sourceSHA256 string) (ToolIdentity, error) {
	return m.WithHelper(ctx, name, func() (string, string, error) { return path, sourceSHA256, nil })
}
