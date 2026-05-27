package engine

import (
	"context"

	"github.com/UberMorgott/morgue/internal/config"
	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/scanner"
	"github.com/UberMorgott/morgue/internal/skiplist"
	"github.com/UberMorgott/morgue/internal/tools"
)

// Engine orchestrates the decompilation pipeline.
type Engine struct {
	cfg      config.Config
	tools    *tools.Manager
	skipList *skiplist.SkipList
}

// New creates a new Engine with the given config and tool base directory.
func New(cfg config.Config, toolBaseDir string) *Engine {
	return &Engine{
		cfg:      cfg,
		tools:    tools.NewManager(toolBaseDir, cfg),
		skipList: skiplist.New(cfg),
	}
}

// Scan walks the input directory and returns grouped targets.
func (e *Engine) Scan(input string) (scanner.ScanResult, error) {
	return scanner.Scan(input)
}

// Classify performs recon on a single file.
func (e *Engine) Classify(ctx context.Context, path string) (recon.Result, error) {
	return recon.Classify(ctx, path)
}

// MatchRecipe finds the best recipe for a recon result, or uses a forced recipe name.
func (e *Engine) MatchRecipe(r *recon.Result, forceName string) recipe.Recipe {
	if forceName != "" {
		return recipe.FindByName(forceName)
	}
	return recipe.Match(r)
}

// ShouldSkip checks if a file should be skipped.
func (e *Engine) ShouldSkip(filename string) (bool, string) {
	return e.skipList.ShouldSkip(filename)
}

// ToolsManager returns the underlying tool manager.
func (e *Engine) ToolsManager() *tools.Manager {
	return e.tools
}
