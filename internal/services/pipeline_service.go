package services

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/util"
)

// PipelineStatus is returned via Wails binding and HTTP API. Uses explicit json tags (camelCase).
type PipelineStatus struct {
	Running        bool   `json:"running"`
	Paused         bool   `json:"paused"`
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
	pause  *engine.PauseGate
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
	s.pause = engine.NewPauseGate()
	s.status = PipelineStatus{Running: true, Phase: "starting"}
	s.mu.Unlock()

	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		s.resetStatus()
		return err
	}

	if output == "" {
		output = cfg.DefaultOutputDir
	}
	if output == "" {
		output = util.DefaultOutputDir()
	}
	os.MkdirAll(output, 0755)

	eng := engine.New(cfg, util.ToolsBaseDir())
	events := make(chan engine.PipelineEvent, 100)

	s.mu.Lock()
	pauseGate := s.pause
	s.mu.Unlock()

	// drained is closed once the drainer goroutine has fully consumed every
	// event (including the engine's final "done" event). We must not reset the
	// status until then, otherwise a late "done" event would overwrite the
	// terminal status and leave it stale (running:false but phase:"done").
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline event drainer panic: %v", r)
			}
		}()
		for ev := range events {
			s.mu.Lock()
			s.status.Phase = ev.Phase
			// Preserve the last non-empty Target. The engine's deferred final
			// "done" event carries an empty Target, which would otherwise blank
			// out the terminal status; only overwrite when the event names one.
			if ev.Target != "" {
				s.status.Target = ev.Target
			}
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
		Pause:  pauseGate,
	}

	err = eng.Run(ctx, opts, events)
	// Close the channel so the drainer exits (otherwise it leaks), then wait
	// for it to finish so no in-flight event can clobber the terminal status.
	close(events)
	<-drained
	s.finishStatus(err)
	return err
}

// Stop cancels the running pipeline.
func (s *PipelineService) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Resume first so the engine doesn't hang on the pause gate
	if s.pause != nil {
		s.pause.Resume()
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
}

// Pause pauses the running pipeline. Tools in progress run to completion.
func (s *PipelineService) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.status.Running || s.pause == nil {
		return nil
	}
	s.pause.Pause()
	s.status.Paused = true
	if app := application.Get(); app != nil {
		app.Event.Emit("pipeline:paused", nil)
	}
	return nil
}

// Resume resumes a paused pipeline.
func (s *PipelineService) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pause == nil || !s.pause.IsPaused() {
		return nil
	}
	s.pause.Resume()
	s.status.Paused = false
	if app := application.Get(); app != nil {
		app.Event.Emit("pipeline:resumed", nil)
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
	s.pause = nil
}

// finishStatus records the terminal state of a completed run so external
// consumers (HTTP /api/run/status, SSE clients, CLI) see a clear, non-running
// terminal phase instead of a stale "done" left over from the last drained
// event. Called only after the event drainer has fully exited, so this is the
// last write to s.status for the run and cannot be clobbered.
func (s *PipelineService) finishStatus(runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	phase := "done"
	if runErr != nil {
		phase = "error"
	}
	// Preserve Target/FilesProcessed/FilesTotal from the final drained event so
	// the terminal status still describes what was processed; just flip the
	// running flags and pin the terminal phase.
	s.status.Running = false
	s.status.Paused = false
	s.status.Phase = phase
	// On success, mark the stepper fully complete (the engine's final "done"
	// event carries no Progress, so the last real event left StepIndex one short
	// of StepTotal). On error, leave StepIndex where it failed.
	if runErr == nil && s.status.StepTotal > 0 {
		s.status.StepIndex = s.status.StepTotal
	}
	s.cancel = nil
	s.pause = nil
}
