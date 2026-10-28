package services

import (
	"context"
	"errors"
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
	// Error is the terminal run error (phase error/cancelled), as plain text.
	Error string `json:"error,omitempty"`
}

// PipelineService wraps the engine for Wails RPC.
type PipelineService struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	status PipelineStatus
	pause  *engine.PauseGate
	runner RunFunc
	// onFinish receives the terminal error of runs started via Start.
	onFinish func(error)
}

// NewPipelineService creates a PipelineService.
func NewPipelineService() *PipelineService {
	return &PipelineService{}
}

// ErrBusy is returned by Start when a run is already active.
var ErrBusy = errors.New("busy")

// ErrNotRunning is returned by StopActive when no run is active.
var ErrNotRunning = errors.New("not running")

// RunFunc executes one pipeline run. It is the seam tests use to replace the
// real engine with a controllable stub.
type RunFunc func(ctx context.Context, pause *engine.PauseGate, input, output string) error

// NewPipelineServiceWithRunner creates a PipelineService whose runs execute fn
// instead of the engine. Intended for tests of admission/stop behaviour.
func NewPipelineServiceWithRunner(fn RunFunc) *PipelineService {
	return &PipelineService{runner: fn}
}

// reserve atomically claims the single run slot. It returns ErrBusy if a run
// is already active. On success the status is Running and the returned context
// is cancelled by Stop.
func (s *PipelineService) reserve() (context.Context, *engine.PauseGate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		return nil, nil, ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.pause = engine.NewPauseGate()
	s.status = PipelineStatus{Running: true, Phase: "starting"}
	return ctx, s.pause, nil
}

// Start reserves the run slot synchronously and executes the run in a
// background goroutine. It returns ErrBusy if a run is already active, so
// concurrent callers get exactly one admission. The hook registered with
// SetRunFinishedHook (if any) receives the run's terminal error.
func (s *PipelineService) Start(input, output string) error {
	ctx, pause, err := s.reserve()
	if err != nil {
		return err
	}
	s.mu.Lock()
	done := s.onFinish
	s.mu.Unlock()
	go func() {
		runErr := s.execute(ctx, pause, input, output)
		if done != nil {
			done(runErr)
		}
	}()
	return nil
}

// SetRunFinishedHook registers fn to receive the terminal error of runs
// started with Start. It is a package function rather than a method so the
// Wails binding generator does not try to expose a func-typed parameter.
func SetRunFinishedHook(s *PipelineService, fn func(error)) {
	s.mu.Lock()
	s.onFinish = fn
	s.mu.Unlock()
}

// Run starts the decompilation pipeline on input, writing results to output.
// It blocks until the run finishes. If a run is already active it returns nil
// without doing anything (Wails binding contract; HTTP callers use Start).
func (s *PipelineService) Run(input, output string) error {
	ctx, pause, err := s.reserve()
	if err != nil {
		return nil //nolint:nilerr // busy is a no-op for the GUI binding
	}
	return s.execute(ctx, pause, input, output)
}

// StopActive cancels the active run, returning ErrNotRunning if there is none.
func (s *PipelineService) StopActive() error {
	s.mu.Lock()
	running := s.status.Running
	s.mu.Unlock()
	if !running {
		return ErrNotRunning
	}
	return s.Stop()
}

// execute runs a reserved run to completion and records its terminal status.
func (s *PipelineService) execute(ctx context.Context, pause *engine.PauseGate, input, output string) error {
	run := s.runEngine
	if s.runner != nil {
		run = s.runner
	}
	err := run(ctx, pause, input, output)
	// A cancelled run whose engine swallowed the cancellation still counts as
	// cancelled, so API clients can tell it apart from a clean finish.
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	s.finishStatus(err)
	return err
}

func (s *PipelineService) runEngine(ctx context.Context, pauseGate *engine.PauseGate, input, output string) error {
	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return err
	}

	if output == "" {
		output = cfg.DefaultOutputDir
	}
	if output == "" {
		// GUI/API implicit default: most-free disk (avoids C:/E:). An explicit
		// output or configured DefaultOutputDir still wins above.
		output = util.AutoOutputRoot()
	}
	_ = os.MkdirAll(output, 0755)

	eng := engine.New(cfg, util.ToolsBaseDir())
	events := make(chan engine.PipelineEvent, 100)

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
		Input:        input,
		Output:       output,
		Pause:        pauseGate,
		AllowDynamic: cfg.AllowDynamicExecution,
		// cflow is OFF by default in the GUI (experimental, near no-op on typical
		// targets). No config knob for v1; the CLI --cflow flag is the opt-in source.
		Cflow: false,
	}

	err = eng.Run(ctx, opts, events)
	// Close the channel so the drainer exits (otherwise it leaks), then wait
	// for it to finish so no in-flight event can clobber the terminal status.
	close(events)
	<-drained
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

// finishStatus records the terminal state of a completed run so external
// consumers (HTTP /api/run/status, SSE clients, CLI) see a clear, non-running
// terminal phase instead of a stale "done" left over from the last drained
// event. Called only after the event drainer has fully exited, so this is the
// last write to s.status for the run and cannot be clobbered.
func (s *PipelineService) finishStatus(runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	phase := "done"
	switch {
	case errors.Is(runErr, context.Canceled):
		phase = "cancelled"
		s.status.Error = runErr.Error()
	case runErr != nil:
		phase = "error"
		s.status.Error = runErr.Error()
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
