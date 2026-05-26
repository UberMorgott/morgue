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

// Install downloads and installs a single tool by name.
func (s *ToolsService) Install(name string) error {
	err := s.manager.Install(name)
	if app := application.Get(); app != nil {
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
