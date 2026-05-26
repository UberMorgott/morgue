package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/scanner"
)

// ScanTarget is a frontend-friendly representation of a scan result.
type ScanTarget struct {
	Path  string `json:"path"`
	Group string `json:"group"`
}

// ReconService exposes binary reconnaissance to the frontend.
type ReconService struct{}

// ScanDirectory walks a directory and returns all binary targets found.
func (s *ReconService) ScanDirectory(dir string) ([]ScanTarget, error) {
	result, err := scanner.Scan(dir)
	if err != nil {
		return nil, err
	}

	var targets []ScanTarget
	for _, g := range result.Groups {
		for _, f := range g.Files {
			targets = append(targets, ScanTarget{
				Path:  f,
				Group: g.Kind.String(),
			})
		}
	}
	return targets, nil
}

// ClassifyFile performs recon on a single file.
func (s *ReconService) ClassifyFile(path string) (*recon.Result, error) {
	r, err := recon.Classify(path)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// PickFile opens a native file picker dialog.
func (s *ReconService) PickFile() (string, error) {
	app := application.Get()
	if app == nil {
		return "", nil
	}
	return app.Dialog.OpenFile().
		CanChooseDirectories(false).
		CanChooseFiles(true).
		SetTitle("Select Binary File").
		PromptForSingleSelection()
}

// PickDirectory opens a native directory picker dialog.
func (s *ReconService) PickDirectory() (string, error) {
	app := application.Get()
	if app == nil {
		return "", nil
	}
	return app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		SetTitle("Select Target Directory").
		PromptForSingleSelection()
}
