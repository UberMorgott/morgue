package services

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/selfupdate"
	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// APICommand represents a command pushed by the HTTP API for the frontend to execute.
type APICommand struct {
	Action string `json:"action"` // "install", "install-all", "delete"
	Tool   string `json:"tool"`
}

// ToolsService exposes tool management to the frontend.
type ToolsService struct {
	manager    *tools.Manager
	appVersion string
	apiQueue   chan APICommand
}

// NewToolsService creates a ToolsService.
func NewToolsService(appVersion string) *ToolsService {
	cfg, _ := config.Load(util.ConfigPath())
	mgr := tools.NewManager(util.BaseDir(), cfg)
	mgr.OnProgress = func(tool string, bytesDown, bytesTotal int64) {
		if app := application.Get(); app != nil {
			app.Event.Emit("tool:download:progress", map[string]interface{}{
				"tool":  tool,
				"bytes": bytesDown,
				"total": bytesTotal,
			})
		}
	}
	return &ToolsService{manager: mgr, appVersion: appVersion, apiQueue: make(chan APICommand, 32)}
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
	return s.manager.Delete(name)
}

// Install downloads and installs a single tool by name.
func (s *ToolsService) Install(name string) error {
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": name})
	}
	version, err := s.manager.Install(name)
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
	for _, st := range statuses {
		if !st.Installed {
			if _, err := s.manager.Install(st.Name); err != nil {
				return err
			}
			if app := application.Get(); app != nil {
				app.Event.Emit("tool:installed", st.Name)
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
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": string(rk) + "-runtime"})
	}
	err := s.manager.InstallRuntime(rk)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": string(rk) + "-runtime", "error": err.Error()})
		} else {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": string(rk) + "-runtime", "error": nil})
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
			if _, err := s.manager.Install(st.Name); err != nil {
				log.Printf("startup: auto-update tool %s failed: %v", st.Name, err)
			} else {
				emit("tool:installed", st.Name)
				result["autoApplied"] = true
			}
		}
	}

	return result
}
