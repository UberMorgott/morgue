package services

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/util"
)

// PipelineStatus describes the current state of the pipeline.
type PipelineStatus struct {
	Running        bool   `json:"running"`
	Phase          string `json:"phase"`
	Target         string `json:"target"`
	StepIndex      int    `json:"stepIndex"`
	StepTotal      int    `json:"stepTotal"`
	StepName       string `json:"stepName"`
	FilesProcessed int    `json:"filesProcessed"`
	FilesTotal     int    `json:"filesTotal"`
}

// PipelineService wraps the engine for Wails RPC.
type PipelineService struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	status PipelineStatus
}

// NewPipelineService creates a PipelineService.
func NewPipelineService() *PipelineService {
	return &PipelineService{}
}

// Run starts the decompilation pipeline on input, writing results to output.
func (s *PipelineService) Run(input, output string) error {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.status = PipelineStatus{Running: true, Phase: "starting"}
	s.mu.Unlock()

	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		s.resetStatus()
		return err
	}

	eng := engine.New(cfg, util.BaseDir())
	events := make(chan engine.PipelineEvent, 100)

	go func() {
		for ev := range events {
			s.mu.Lock()
			s.status.Phase = ev.Phase
			s.status.Target = ev.Target
			s.status.FilesTotal = ev.FilesTotal
			s.status.FilesProcessed = ev.FilesProcessed
			if ev.Progress != nil {
				s.status.StepIndex = ev.Progress.Step
				s.status.StepTotal = ev.Progress.Total
				s.status.StepName = ev.Progress.Name
			}
			s.mu.Unlock()

			if app := application.Get(); app != nil {
				app.Event.Emit("pipeline:progress", ev)
			}
		}
	}()

	opts := engine.Options{
		Input:  input,
		Output: output,
	}

	err = eng.Run(ctx, opts, events)
	s.resetStatus()
	return err
}

// Stop cancels the running pipeline.
func (s *PipelineService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
}

// GetStatus returns the current pipeline status.
func (s *PipelineService) GetStatus() PipelineStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *PipelineService) resetStatus() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = PipelineStatus{}
	s.cancel = nil
}
