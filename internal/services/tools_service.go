package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/selfupdate"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// OpState tracks a currently active tool operation.
type OpState struct {
	Action       string    `json:"action"`
	Progress     int       `json:"progress"`
	LastActivity time.Time `json:"lastActivity"`
}

// EnrichedToolStatus extends ToolStatus with operation state.
type EnrichedToolStatus struct {
	tools.ToolStatus
	Installing   bool      `json:"installing,omitempty"`
	Progress     int       `json:"progress,omitempty"`
	LastActivity time.Time `json:"lastActivity,omitempty"`
}

// EnrichedToolsResponse is the enriched /api/tools response.
type EnrichedToolsResponse struct {
	Tools   []EnrichedToolStatus `json:"tools"`
	Busy    bool                 `json:"busy"`
	Changed bool                 `json:"changed,omitempty"`
}

// ToolsService exposes tool management to the frontend.
type ToolsService struct {
	manager    *tools.Manager
	appVersion string

	mu        sync.RWMutex
	activeOps map[string]*OpState
	changeCh  chan struct{}

	// installing holds API-admitted install jobs (tool name, or allToolsKey).
	installing map[string]struct{}
}

// ErrInvalidToolRequest rejects a malformed install request before admission.
var ErrInvalidToolRequest = errors.New("invalid tool request")

// allToolsKey is the admission key for an install-all job.
const allToolsKey = "*"

// StartInstall admits an install job synchronously and runs it in the
// background. name == "" installs every missing tool. It returns ErrBusy if
// the same tool, or an install-all job, is already admitted, so concurrent
// callers cannot start duplicate installs of one tool.
func (s *ToolsService) StartInstall(name string) error {
	return s.StartInstallVersion(name, "")
}

// StartInstallVersion is StartInstall with an optional exact version. A
// non-empty version records an operator override (pin) on success; an empty
// version clears an existing override on success. The version requires a name.
func (s *ToolsService) StartInstallVersion(name, version string) error {
	if version != "" && name == "" {
		return fmt.Errorf("%w: version requires a tool name", ErrInvalidToolRequest)
	}
	if name != "" {
		if _, ok := tools.FindByName(name); !ok {
			return fmt.Errorf("%w: unknown tool %q", ErrInvalidToolRequest, name)
		}
		if s.manager != nil && s.manager.Check(name).InUse {
			return tools.ErrInUse
		}
	}
	key := name
	if key == "" {
		key = allToolsKey
	}
	s.mu.Lock()
	_, sameBusy := s.installing[key]
	_, allBusy := s.installing[allToolsKey]
	if sameBusy || allBusy || (key == allToolsKey && len(s.installing) > 0) {
		s.mu.Unlock()
		return ErrBusy
	}
	s.installing[key] = struct{}{}
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.installing, key)
			s.mu.Unlock()
		}()
		var err error
		if key == allToolsKey {
			err = s.InstallAll()
		} else {
			err = s.Install(context.Background(), name)
		}
		if err != nil {
			log.Printf("api: install %s: %v", key, err)
		}
	}()
	return nil
}

// NewToolsService creates a ToolsService.
func NewToolsService(appVersion string) *ToolsService {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.ToolsBaseDir(), cfg)
	svc := &ToolsService{
		manager:    mgr,
		appVersion: appVersion,
		activeOps:  make(map[string]*OpState),
		changeCh:   make(chan struct{}),
		installing: make(map[string]struct{}),
	}
	return svc
}

// broadcastChange signals all WaitForChange callers that state has changed.
func (s *ToolsService) broadcastChange() {
	s.mu.Lock()
	close(s.changeCh)
	s.changeCh = make(chan struct{})
	s.mu.Unlock()
}

// updateOp updates or creates an active operation entry.
func (s *ToolsService) updateOp(tool, action string, progress int) {
	s.mu.Lock()
	s.activeOps[tool] = &OpState{Action: action, Progress: progress, LastActivity: time.Now()}
	s.mu.Unlock()
	s.broadcastChange()
}

// removeOp removes an active operation entry.
func (s *ToolsService) removeOp(tool string) {
	s.mu.Lock()
	delete(s.activeOps, tool)
	s.mu.Unlock()
	s.broadcastChange()
}

// GetToolsEnriched returns all tools with active operation state merged in.
func (s *ToolsService) GetToolsEnriched() EnrichedToolsResponse {
	statuses := s.manager.CheckAll()
	s.mu.RLock()
	defer s.mu.RUnlock()

	enriched := make([]EnrichedToolStatus, len(statuses))
	busy := len(s.activeOps) > 0
	for i, st := range statuses {
		e := EnrichedToolStatus{ToolStatus: st}
		if op, ok := s.activeOps[st.Name]; ok {
			e.Installing = true
			e.Progress = op.Progress
			e.LastActivity = op.LastActivity
		}
		enriched[i] = e
	}
	return EnrichedToolsResponse{Tools: enriched, Busy: busy}
}

// WaitForChange blocks until tool state changes or timeout elapses.
// Returns true if a change occurred, false on timeout.
func (s *ToolsService) WaitForChange(timeout time.Duration) bool {
	s.mu.RLock()
	ch := s.changeCh
	s.mu.RUnlock()

	select {
	case <-ch:
		return true
	case <-time.After(timeout):
		return false
	}
}

// CheckAll returns the installation status of all registered tools.
func (s *ToolsService) CheckAll() []tools.ToolStatus {
	return s.manager.CheckAll()
}

// CheckAllWithUpdates returns tool statuses including latest versions from GitHub.
func (s *ToolsService) CheckAllWithUpdates() []tools.ToolStatus {
	return s.manager.CheckAllWithUpdates()
}

// CheckLatestVersion checks the latest version for a single tool.
// Returns a map with latestVersion and updateAvailable.
func (s *ToolsService) CheckLatestVersion(name string) map[string]any {
	latestVersion, updateAvailable := s.manager.CheckLatestVersionSingle(name)
	return map[string]any{
		"name":            name,
		"latestVersion":   latestVersion,
		"updateAvailable": updateAvailable,
	}
}

// Delete removes a tool from disk.
func (s *ToolsService) Delete(name string) error {
	s.updateOp(name, "deleting", 0)
	err := s.manager.Delete(name)
	s.removeOp(name)
	return err
}

// ensureRuntimeDeps checks and installs any missing runtime dependencies for a tool.
func (s *ToolsService) ensureRuntimeDeps(name string) error {
	tool, ok := tools.FindByName(name)
	if !ok || len(tool.RuntimeDeps) == 0 {
		return nil
	}
	for _, rk := range tool.RuntimeDeps {
		if _, err := s.manager.RuntimePath(rk); err == nil {
			continue // runtime already available
		}
		log.Printf("auto-installing runtime %s required by %s", rk, name)
		if err := s.InstallRuntime(string(rk)); err != nil {
			return fmt.Errorf("install runtime %s for %s: %w", rk, name, err)
		}
	}
	return nil
}

// installCallbacks builds progress callbacks wired to Wails events and active operation tracking.
func (s *ToolsService) installCallbacks() *tools.InstallCallbacks {
	return &tools.InstallCallbacks{
		OnProgress: func(tool string, bytesDown, bytesTotal int64) {
			pct := 0
			if bytesTotal > 0 {
				pct = int(bytesDown * 100 / bytesTotal)
			}
			s.updateOp(tool, "installing", pct)
			if app := application.Get(); app != nil {
				app.Event.Emit("tool:download:progress", map[string]any{
					"tool":  tool,
					"bytes": bytesDown,
					"total": bytesTotal,
				})
			}
		},
		OnExtract: func(tool string) {
			s.updateOp(tool, "extracting", 100)
			if app := application.Get(); app != nil {
				app.Event.Emit("tool:extract:start", map[string]any{
					"tool": tool,
				})
			}
		},
	}
}

// Install downloads and installs a single tool by name. Cancelling ctx aborts
// an in-flight download.
func (s *ToolsService) Install(ctx context.Context, name string) error {
	return s.InstallVersion(ctx, name, "")
}

// InstallVersion installs name through the same policy as CLI and API: an
// exact version records an operator override (shown as pinned, honored by the
// next run's freshness check); an empty version resumes automatic updates.
// A tool leased by an active run is never swapped (tools.ErrInUse). Success is
// reported only after activation and installed-lock recording.
func (s *ToolsService) InstallVersion(ctx context.Context, name, version string) error {
	//nolint:contextcheck // reaches tools.Manager.RuntimePath -> systemDotNetHasAspNet10, a local
	// `dotnet --list-runtimes` probe that already bounds itself to 10s (same note as engine.Run).
	if err := s.ensureRuntimeDeps(name); err != nil {
		return err
	}
	s.updateOp(name, "installing", 0)
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": name})
	}
	id, err := s.manager.InstallVersion(ctx, name, tools.InstallOptions{Version: version}, s.installCallbacks())
	s.removeOp(name)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]any{"tool": name, "error": err.Error(), "inUse": errors.Is(err, tools.ErrInUse)})
		} else {
			app.Event.Emit("tool:download:complete", map[string]any{"tool": name, "version": id.Version, "operatorVersion": version, "error": nil})
		}
		app.Event.Emit("tool:installed", name)
	}
	return err
}

// InstallAll installs every tool that is not yet present.
func (s *ToolsService) InstallAll() error {
	statuses := s.manager.CheckAll()

	// Collect all runtimes needed by tools that aren't installed yet.
	needed := map[tools.RuntimeKind]bool{}
	for _, st := range statuses {
		if !st.Installed {
			for _, rk := range st.RuntimeDeps {
				needed[rk] = true
			}
		}
	}
	// Install missing runtimes first.
	for rk := range needed {
		if _, err := s.manager.RuntimePath(rk); err == nil {
			continue
		}
		log.Printf("auto-installing runtime %s required by pending tools", rk)
		if err := s.InstallRuntime(string(rk)); err != nil {
			return fmt.Errorf("install runtime %s: %w", rk, err)
		}
	}

	for _, st := range statuses {
		if !st.Installed {
			if err := s.Install(context.Background(), st.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// CheckRuntimes returns the status of all runtimes.
func (s *ToolsService) CheckRuntimes() []tools.RuntimeStatus {
	return s.manager.CheckRuntimes()
}

// InstallRuntime downloads and installs a portable runtime.
func (s *ToolsService) InstallRuntime(kind string) error {
	rk := tools.RuntimeKind(kind)
	opName := string(rk) + "-runtime"
	s.updateOp(opName, "installing", 0)
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": opName})
	}
	err := s.manager.InstallRuntime(rk, s.installCallbacks())
	s.removeOp(opName)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]any{"tool": opName, "error": err.Error()})
		} else {
			app.Event.Emit("tool:download:complete", map[string]any{"tool": opName, "error": nil})
		}
	}
	return err
}

// ShouldCheckUpdates returns true if enough time has passed since the last update check.
func (s *ToolsService) ShouldCheckUpdates() bool {
	return s.manager.ShouldCheckUpdates()
}

// MarkUpdateChecked saves the current time as the last update check timestamp.
func (s *ToolsService) MarkUpdateChecked() {
	s.manager.MarkUpdateChecked()
}

// StartupAutoUpdate runs background update checks and auto-applies if configured.
func (s *ToolsService) StartupAutoUpdate() map[string]any {
	result := map[string]any{
		"appUpdate":   false,
		"toolUpdates": 0,
		"autoApplied": false,
	}

	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		log.Printf("startup: failed to load config: %v", err)
		return result
	}

	if !cfg.AutoUpdateCheck {
		return result
	}

	emit := func(event string, data any) {
		if app := application.Get(); app != nil {
			app.Event.Emit(event, data)
		}
	}

	// --- App update check ---
	appStatus := selfupdate.CheckStatus(s.appVersion)
	appUpdateAvailable := false
	newVersion := ""
	if appStatus != "up to date" && appStatus != "offline" && len(appStatus) > 8 {
		newVersion = appStatus[8:]
		appUpdateAvailable = true
		result["appUpdate"] = true
		emit("app:update:available", map[string]any{
			"version": newVersion,
		})
	}

	// --- Tool update check ---
	statuses := s.manager.CheckAllWithUpdates()
	updatable := []tools.ToolStatus{}
	for _, st := range statuses {
		if st.UpdateAvailable && st.Installed {
			updatable = append(updatable, st)
		}
	}
	result["toolUpdates"] = len(updatable)
	if len(updatable) > 0 {
		emit("tools:updates:available", map[string]any{
			"count": len(updatable),
		})
	}

	// --- Auto-apply app update ---
	if cfg.AutoUpdateApp && appUpdateAvailable {
		emit("startup:progress", map[string]any{
			"phase": "app-update",
			"label": "Updating app to " + newVersion + "...",
		})
		onProgress := func(p selfupdate.Progress) {
			emit("update:progress", p)
		}
		if err := selfupdate.Update(s.appVersion, onProgress); err != nil {
			log.Printf("startup: app auto-update failed: %v", err)
		} else {
			result["autoApplied"] = true
			emit("app:update:complete", map[string]any{
				"version": newVersion,
			})
			// Auto-relaunch into the freshly-installed binary (GUI context only;
			// app == nil short-circuits inside relaunch's caller guard).
			if app := application.Get(); app != nil {
				relaunch(app)
			}
		}
	}

	// Tool updates are NOT applied at startup: every run checks freshness of
	// exactly the tools it uses before first use (tools.RunSelection), the same
	// policy for GUI, CLI and API runs. A registry-wide startup update would
	// bypass operator overrides and run leases. The check above is display-only.
	return result
}
