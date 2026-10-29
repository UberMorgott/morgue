package services

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/engine"
	_ "github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/runmodel"
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
	// RunID identifies the admitted run (events v2 envelope "run").
	RunID string `json:"runId,omitempty"`
	// StartedAt is the backend-owned run start; clients derive elapsed from it.
	StartedAt *time.Time `json:"startedAt,omitempty"`
	// LastSeq is the highest events v2 sequence delivered for RunID.
	LastSeq uint64 `json:"lastSeq"`
	// Outcome is the terminal run outcome (success/error/cancelled), empty while running.
	Outcome string `json:"outcome,omitempty"`
}

// AdmittedRun is a prepared run awaiting execution (implemented by *engine.Run).
type AdmittedRun interface {
	Handle() runmodel.RunHandle
	Execute(ctx context.Context, pause *engine.PauseGate, events chan<- runmodel.Event) (runmodel.RunResult, error)
	Close() error
}

// PrepareFunc admits one run: resolves defaults, validates and reserves output.
// It is the seam tests use to replace the real engine with a controllable stub.
type PrepareFunc func(ctx context.Context, req runmodel.RunRequest) (AdmittedRun, error)

// PipelineService wraps the engine for Wails RPC and the HTTP API.
type PipelineService struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	status  PipelineStatus
	pause   *engine.PauseGate
	prepare PrepareFunc
	// onFinish receives the terminal error of runs started via Start/StartSpec.
	onFinish func(error)
	// onEvent receives every events v2 record in delivery order, fanned out
	// to each registered hook (SSE transport, workspace tracking).
	onEvent []func(runmodel.Event)
}

// NewPipelineService creates a PipelineService backed by the engine.
func NewPipelineService() *PipelineService {
	return &PipelineService{}
}

// NewPipelineServiceWithPrepare creates a PipelineService whose runs are
// admitted by fn instead of the engine. Intended for admission/stop tests.
func NewPipelineServiceWithPrepare(fn PrepareFunc) *PipelineService {
	return &PipelineService{prepare: fn}
}

// ErrBusy is returned by Start when a run is already active.
var ErrBusy = errors.New("busy")

// ErrNotRunning is returned by StopActive when no run is active.
var ErrNotRunning = errors.New("not running")

// eventBuffer bounds in-flight v2 notifications. The engine never blocks on
// delivery (a full buffer drops a notification); RunResult stays authoritative.
const eventBuffer = 4096

// enginePrepare loads config once per run and lets the engine resolve the spec.
func enginePrepare(ctx context.Context, req runmodel.RunRequest) (AdmittedRun, error) {
	cfg, err := config.Load(util.ConfigPath())
	if err != nil {
		return nil, err
	}
	run, err := engine.New(cfg, util.ToolsBaseDir()).PrepareRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// admit atomically claims the single run slot and prepares the run. The slot
// is released (status restored) if preparation fails, so a rejected request
// never leaves the service busy.
func (s *PipelineService) admit(req runmodel.RunRequest) (context.Context, AdmittedRun, *engine.PauseGate, error) {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return nil, nil, nil, ErrBusy
	}
	prev := s.status
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.pause = engine.NewPauseGate()
	s.status = PipelineStatus{Running: true, Phase: "starting"}
	pause := s.pause
	prepare := s.prepare
	s.mu.Unlock()

	if prepare == nil {
		prepare = enginePrepare
	}
	run, err := prepare(ctx, req)
	if err != nil {
		cancel()
		s.mu.Lock()
		s.status, s.cancel, s.pause = prev, nil, nil
		s.mu.Unlock()
		return nil, nil, nil, err
	}
	h := run.Handle()
	started := h.StartedAt
	s.mu.Lock()
	s.status.RunID = h.ID
	s.status.StartedAt = &started
	s.mu.Unlock()
	return ctx, run, pause, nil
}

// StartSpec reserves the run slot, prepares the run synchronously (so the
// caller learns its handle or a validation/output error), then executes it in
// a background goroutine. Returns ErrBusy if a run is already active. The
// hook registered with SetRunFinishedHook receives the run's terminal error.
func (s *PipelineService) StartSpec(req runmodel.RunRequest) (runmodel.RunHandle, error) {
	ctx, run, pause, err := s.admit(req)
	if err != nil {
		return runmodel.RunHandle{}, err
	}
	s.mu.Lock()
	done := s.onFinish
	s.mu.Unlock()
	go func() {
		runErr := s.execute(ctx, run, pause)
		if done != nil {
			done(runErr)
		}
	}()
	return run.Handle(), nil
}

// RunSpec admits req and blocks until the run finishes, returning its handle
// and terminal error. Returns ErrBusy if a run is already active.
func (s *PipelineService) RunSpec(req runmodel.RunRequest) (runmodel.RunHandle, error) {
	ctx, run, pause, err := s.admit(req)
	if err != nil {
		return runmodel.RunHandle{}, err
	}
	return run.Handle(), s.execute(ctx, run, pause)
}

// Start is the migration wrapper for StartSpec with only input/output.
//
// Deprecated: use StartSpec.
func (s *PipelineService) Start(input, output string) error {
	_, err := s.StartSpec(runmodel.RunRequest{Path: input, Output: output})
	return err
}

// SetRunFinishedHook registers fn to receive the terminal error of runs
// started with Start. It is a package function rather than a method so the
// Wails binding generator does not try to expose a func-typed parameter.
func SetRunFinishedHook(s *PipelineService, fn func(error)) {
	s.mu.Lock()
	s.onFinish = fn
	s.mu.Unlock()
}

// AddEventHook registers fn to receive every events v2 record of every run in
// delivery order, alongside previously added hooks (HTTP SSE transport,
// WorkspaceService current-run tracking). Hooks run in registration order on
// the delivery goroutine; fn must not block. Nil fn is ignored.
func AddEventHook(s *PipelineService, fn func(runmodel.Event)) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	s.onEvent = append(s.onEvent, fn)
	s.mu.Unlock()
}

// Run starts the decompilation pipeline on input, writing results to output.
// It blocks until the run finishes. If a run is already active it returns nil
// without doing anything (Wails binding contract; HTTP callers use Start).
//
// Deprecated: use StartSpec.
func (s *PipelineService) Run(input, output string) error {
	_, err := s.RunSpec(runmodel.RunRequest{Path: input, Output: output})
	if errors.Is(err, ErrBusy) {
		return nil // busy is a no-op for the GUI binding
	}
	return err
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

// execute runs an admitted run to completion, forwarding its events, and
// records its terminal status. It returns the run's terminal error.
func (s *PipelineService) execute(ctx context.Context, run AdmittedRun, pause *engine.PauseGate) error {
	events := make(chan runmodel.Event, eventBuffer)
	// drained is closed once every event (including run.finished) has been
	// consumed, so a late event cannot overwrite the terminal status.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("pipeline event drainer panic: %v", r)
			}
		}()
		for ev := range events {
			s.forward(ev)
		}
	}()

	result, err := run.Execute(ctx, pause, events)
	close(events)
	<-drained
	if closeErr := run.Close(); closeErr != nil {
		log.Printf("pipeline: release run: %v", closeErr)
	}
	// A cancelled run whose engine swallowed the cancellation still counts as
	// cancelled, so API clients can tell it apart from a clean finish.
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && result.Outcome != "" && result.Outcome != runmodel.OutcomeSuccess {
		msg := result.Error
		if msg == "" {
			msg = "run finished with outcome " + result.Outcome
		}
		err = errors.New(msg)
	}
	s.finishStatus(result, err)
	return err
}

// forward folds one v2 event into the status and delivers it to the GUI
// (pipeline:event:v2 plus the legacy pipeline:progress adapter) and SSE hook.
func (s *PipelineService) forward(ev runmodel.Event) {
	s.mu.Lock()
	if ev.Run == s.status.RunID && ev.Seq > s.status.LastSeq {
		s.status.LastSeq = ev.Seq
	}
	applyEvent(&s.status, ev)
	hooks := s.onEvent
	s.mu.Unlock()

	for _, hook := range hooks {
		hook(ev)
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(runmodel.EventName, ev)
		// During v0.8 the legacy stream is derived from v2 by one adapter.
		app.Event.Emit("pipeline:progress", engine.LegacyEvent(ev))
	}
}

// applyEvent projects typed event fields into the coarse status; it never
// parses human-readable messages.
func applyEvent(st *PipelineStatus, ev runmodel.Event) {
	switch ev.Type {
	case runmodel.EventRunStarted:
		st.Phase = "scan"
	case runmodel.EventScanCompleted:
		var p runmodel.ScanCompleted
		if json.Unmarshal(ev.Data, &p) == nil {
			st.Phase = "analysis"
			st.FilesTotal = int(p.FilesTotal)
		}
	case runmodel.EventTargetDiscovered:
		var p runmodel.TargetDiscovered
		if json.Unmarshal(ev.Data, &p) == nil {
			st.Target = p.InputPath
		}
	case runmodel.EventToolProgress:
		st.Phase = "tools"
	case runmodel.EventStepProgress:
		var p runmodel.StepProgress
		if json.Unmarshal(ev.Data, &p) == nil {
			st.Phase = "execute"
			st.StepIndex = int(p.StepIndex)
			st.StepTotal = int(p.StepTotal)
			st.StepName = p.Name
		}
	case runmodel.EventTargetFinished:
		if ev.ParentTargetID == "" {
			st.FilesProcessed++
		}
	}
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
	st := s.status
	if st.StartedAt != nil {
		t := *st.StartedAt
		st.StartedAt = &t
	}
	return st
}

// finishStatus records the terminal state of a completed run so external
// consumers (HTTP /api/run/status, SSE clients, CLI) see a clear, non-running
// terminal phase. Called only after the event drainer has fully exited, so this
// is the last write to s.status for the run and cannot be clobbered.
func (s *PipelineService) finishStatus(result runmodel.RunResult, runErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	phase := "done"
	outcome := runmodel.OutcomeSuccess
	switch {
	case errors.Is(runErr, context.Canceled) || result.Outcome == runmodel.OutcomeCancelled:
		phase, outcome = "cancelled", runmodel.OutcomeCancelled
	case runErr != nil:
		phase, outcome = "error", runmodel.OutcomeError
	}
	if runErr != nil {
		s.status.Error = runErr.Error()
	}
	s.status.Running = false
	s.status.Paused = false
	s.status.Phase = phase
	s.status.Outcome = outcome
	// On success, mark the stepper fully complete; on error leave StepIndex
	// where it failed.
	if runErr == nil && s.status.StepTotal > 0 {
		s.status.StepIndex = s.status.StepTotal
	}
	s.cancel = nil
	s.pause = nil
}
