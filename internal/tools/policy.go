package tools

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type policyState struct {
	mu       sync.Mutex
	statuses map[string]ToolPolicyStatus
}

var freshnessSlots = make(chan struct{}, 3)

func (m *Manager) report(st ToolPolicyStatus, cb *InstallCallbacks) {
	m.state.mu.Lock()
	m.state.statuses[st.Name] = st
	m.state.mu.Unlock()
	if cb != nil && cb.OnStatus != nil {
		cb.OnStatus(st)
	}
}

func (m *Manager) applyPolicyStatus(st *ToolStatus) {
	m.state.mu.Lock()
	policy := m.state.statuses[st.Name]
	m.state.mu.Unlock()
	st.Freshness = policy.Freshness
	if st.Freshness == "" {
		st.Freshness = "unchecked"
	}
	st.FreshnessWarning = policy.Warning
	if lk, err := ReadInstalledLock(m.baseDir); err == nil {
		st.OperatorVersion = lk.OperatorVersions[st.Name]
	}
	st.InUse = toolInUse(m.baseDir, st.Name)
}

func toolWithVersion(t ToolDef, version string) (ToolDef, error) {
	if version == "" {
		return t, nil
	}
	if strings.TrimSpace(version) != version || strings.ContainsAny(version, "/\\\r\n\x00") {
		return t, fmt.Errorf("invalid tool version %q", version)
	}
	switch t.Method {
	case MethodGitHubRelease:
		if t.Version != version {
			t.SHA256 = ""
		}
		t.Version = version
	case MethodNuGet, MethodDotnetTool:
		t.DotnetVersion = version
	case MethodDirectURL:
		if t.Version == "" || t.Version != version {
			return t, fmt.Errorf("tool %s uses a fixed direct URL; version %s is unsupported", t.Name, version)
		}
	default:
		return t, fmt.Errorf("tool %s does not support explicit versions", t.Name)
	}
	return t, nil
}

// freshVersion never consults display/release caches. Pin answers do not touch
// an irrelevant latest endpoint. All actual metadata requests have a 15s bound.
func freshVersion(ctx context.Context, t ToolDef) (string, error) {
	if t.Version != "" {
		return t.Version, nil
	}
	if t.DotnetVersion != "" && (t.Method == MethodNuGet || t.Method == MethodDotnetTool) {
		return t.DotnetVersion, nil
	}
	select {
	case freshnessSlots <- struct{}{}:
		defer func() { <-freshnessSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	switch t.Method {
	case MethodGitHubRelease:
		return fetchLatestVersion(ctx, t.Repo)
	case MethodNuGet, MethodDotnetTool:
		return fetchNuGetLatestVersion(ctx, t.DotnetID)
	case MethodDirectURL:
		url := t.URL
		if url == "" && len(t.DownloadURLs) > 0 {
			url = t.DownloadURLs[0]
		}
		if url == "" {
			return "", fmt.Errorf("tool %s has no upstream URL", t.Name)
		}
		resp, err := httpDo(ctx, &http.Client{Timeout: 15 * time.Second}, http.MethodHead, rewriteMirror(url, currentAssetMirror()))
		if err != nil {
			return "", err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("freshness check HTTP %d", resp.StatusCode)
		}
		if lm := resp.Header.Get("Last-Modified"); lm != "" {
			if stamp, err := http.ParseTime(lm); err == nil {
				return stamp.UTC().Format("2006.01.02"), nil
			}
		}
		return "unknown", nil
	default:
		return "", fmt.Errorf("tool %s has no supported freshness source", t.Name)
	}
}

// InstallVersion is an explicit operator action, not automatic selection.
func (m *Manager) InstallVersion(ctx context.Context, name string, opts InstallOptions, cb *InstallCallbacks) (ToolIdentity, error) {
	if m.selection != nil {
		return ToolIdentity{}, fmt.Errorf("explicit tool mutation is forbidden on a bound manager: %w", ErrInUse)
	}
	t, ok := FindByName(name)
	if !ok {
		return ToolIdentity{}, fmt.Errorf("unknown tool: %s", name)
	}
	t, err := toolWithVersion(t, opts.Version)
	if err != nil {
		return ToolIdentity{}, err
	}
	lease, err := acquireToolLock(ctx, m.baseDir, name, true, false)
	if err != nil {
		return ToolIdentity{}, err
	}
	defer func() { _ = lease.Close() }()
	m.report(ToolPolicyStatus{Name: name, Freshness: "checking"}, cb)
	version, err := freshVersion(ctx, t)
	if err == nil {
		t, err = resolvedToolVersion(t, version)
	}
	var id ToolIdentity
	if err == nil {
		id, err = m.installLocked(ctx, t, version, &opts.Version, cb)
	}
	if err != nil {
		m.report(ToolPolicyStatus{Name: name, Freshness: "error", Warning: err.Error()}, cb)
		return ToolIdentity{}, err
	}
	m.report(ToolPolicyStatus{Name: name, Freshness: "updated", OperatorVersion: opts.Version}, cb)
	return id, nil
}

// A direct URL's observed date is metadata, not an operator-selectable URL.
func resolvedToolVersion(t ToolDef, version string) (ToolDef, error) {
	if t.Method == MethodDirectURL {
		t.Version = version
		return t, nil
	}
	return toolWithVersion(t, version)
}

func (m *Manager) installLocked(ctx context.Context, t ToolDef, version string, pin *string, cb *InstallCallbacks) (ToolIdentity, error) {
	// Legacy build methods execute the SDK even during installation. Protect
	// that payload just as recipe execution does; never bypass a runtime lease.
	if t.Method == MethodDotnetTool || t.Method == MethodGitBuild {
		sdk, err := acquireToolLock(ctx, m.baseDir, "runtime:"+string(RuntimeDotnet), false, true)
		if err != nil {
			return ToolIdentity{}, err
		}
		defer func() { _ = sdk.Close() }()
	}
	stage, err := os.MkdirTemp(m.baseDir, "."+t.Name+".staging-*")
	if err != nil {
		return ToolIdentity{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	got, err := m.installInto(ctx, t, stage, cb)
	if err != nil {
		return ToolIdentity{}, err
	}
	if err = ctx.Err(); err != nil {
		return ToolIdentity{}, err
	}
	if err = validateBinaryInstalled(t, stage); err != nil {
		return ToolIdentity{}, err
	}
	if version != "" {
		got = version
	}
	got = observedVersion(got)
	if err = os.WriteFile(filepath.Join(stage, ".version"), []byte(got), 0644); err != nil {
		return ToolIdentity{}, err
	}
	id, err := identityAt(ctx, t, stage, got, true)
	if err != nil {
		return ToolIdentity{}, err
	}
	dest := filepath.Join(m.baseDir, t.Name)
	backup, err := os.MkdirTemp(m.baseDir, "."+t.Name+".old-*")
	if err != nil {
		return ToolIdentity{}, err
	}
	if err = os.Remove(backup); err != nil {
		return ToolIdentity{}, err
	}
	hadOld := false
	if _, err = os.Stat(dest); err == nil {
		if err = os.Rename(dest, backup); err != nil {
			return ToolIdentity{}, err
		}
		hadOld = true
	} else if !os.IsNotExist(err) {
		return ToolIdentity{}, err
	}
	rollback := func(cause error) (ToolIdentity, error) {
		if _, err := os.Stat(dest); err == nil {
			if err = os.Rename(dest, stage); err != nil {
				return ToolIdentity{}, fmt.Errorf("%w; rollback new install failed: %w (old kept at %s)", cause, err, backup)
			}
		}
		if hadOld {
			if err := os.Rename(backup, dest); err != nil {
				return ToolIdentity{}, fmt.Errorf("%w; restore old failed: %w (old kept at %s)", cause, err, backup)
			}
		}
		return ToolIdentity{}, cause
	}
	if err = os.Rename(stage, dest); err != nil {
		if hadOld {
			if restore := os.Rename(backup, dest); restore != nil {
				return ToolIdentity{}, fmt.Errorf("activate: %w; restore: %w", err, restore)
			}
		}
		return ToolIdentity{}, err
	}
	if err = m.recordIdentity(ctx, id, pin); err != nil {
		return rollback(fmt.Errorf("record installed tool: %w", err))
	}
	if hadOld {
		_ = os.RemoveAll(backup)
	}
	return id, nil
}

func (m *Manager) deleteLocked(name string) error {
	state, err := acquireToolLock(context.Background(), m.baseDir, "installed-state", true, true)
	if err != nil {
		return err
	}
	defer func() { _ = state.Close() }()
	lk, err := ReadInstalledLock(m.baseDir)
	if err != nil {
		return err
	}
	// Keep the payload until the state write is known to be possible.
	old := lk
	lk.Tools = cloneIdentities(lk.Tools)
	lk.OperatorVersions = cloneVersions(lk.OperatorVersions)
	delete(lk.Tools, name)
	delete(lk.OperatorVersions, name)
	if err = writeInstalledLock(m.baseDir, lk); err != nil {
		return err
	}
	if err = os.RemoveAll(filepath.Join(m.baseDir, name)); err != nil {
		if restore := writeInstalledLock(m.baseDir, old); restore != nil {
			return fmt.Errorf("delete: %w; restore lock: %w", err, restore)
		}
		return err
	}
	return nil
}
func cloneIdentities(in map[string]ToolIdentity) map[string]ToolIdentity {
	out := map[string]ToolIdentity{}
	maps.Copy(out, in)
	return out
}
func cloneVersions(in map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, in)
	return out
}
