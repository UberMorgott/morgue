package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/scanner"
)

// Run executes the full pipeline: scan → recon → skip → match → execute → save.
func (e *Engine) Run(ctx context.Context, opts Options, events chan<- PipelineEvent) error {
	defer func() {
		if events != nil {
			events <- PipelineEvent{Phase: "done", Done: true}
		}
	}()

	filesTotal := 0
	filesProcessed := 0

	emit := func(phase, target, msg string) {
		if events != nil {
			events <- PipelineEvent{Phase: phase, Target: target, Message: msg, FilesTotal: filesTotal, FilesProcessed: filesProcessed}
		}
	}
	emitErr := func(phase, target string, err error) {
		if events != nil {
			events <- PipelineEvent{Phase: phase, Target: target, Error: err, FilesTotal: filesTotal, FilesProcessed: filesProcessed}
		}
	}

	// Phase 1: Scan
	emit("scan", opts.Input, "Scanning for binaries...")
	scanResult, err := e.Scan(opts.Input)
	if err != nil {
		emitErr("scan", opts.Input, err)
		return fmt.Errorf("scan: %w", err)
	}
	// Count total files across all groups.
	for _, g := range scanResult.Groups {
		filesTotal += len(g.Files)
	}

	emit("scan", opts.Input, fmt.Sprintf("Found %d files in %d groups", len(scanResult.Files), len(scanResult.Groups)))

	var results []TargetResult

	// Phase 2: Process each group
	for _, group := range scanResult.Groups {
		for _, filePath := range group.Files {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Block if paused, respecting cancellation
			if opts.Pause != nil {
				if err := opts.Pause.WaitIfPaused(ctx); err != nil {
					return err
				}
			}

			// Skip-list check
			if !opts.NoSkip {
				if skip, reason := e.ShouldSkip(filepath.Base(filePath)); skip {
					results = append(results, TargetResult{
						Group: group, Skipped: true, SkipReason: reason,
					})
					filesProcessed++
					emit("skip", filePath, fmt.Sprintf("Skipped: %s", reason))
					continue
				}
			}

			// Exclude patterns
			if matchesExclude(filepath.Base(filePath), opts.Exclude) {
				results = append(results, TargetResult{
					Group: group, Skipped: true, SkipReason: "excluded",
				})
				filesProcessed++
				emit("skip", filePath, "Excluded by pattern")
				continue
			}

			// Recon
			emit("recon", filePath, "Classifying...")
			reconResult, err := e.Classify(filePath)
			if err != nil {
				filesProcessed++
				emitErr("recon", filePath, err)
				results = append(results, TargetResult{Group: group, Error: err})
				continue
			}

			// Override Kind based on scanner group classification
			switch group.Kind {
			case scanner.GroupUnreal:
				reconResult.Kind = recon.UnrealEngine
			case scanner.GroupUnityMono:
				reconResult.Kind = recon.UnityMono
			case scanner.GroupUnityIL2CPP:
				reconResult.Kind = recon.UnityIL2CPP
			}

			// Match recipe
			rec := e.MatchRecipe(&reconResult, opts.Recipe)
			if rec == nil {
				filesProcessed++
				emit("match", filePath, "No matching recipe found")
				results = append(results, TargetResult{
					Group: group, Recon: reconResult,
					Skipped: true, SkipReason: "no matching recipe",
				})
				continue
			}
			emit("match", filePath, fmt.Sprintf("Matched recipe: %s", rec.Name()))

			// Check required tools — auto-install if missing
			needed := e.tools.ToolsNeeded(rec.RequiredTools())
			if len(needed) > 0 {
				installFailed := false
				for _, name := range needed {
					emit("install", filePath, fmt.Sprintf("Installing %s...", name))
					if _, err := e.tools.Install(name); err != nil {
						emitErr("tools", filePath, fmt.Errorf("auto-install %s: %w", name, err))
						installFailed = true
						break
					}
				}
				if installFailed {
					filesProcessed++
					results = append(results, TargetResult{
						Group: group, Recon: reconResult, Recipe: rec,
						Error: fmt.Errorf("failed to auto-install required tools"),
					})
					continue
				}
				// Re-check after install
				needed = e.tools.ToolsNeeded(rec.RequiredTools())
				if len(needed) > 0 {
					err := fmt.Errorf("still missing after install: %v", needed)
					filesProcessed++
					emitErr("tools", filePath, err)
					results = append(results, TargetResult{
						Group: group, Recon: reconResult, Recipe: rec, Error: err,
					})
					continue
				}
			}

			// Pause check before execution
			if opts.Pause != nil {
				if err := opts.Pause.WaitIfPaused(ctx); err != nil {
					return err
				}
			}

			// Execute recipe
			targetOutput := filepath.Join(opts.Output, sanitizeName(filepath.Base(filePath)))
			os.MkdirAll(targetOutput, 0755)

			progressCh := make(chan recipe.StepProgress, 20)
			logCh := make(chan string, 50)

			// Forward progress events
			done := make(chan struct{})
			go func() {
				defer close(done)
				for {
					select {
					case p, ok := <-progressCh:
						if !ok {
							return
						}
						if events != nil {
							events <- PipelineEvent{
								Phase: "execute", Target: filePath, Progress: &p,
								FilesTotal: filesTotal, FilesProcessed: filesProcessed,
							}
						}
					case msg, ok := <-logCh:
						if !ok {
							return
						}
						emit("log", filePath, msg)
					}
				}
			}()

			rctx := &recipe.Context{
				Target:   filePath,
				Output:   targetOutput,
				Progress: progressCh,
				Log:      logCh,
				Tools:    e.tools,
				Ctx:      ctx,
				Config:   &e.cfg,
				Pause:    opts.Pause,
			}

			execErr := rec.Execute(rctx)
			close(progressCh)
			close(logCh)
			<-done
			filesProcessed++

			// Save recon.json per target
			reconJSON, _ := json.MarshalIndent(reconResult, "", "  ")
			os.WriteFile(filepath.Join(targetOutput, "recon.json"), reconJSON, 0644)

			results = append(results, TargetResult{
				Group:  group,
				Recon:  reconResult,
				Recipe: rec,
				Output: targetOutput,
				Error:  execErr,
			})

			if execErr != nil {
				emitErr("execute", filePath, execErr)
			} else {
				emit("execute", filePath, "Complete")
			}
		}
	}

	// Write summary.json
	summary := buildSummary(results)
	summaryJSON, _ := json.MarshalIndent(summary, "", "  ")
	os.WriteFile(filepath.Join(opts.Output, "summary.json"), summaryJSON, 0644)

	return nil
}

type summaryEntry struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Recipe     string `json:"recipe,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
	Error      string `json:"error,omitempty"`
}

func buildSummary(results []TargetResult) []summaryEntry {
	entries := make([]summaryEntry, 0, len(results))
	for _, r := range results {
		e := summaryEntry{
			Path:       r.Recon.Path,
			Kind:       r.Recon.Kind.String(),
			Skipped:    r.Skipped,
			SkipReason: r.SkipReason,
		}
		if r.Recipe != nil {
			e.Recipe = r.Recipe.Name()
		}
		if r.Error != nil {
			e.Error = r.Error.Error()
		}
		entries = append(entries, e)
	}
	return entries
}

func matchesExclude(filename string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, filename); matched {
			return true
		}
	}
	return false
}

func sanitizeName(name string) string {
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	return base
}
