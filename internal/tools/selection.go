package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/UberMorgott/morgue/internal/util"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type selectedPayload struct {
	id         ToolIdentity
	path, root string
	lease      *toolLease
}

type RunSelection struct {
	manager          *Manager
	ctx              context.Context
	cancel           context.CancelFunc
	work             sync.Mutex
	mu               sync.Mutex
	selected         map[string]selectedPayload
	failures         map[string]error
	complete, closed bool
}

func (m *Manager) NewRunSelection(ctx context.Context) (*RunSelection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.selection != nil {
		return nil, fmt.Errorf("manager is already bound")
	}
	if err := os.MkdirAll(m.baseDir, 0755); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	return &RunSelection{manager: m, ctx: runCtx, cancel: cancel, selected: map[string]selectedPayload{}, failures: map[string]error{}}, nil
}

func (s *RunSelection) Bind() *Manager {
	return &Manager{baseDir: s.manager.baseDir, cfg: s.manager.cfg, state: s.manager.state, selection: s}
}

func (s *RunSelection) Snapshot(complete bool) RunToolsLock {
	if complete {
		s.work.Lock()
		defer s.work.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if complete {
		s.complete = true
	}
	lock := RunToolsLock{SchemaVersion: RunLockVersion, Complete: s.complete, Tools: []ToolIdentity{}}
	for _, entry := range s.selected {
		lock.Tools = append(lock.Tools, entry.id)
	}
	sort.Slice(lock.Tools, func(i, j int) bool { return lock.Tools[i].Name < lock.Tools[j].Name })
	return lock
}

func (s *RunSelection) Close() error {
	s.cancel()
	s.work.Lock()
	defer s.work.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var errs []error
	for name, entry := range s.selected {
		if err := entry.lease.Close(); err != nil {
			errs = append(errs, err)
		}
		entry.lease = nil
		s.selected[name] = entry
	}
	return errors.Join(errs...)
}

func (s *RunSelection) existing(name string) (selectedPayload, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return selectedPayload{}, false, ErrSelectionClosed
	}
	entry, ok := s.selected[name]
	if !ok && s.complete {
		return selectedPayload{}, false, fmt.Errorf("tool acquisition is sealed: %s", name)
	}
	return entry, ok, nil
}

func (s *RunSelection) put(entry selectedPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.complete {
		_ = entry.lease.Close()
		return ErrSelectionClosed
	}
	s.selected[entry.id.Name] = entry
	return nil
}

func (s *RunSelection) Ensure(ctx context.Context, names []string, cb *InstallCallbacks) ([]ToolIdentity, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel) //nolint:contextcheck // ctx still derives from the caller; s.ctx (selection lifetime) only adds a second cancel source
	defer stop()
	defer cancel()
	s.work.Lock()
	defer s.work.Unlock()
	result := []ToolIdentity{}
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		if err := s.ctx.Err(); err != nil {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		entry, ok, err := s.existing(name)
		if err != nil {
			return result, err
		}
		if !ok {
			if previous := s.failures[name]; previous != nil {
				return result, previous
			}
			entry, err = s.selectTool(ctx, name, cb)
			if err != nil {
				s.failures[name] = err
				return result, err
			}
		} else if cb != nil && cb.OnStatus != nil {
			s.manager.state.mu.Lock()
			st := s.manager.state.statuses[name]
			s.manager.state.mu.Unlock()
			if st.Name != "" {
				cb.OnStatus(st)
			}
		}
		if err = s.verify(ctx, entry); err != nil {
			return result, err
		}
		result = append(result, entry.id)
	}
	return result, nil
}

func (s *RunSelection) verify(ctx context.Context, entry selectedPayload) error {
	// Managed payloads cannot change through Morgue while leased. External
	// payloads have no enforceable lease against other package managers.
	if !entry.id.Managed {
		hash, err := inventoryHash(ctx, entry.root)
		if err != nil {
			return err
		}
		if hash != entry.id.PayloadSHA256 {
			return fmt.Errorf("selected external payload changed: %s", entry.id.Name)
		}
	}
	for pair := range strings.SplitSeq(entry.id.RuntimeVersion, ";") {
		name, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		s.mu.Lock()
		dep, exists := s.selected[name]
		s.mu.Unlock()
		if exists {
			if err := s.verify(ctx, dep); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *RunSelection) resolve(name string) (string, error) {
	if _, err := s.Ensure(s.ctx, []string{name}, nil); err != nil {
		return "", err
	}
	s.mu.Lock()
	entry := s.selected[name]
	s.mu.Unlock()
	if _, err := os.Stat(entry.path); err != nil && (name != "ghidra" || validateGhidraHome(filepath.Dir(entry.path)) == "") {
		return "", err
	}
	return entry.path, nil
}

func (s *RunSelection) selectTool(ctx context.Context, name string, cb *InstallCallbacks) (selectedPayload, error) {
	m := s.manager
	t, ok := FindByName(name)
	if !ok {
		return selectedPayload{}, fmt.Errorf("unknown tool: %s", name)
	}
	// Runtime leases are acquired before a tool mutation lock; no inverse lock
	// ordering exists in installers. Bound runtime paths never reselect later.
	deps := append([]RuntimeKind(nil), t.RuntimeDeps...)
	if t.Name == "ghidra" {
		deps = append(deps, RuntimeJava)
	}
	if strings.EqualFold(filepath.Ext(t.Binary), ".jar") {
		deps = append(deps, RuntimeJava)
	}
	if strings.EqualFold(filepath.Ext(t.Binary), ".dll") && len(deps) == 0 {
		deps = append(deps, RuntimeDotnet)
	}
	for _, kind := range deps {
		if _, err := s.selectRuntime(ctx, kind, cb); err != nil {
			return selectedPayload{}, err
		}
	}
	lease, err := acquireToolLock(ctx, m.baseDir, name, true, false)
	exclusive := true
	if errors.Is(err, ErrInUse) {
		exclusive = false
		lease, err = acquireToolLock(ctx, m.baseDir, name, false, true)
	}
	if err != nil {
		return selectedPayload{}, err
	}
	retained := false
	defer func() {
		if !retained {
			_ = lease.Close()
		}
	}()
	st := m.checkUnbound(name)
	_, resolveErr := m.resolveUnbound(name)
	external := resolveErr == nil && (st.system || (name == "ghidra" && preSeededGhidraHome(m.cfg) != ""))
	policy := ToolPolicyStatus{Name: name, Freshness: "checking"}
	lk, err := ReadInstalledLock(m.baseDir)
	if err != nil {
		_ = lease.Close()
		return selectedPayload{}, err
	}
	policy.OperatorVersion = lk.OperatorVersions[name]
	if policy.OperatorVersion != "" {
		t, err = toolWithVersion(t, policy.OperatorVersion)
		if err != nil {
			_ = lease.Close()
			return selectedPayload{}, err
		}
	}
	m.report(policy, cb)
	if external {
		policy.Freshness = "system"
	} else {
		latest, probeErr := freshVersion(ctx, t)
		if probeErr == nil {
			pinned := t.Version != "" || t.DotnetVersion != ""
			needs := !st.Installed || observedVersion(st.Version) == "unknown" || latest == "unknown" || st.Version != latest
			if needs {
				resolved, e := resolvedToolVersion(t, latest)
				if !exclusive {
					e = fmt.Errorf("update deferred while another run leases this tool: %w", ErrInUse)
				}
				if e == nil {
					_, e = m.installLocked(ctx, resolved, latest, nil, cb)
				}
				if e != nil {
					probeErr = e
				} else {
					policy.Freshness = "updated"
				}
			} else {
				policy.Freshness = "current"
			}
			if probeErr == nil && pinned {
				policy.Freshness = "pinned"
			}
		}
		if probeErr != nil {
			if ctx.Err() != nil {
				_ = lease.Close()
				return selectedPayload{}, ctx.Err()
			}
			if !st.Installed {
				_ = lease.Close()
				policy.Freshness = "error"
				policy.Warning = probeErr.Error()
				m.report(policy, cb)
				return selectedPayload{}, probeErr
			}
			policy.Freshness = "warning"
			policy.Warning = fmt.Sprintf("freshness/update failed; using installed payload: %v", probeErr)
		}
	}
	if exclusive {
		_ = lease.Close()
		// Re-open shared before observing the final identity. A competing update
		// in the downgrade gap is therefore recorded, never mislabeled.
		lease, err = acquireToolLock(ctx, m.baseDir, name, false, true)
		if err != nil {
			return selectedPayload{}, err
		}
	}
	path, err := m.resolveUnbound(name)
	if err != nil {
		_ = lease.Close()
		return selectedPayload{}, err
	}
	st = m.checkUnbound(name)
	root := filepath.Join(m.baseDir, name)
	if external {
		if name == "ghidra" {
			root, err = physicalToolPath(filepath.Dir(path))
			path = filepath.Join(root, filepath.Base(path))
		} else {
			path, err = physicalToolPath(path)
			root = filepath.Dir(path)
		}
		if err != nil {
			_ = lease.Close()
			return selectedPayload{}, err
		}
	}
	id, err := identityAt(ctx, t, root, st.Version, !external)
	if err != nil {
		_ = lease.Close()
		return selectedPayload{}, err
	}
	if external {
		id.Version = "unknown"
	}
	s.attachRuntimeIdentity(&id, deps)
	entry := selectedPayload{id: id, path: path, root: root, lease: lease}
	if !external {
		if err = m.recordIdentity(ctx, id, nil); err != nil {
			_ = lease.Close()
			return selectedPayload{}, err
		}
	}
	if err = s.put(entry); err != nil {
		return selectedPayload{}, err
	}
	retained = true
	policy.InUse = true
	m.report(policy, cb)
	return entry, nil
}

func (s *RunSelection) attachRuntimeIdentity(id *ToolIdentity, deps []RuntimeKind) {
	seen := map[string]bool{}
	var versions, hashes []string
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, kind := range deps {
		key := "runtime:" + string(kind)
		if seen[key] {
			continue
		}
		seen[key] = true
		if dep, ok := s.selected[key]; ok {
			versions = append(versions, key+"="+dep.id.Version)
			hashes = append(hashes, key+"="+dep.id.PayloadSHA256)
		}
	}
	sort.Strings(versions)
	sort.Strings(hashes)
	id.RuntimeVersion = strings.Join(versions, ";")
	if len(hashes) > 0 {
		hash := sha256.Sum256([]byte(strings.Join(hashes, "\n")))
		id.RuntimeSHA256 = hex.EncodeToString(hash[:])
	}
}

func (s *RunSelection) selectRuntime(ctx context.Context, kind RuntimeKind, cb *InstallCallbacks) (selectedPayload, error) {
	name := "runtime:" + string(kind)
	if entry, ok, err := s.existing(name); ok || err != nil {
		if err == nil {
			err = s.verify(ctx, entry)
		}
		return entry, err
	}
	if runtimeBinary(kind) == "" {
		return selectedPayload{}, fmt.Errorf("unknown runtime: %s", kind)
	}
	m := s.manager
	lease, err := acquireToolLock(ctx, m.baseDir, name, false, true)
	if err != nil {
		return selectedPayload{}, err
	}
	retained := false
	defer func() {
		if !retained {
			_ = lease.Close()
		}
	}()
	_, err = m.runtimePathUnbound(kind)
	if err != nil {
		_ = lease.Close()
		lease, err = acquireToolLock(ctx, m.baseDir, name, true, true)
		if err != nil {
			return selectedPayload{}, err
		}
		_, err = m.runtimePathUnbound(kind)
		if err != nil {
			err = m.installRuntimeLocked(ctx, kind, cb)
		}
		if err == nil {
			_, err = m.runtimePathUnbound(kind)
		}
		_ = lease.Close()
		if err != nil {
			return selectedPayload{}, err
		}
		lease, err = acquireToolLock(ctx, m.baseDir, name, false, true)
		if err != nil {
			return selectedPayload{}, err
		}
	}
	path, err := m.runtimePathUnbound(kind)
	if err != nil {
		_ = lease.Close()
		return selectedPayload{}, err
	}
	managed := withinRoot(m.localRuntimeDir(kind), path)
	root := m.localRuntimeDir(kind)
	if !managed {
		path, err = physicalToolPath(path)
		if err != nil {
			_ = lease.Close()
			return selectedPayload{}, err
		}
		root = filepath.Dir(path)
		if kind == RuntimeJava {
			root = filepath.Dir(root)
		}
	}
	version := detectRuntimeVersion(kind, path)
	id, err := identityAt(ctx, ToolDef{Name: name}, root, version, managed)
	if err != nil {
		_ = lease.Close()
		return selectedPayload{}, err
	}
	id.Source = "runtime"
	entry := selectedPayload{id: id, path: path, root: root, lease: lease}
	if err = s.put(entry); err != nil {
		return selectedPayload{}, err
	}
	retained = true
	return entry, nil
}

func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (s *RunSelection) runtimeEnv() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var dirs []string
	var env []string
	if entry, ok := s.selected["runtime:dotnet"]; ok {
		dirs = append(dirs, filepath.Dir(entry.path))
	}
	if entry, ok := s.selected["runtime:java"]; ok {
		bin := filepath.Dir(entry.path)
		dirs = append(dirs, bin)
		env = append(env, "JAVA_HOME="+filepath.Dir(bin))
	}
	if entry, ok := s.selected["runtime:dotnet-aspnet10"]; ok {
		dirs = append(dirs, filepath.Dir(entry.path))
	}
	if len(dirs) > 0 {
		env = append(env, "PATH="+strings.Join(dirs, string(os.PathListSeparator))+string(os.PathListSeparator)+util.ChildPath())
	}
	return env
}
