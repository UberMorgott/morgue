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

// PipelineEvent is emitted via Wails events. JSON keys are explicit PascalCase.
type PipelineEvent struct {
	Phase          string              `json:"Phase"`
	Target         string              `json:"Target"`
	Message        string              `json:"Message"`
	Progress       *recipe.StepProgress `json:"Progress,omitempty"`
	Done           bool                `json:"Done"`
	Error          error               `json:"Error,omitempty"`
	FilesTotal     int                 `json:"FilesTotal"`
	FilesProcessed int                 `json:"FilesProcessed"`
	// Enriched fields for frontend
	ReconKind   string   `json:"ReconKind,omitempty"`
	Compiler    string   `json:"Compiler,omitempty"`
	Obfuscator  string   `json:"Obfuscator,omitempty"`
	FileSize    int64    `json:"FileSize,omitempty"`
	RecipeName  string   `json:"RecipeName,omitempty"`
	RecipeDesc  string   `json:"RecipeDesc,omitempty"`
	ToolsNeeded []string `json:"ToolsNeeded,omitempty"`
	OutputPath  string   `json:"OutputPath,omitempty"`
	OutputStats []string `json:"OutputStats,omitempty"`
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
