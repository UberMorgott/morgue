package engine

import (
	"context"
	"sync"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/scanner"
)

// TargetResult holds the outcome for a single target.
type TargetResult struct {
	Group      scanner.TargetGroup
	Recon      recon.Result
	Recipe     recipe.Recipe
	Output     string
	Error      error
	Skipped    bool
	SkipReason string
}

// PipelineEvent reports progress of the pipeline to the UI.
type PipelineEvent struct {
	Phase          string
	Target         string
	Message        string
	Progress       *recipe.StepProgress
	Done           bool
	Error          error
	FilesTotal     int // total targets found
	FilesProcessed int // targets completed so far
}

// PauseGate allows pausing/resuming the pipeline between steps.
type PauseGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	paused bool
}

// NewPauseGate creates a new PauseGate.
func NewPauseGate() *PauseGate {
	pg := &PauseGate{}
	pg.cond = sync.NewCond(&pg.mu)
	return pg
}

// Pause blocks future WaitIfPaused calls until Resume is called.
func (pg *PauseGate) Pause() {
	pg.mu.Lock()
	pg.paused = true
	pg.mu.Unlock()
}

// Resume unblocks any goroutine waiting in WaitIfPaused.
func (pg *PauseGate) Resume() {
	pg.mu.Lock()
	pg.paused = false
	pg.cond.Broadcast()
	pg.mu.Unlock()
}

// IsPaused returns whether the gate is currently paused.
func (pg *PauseGate) IsPaused() bool {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	return pg.paused
}

// WaitIfPaused blocks if paused. Returns ctx.Err() if context is cancelled while waiting.
func (pg *PauseGate) WaitIfPaused(ctx context.Context) error {
	pg.mu.Lock()
	for pg.paused {
		// Check context before blocking
		select {
		case <-ctx.Done():
			pg.mu.Unlock()
			return ctx.Err()
		default:
		}
		// Wait with periodic context checks
		done := make(chan struct{})
		go func() {
			pg.cond.Wait()
			close(done)
		}()
		pg.mu.Unlock()

		select {
		case <-done:
			pg.mu.Lock()
			continue
		case <-ctx.Done():
			// Wake up the waiting goroutine so it doesn't leak
			pg.cond.Broadcast()
			<-done
			return ctx.Err()
		}
	}
	pg.mu.Unlock()
	return nil
}

// Options configures a pipeline run.
type Options struct {
	Input   string
	Output  string
	Recipe  string   // force a specific recipe name
	NoSkip  bool     // disable skip-list
	Exclude []string // additional exclude patterns
	Pause   *PauseGate
}
