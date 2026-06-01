package engine

import (
	"context"
	"sync"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/scanner"
)

var _ recipe.PauseChecker = (*PauseGate)(nil)

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
	Tool           string              `json:"Tool,omitempty"`
	Progress       *recipe.StepProgress `json:"Progress,omitempty"`
	Done           bool                `json:"Done"`
	Error          error               `json:"Error,omitempty"`
	// Enriched fields for frontend
	ReconKind   string   `json:"ReconKind,omitempty"`
	Compiler    string   `json:"Compiler,omitempty"`
	Obfuscator   string   `json:"Obfuscator,omitempty"`
	Deobfuscator string   `json:"Deobfuscator,omitempty"`
	FileSize    int64    `json:"FileSize,omitempty"`
	RecipeName  string   `json:"RecipeName,omitempty"`
	RecipeDesc  string   `json:"RecipeDesc,omitempty"`
	ToolsNeeded []string `json:"ToolsNeeded,omitempty"`
	OutputPath     string   `json:"OutputPath,omitempty"`
	OutputStats    []string `json:"OutputStats,omitempty"`
	FilesTotal     int      `json:"FilesTotal,omitempty"`
	FilesProcessed int      `json:"FilesProcessed,omitempty"`
}

// PauseGate allows pausing/resuming the pipeline between steps.
// Uses a channel-based approach: closed channel = running, open channel = paused.
type PauseGate struct {
	mu sync.Mutex
	ch chan struct{} // closed = running, open = paused (blocks on receive)
}

// NewPauseGate creates a new PauseGate in the unpaused state.
func NewPauseGate() *PauseGate {
	ch := make(chan struct{})
	close(ch) // start unpaused — reads proceed immediately
	return &PauseGate{ch: ch}
}

// Pause blocks future WaitIfPaused calls until Resume is called.
func (pg *PauseGate) Pause() {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	// Only pause if currently running (channel is closed).
	select {
	case <-pg.ch:
		// Channel was closed (running) — replace with a new open channel to block waiters.
		pg.ch = make(chan struct{})
	default:
		// Already paused (channel is open/blocking) — nothing to do.
	}
}

// Resume unblocks any goroutine waiting in WaitIfPaused.
func (pg *PauseGate) Resume() {
	pg.mu.Lock()
	defer pg.mu.Unlock()
	// Only resume if currently paused (channel is open).
	select {
	case <-pg.ch:
		// Already running (closed channel) — nothing to do.
	default:
		// Paused — close to unblock all waiters.
		close(pg.ch)
	}
}

// IsPaused returns whether the gate is currently paused.
func (pg *PauseGate) IsPaused() bool {
	pg.mu.Lock()
	ch := pg.ch
	pg.mu.Unlock()
	select {
	case <-ch:
		return false // channel closed = running
	default:
		return true // channel open = paused
	}
}

// WaitIfPaused blocks if paused. Returns ctx.Err() if context is cancelled while waiting.
func (pg *PauseGate) WaitIfPaused(ctx context.Context) error {
	pg.mu.Lock()
	ch := pg.ch
	pg.mu.Unlock()
	select {
	case <-ch:
		return nil // not paused or just resumed
	case <-ctx.Done():
		return ctx.Err()
	}
}

// PipelineSummary wraps results with aggregate stats.
type PipelineSummary struct {
	Stats   SummaryStats   `json:"stats"`
	Results []summaryEntry `json:"results"`
}

// SummaryStats holds aggregate pipeline statistics.
type SummaryStats struct {
	Total    int            `json:"total"`
	Success  int            `json:"success"`
	Failed   int            `json:"failed"`
	Skipped  int            `json:"skipped"`
	Duration string         `json:"duration"`
	ByKind   map[string]int `json:"by_kind"`
	ByRecipe map[string]int `json:"by_recipe"`
}

// Options configures a pipeline run.
type Options struct {
	Input   string
	Output  string
	Recipe  string   // force a specific recipe name
	NoSkip  bool     // disable skip-list
	Exclude []string // additional exclude patterns
	Pause   *PauseGate
	// AllowDynamic opts into recipe steps that execute target code.
	AllowDynamic bool
}
