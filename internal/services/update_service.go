package services

import (
	"log"
	"os"
	"os/exec"

	"github.com/wailsapp/wails/v3/pkg/application"

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

// Apply downloads and installs the latest version, emitting `update:progress`
// Wails events throughout so the GUI can render a progress bar. On success it
// auto-relaunches the freshly-installed binary and quits the current process.
func (s *UpdateService) Apply() error {
	app := application.Get()

	onProgress := func(p selfupdate.Progress) {
		if app != nil {
			app.Event.Emit("update:progress", p)
		}
	}

	if err := selfupdate.Update(s.Version, onProgress); err != nil {
		// selfupdate.Update already emitted a PhaseError event with details.
		return err
	}

	// Only auto-relaunch in GUI/app context. A headless `morgue self-update`
	// CLI run goes through main.go (app == nil there) and just prints the
	// restart message — never force-relaunch a headless process.
	if app != nil {
		relaunch(app)
	}
	return nil
}

// relaunch spawns the freshly-installed executable detached and quits the
// current process. Guards against relaunch loops via the MORGUE_RELAUNCHED env
// marker so a crash-on-start of the new binary can't fork-bomb.
func relaunch(app *application.App) {
	if os.Getenv("MORGUE_RELAUNCHED") == "1" {
		log.Printf("relaunch: skipped (already relaunched once this chain)")
		app.Quit()
		return
	}

	exe, err := os.Executable()
	if err != nil {
		log.Printf("relaunch: cannot resolve executable: %v", err)
		app.Quit()
		return
	}

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "MORGUE_RELAUNCHED=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("relaunch: failed to start new binary: %v", err)
		// Don't quit — leave the (already-updated-on-disk) current process
		// running so the user isn't left with nothing.
		return
	}
	log.Printf("relaunch: started %s (pid %d), quitting current process", exe, cmd.Process.Pid)
	app.Quit()
}
