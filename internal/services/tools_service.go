package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/tools"
	"github.com/UberMorgott/morgue/internal/util"
)

// ToolsService exposes tool management to the frontend.
type ToolsService struct {
	manager *tools.Manager
}

// NewToolsService creates a ToolsService.
func NewToolsService() *ToolsService {
	return &ToolsService{
		manager: tools.NewManager(util.BaseDir()),
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

// Delete removes a tool from disk.
func (s *ToolsService) Delete(name string) error {
	return s.manager.Delete(name)
}

// Install downloads and installs a single tool by name.
func (s *ToolsService) Install(name string) error {
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": name})
	}
	err := s.manager.Install(name)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": name, "error": err.Error()})
		} else {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": name, "error": nil})
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
			if err := s.manager.Install(st.Name); err != nil {
				return err
			}
			if app := application.Get(); app != nil {
				app.Event.Emit("tool:installed", st.Name)
			}
		}
	}
	return nil
}
