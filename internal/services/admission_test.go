package services

import (
	"context"
	"errors"
	"testing"

	"github.com/UberMorgott/morgue/internal/engine"
)

func TestPipelineStartBusy(t *testing.T) {
	release := make(chan struct{})
	s := NewPipelineServiceWithRunner(func(ctx context.Context, _ *engine.PauseGate, _, _ string) error {
		<-release
		return nil
	})
	done := make(chan error, 1)
	if err := s.Start("a", "", func(err error) { done <- err }); err != nil {
		t.Fatalf("first start: %v", err)
	}
	if err := s.Start("b", "", nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("second start: %v, want ErrBusy", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	if st := s.GetStatus(); st.Running || st.Phase != "done" {
		t.Fatalf("status %+v", st)
	}
}

func TestToolsStartInstallBusy(t *testing.T) {
	s := &ToolsService{installing: map[string]struct{}{}}
	// Simulate in-flight admissions without running real installs.
	s.installing["ilspycmd"] = struct{}{}
	if err := s.StartInstall("ilspycmd"); !errors.Is(err, ErrBusy) {
		t.Fatalf("same tool: %v, want ErrBusy", err)
	}
	if err := s.StartInstall(""); !errors.Is(err, ErrBusy) {
		t.Fatalf("install-all while one tool busy: %v, want ErrBusy", err)
	}
	delete(s.installing, "ilspycmd")
	s.installing[allToolsKey] = struct{}{}
	if err := s.StartInstall("ghidra"); !errors.Is(err, ErrBusy) {
		t.Fatalf("tool while install-all busy: %v, want ErrBusy", err)
	}
}
