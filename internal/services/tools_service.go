package services

import (
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

// APICommand represents a command pushed by the HTTP API for the frontend to execute.
type APICommand struct {
	Action string `json:"action"` // "install", "install-all", "delete", "run"
	Tool   string `json:"tool"`
	Path   string `json:"path,omitempty"`   // for "run" action
	Output string `json:"output,omitempty"` // for "run" action
}

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
	apiQueue   chan APICommand

	mu        sync.RWMutex
	activeOps map[string]*OpState
	changeCh  chan struct{}
}

// NewToolsService creates a ToolsService.
func NewToolsService(appVersion string) *ToolsService {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.BaseDir(), cfg)
	svc := &ToolsService{
		manager:   mgr,
		appVersion: appVersion,
		apiQueue:  make(chan APICommand, 32),
		activeOps: make(map[string]*OpState),
		changeCh:  make(chan struct{}),
	}
	mgr.OnExtract = func(tool string) {
		svc.updateOp(tool, "extracting", 100)
		if app := application.Get(); app != nil {
			app.Event.Emit("tool:extract:start", map[string]interface{}{
				"tool": tool,
			})
		}
	}
	mgr.OnProgress = func(tool string, bytesDown, bytesTotal int64) {
		pct := 0
		if bytesTotal > 0 {
			pct = int(bytesDown * 100 / bytesTotal)
		}
		svc.updateOp(tool, "installing", pct)
		if app := application.Get(); app != nil {
			app.Event.Emit("tool:download:progress", map[string]interface{}{
				"tool":  tool,
				"bytes": bytesDown,
				"total": bytesTotal,
			})
		}
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

// PushAPICommand enqueues a command from the HTTP API for the frontend to pick up.
func (s *ToolsService) PushAPICommand(cmd APICommand) {
	select {
	case s.apiQueue <- cmd:
	default: // drop if queue is full
	}
}

// PollAPICommand returns the next pending API command, or nil if the queue is empty.
// The frontend calls this on a timer to receive commands from the HTTP API.
func (s *ToolsService) PollAPICommand() *APICommand {
	select {
	case cmd := <-s.apiQueue:
		return &cmd
	default:
		return nil
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
func (s *ToolsService) CheckLatestVersion(name string) map[string]interface{} {
	latestVersion, updateAvailable := s.manager.CheckLatestVersionSingle(name)
	return map[string]interface{}{
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

// Install downloads and installs a single tool by name.
func (s *ToolsService) Install(name string) error {
	if err := s.ensureRuntimeDeps(name); err != nil {
		return err
	}
	s.updateOp(name, "installing", 0)
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": name})
	}
	version, err := s.manager.Install(name)
	s.removeOp(name)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": name, "error": err.Error()})
		} else {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": name, "version": version, "error": nil})
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
			if err := s.Install(st.Name); err != nil {
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
	err := s.manager.InstallRuntime(rk)
	s.removeOp(opName)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": opName, "error": err.Error()})
		} else {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": opName, "error": nil})
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
func (s *ToolsService) StartupAutoUpdate() map[string]interface{} {
	result := map[string]interface{}{
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

	emit := func(event string, data interface{}) {
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
		emit("app:update:available", map[string]interface{}{
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
		emit("tools:updates:available", map[string]interface{}{
			"count": len(updatable),
		})
	}

	// --- Auto-apply app update ---
	if cfg.AutoUpdateApp && appUpdateAvailable {
		emit("startup:progress", map[string]interface{}{
			"phase": "app-update",
			"label": "Updating app to " + newVersion + "...",
		})
		if err := selfupdate.Update(s.appVersion); err != nil {
			log.Printf("startup: app auto-update failed: %v", err)
		} else {
			result["autoApplied"] = true
			emit("app:update:complete", map[string]interface{}{
				"version": newVersion,
			})
		}
	}

	// --- Auto-apply tool updates ---
	if cfg.AutoUpdateTools && len(updatable) > 0 {
		for i, st := range updatable {
			emit("startup:progress", map[string]interface{}{
				"phase": "tool-update",
				"label": st.Name,
				"index": i + 1,
				"total": len(updatable),
			})
			if err := s.Install(st.Name); err != nil {
				log.Printf("startup: auto-update tool %s failed: %v", st.Name, err)
			} else {
				result["autoApplied"] = true
			}
		}
	}

	return result
}
