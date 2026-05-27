package engine

import (
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

// Options configures a pipeline run.
type Options struct {
	Input   string
	Output  string
	Recipe  string   // force a specific recipe name
	NoSkip  bool     // disable skip-list
	Exclude []string // additional exclude patterns
}
