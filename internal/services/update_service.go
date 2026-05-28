package services

import (
	"github.com/UberMorgott/morgue/internal/selfupdate"
)

// UpdateStatus holds version check results for the frontend.
type UpdateStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Status    string `json:"status"`
}

// UpdateService exposes self-update functionality to the frontend.
type UpdateService struct {
	Version string
	Commit  string
}

// GetVersion returns the current application version.
func (s *UpdateService) GetVersion() string {
	return s.Version
}

// GetCommit returns the current build commit hash.
func (s *UpdateService) GetCommit() string {
	return s.Commit
}

// Check queries GitHub for the latest release.
func (s *UpdateService) Check() UpdateStatus {
	status := selfupdate.CheckStatus(s.Version)
	switch status {
	case "up to date":
		return UpdateStatus{Status: status}
	case "offline":
		return UpdateStatus{Status: status}
	default:
		// "update: vX.Y.Z"
		ver := ""
		if len(status) > 8 {
			ver = status[8:]
		}
		return UpdateStatus{Available: true, Version: ver, Status: status}
	}
}

// Apply downloads and installs the latest version.
func (s *UpdateService) Apply() error {
	return selfupdate.Update(s.Version)
}
