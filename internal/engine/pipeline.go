package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/UberMorgott/morgue/internal/recon"
	"github.com/UberMorgott/morgue/internal/recipe"
	"github.com/UberMorgott/morgue/internal/scanner"
	"github.com/UberMorgott/morgue/internal/tools"
)

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

// Run executes the full pipeline: scan → recon → skip → match → execute → save.
func (e *Engine) Run(ctx context.Context, opts Options, events chan<- PipelineEvent) error {
	startTime := time.Now()
	defer func() {
		if events != nil {
			events <- PipelineEvent{Phase: "done", Done: true, OutputPath: opts.Output}
		}
	}()

	emit := func(phase, target, msg string) {
		if events != nil {
			events <- PipelineEvent{Phase: phase, Target: target, Message: msg}
		}
	}
	emitErr := func(phase, target string, err error) {
		if events != nil {
			events <- PipelineEvent{Phase: phase, Target: target, Error: err}
		}
	}

	// Phase 1: Scan
	emit("scan", opts.Input, "Scanning for binaries...")
	scanResult, err := e.Scan(opts.Input)
	if err != nil {
		emitErr("scan", opts.Input, err)
		return fmt.Errorf("scan: %w", err)
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
					emit("skip", filePath, fmt.Sprintf("Skipped: %s", reason))
					continue
				}
			}

			// Exclude patterns
			if matchesExclude(filepath.Base(filePath), opts.Exclude) {
				results = append(results, TargetResult{
					Group: group, Skipped: true, SkipReason: "excluded",
				})
				emit("skip", filePath, "Excluded by pattern")
				continue
			}

			// Recon
			emit("recon", filePath, "Classifying...")
			reconResult, err := e.Classify(ctx, filePath)
			if err != nil {
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

			// Emit enriched recon event
			if events != nil {
				events <- PipelineEvent{
					Phase:      "recon",
					Target:     filePath,
					Message:    reconResult.Kind.String(),
					ReconKind:  reconResult.Kind.String(),
					Compiler:   reconResult.Compiler,
					Obfuscator: reconResult.Obfuscator,
					FileSize:   reconResult.Size,
				}
			}

			// Match recipe
			rec := e.MatchRecipe(&reconResult, opts.Recipe)
			if rec == nil {
				emit("match", filePath, "No matching recipe found")
				results = append(results, TargetResult{
					Group: group, Recon: reconResult,
					Skipped: true, SkipReason: "no matching recipe",
				})
				continue
			}
			if events != nil {
				events <- PipelineEvent{
					Phase:      "match",
					Target:     filePath,
					Message:    fmt.Sprintf("Recipe: %s", rec.Name()),
					RecipeName: rec.Name(),
					RecipeDesc: rec.Description(),
				}
			}

			// Emit tools event with full list before install
			if events != nil {
				events <- PipelineEvent{
					Phase:       "tools",
					Target:      filePath,
					Message:     fmt.Sprintf("%d tools needed", len(rec.RequiredTools())),
					ToolsNeeded: rec.RequiredTools(),
				}
			}

			// Check runtime dependencies — auto-install if missing
			runtimeFailed := false
			{
				runtimeSeen := map[tools.RuntimeKind]bool{}
				for _, tName := range rec.RequiredTools() {
					if runtimeFailed {
						break
					}
					td, ok := tools.FindByName(tName)
					if !ok {
						continue
					}
					for _, rk := range td.RuntimeDeps {
						if runtimeSeen[rk] {
							continue
						}
						runtimeSeen[rk] = true
						if _, err := e.tools.RuntimePath(rk); err == nil {
							continue // already available
						}
						emit("runtime", filePath, fmt.Sprintf("Installing runtime %s...", rk))
						rtCb := &tools.InstallCallbacks{
							OnProgress: func(name string, bytesDown, bytesTotal int64) {
								if events != nil {
									pct := 0
									if bytesTotal > 0 {
										pct = int(bytesDown * 100 / bytesTotal)
									}
									events <- PipelineEvent{
										Phase:   "download",
										Target:  filePath,
										Message: fmt.Sprintf("Downloading runtime %s... %d%%", name, pct),
									}
								}
							},
							OnExtract: func(name string) {
								if events != nil {
									events <- PipelineEvent{
										Phase:   "extract",
										Target:  filePath,
										Message: fmt.Sprintf("Extracting runtime %s...", name),
									}
								}
							},
						}
						if err := e.tools.InstallRuntime(rk, rtCb); err != nil {
							emitErr("runtime", filePath, fmt.Errorf("auto-install runtime %s: %w", rk, err))
							runtimeFailed = true
							break
						}
						emit("runtime", filePath, fmt.Sprintf("Runtime %s installed", rk))
					}
				}
			}
			if runtimeFailed {
				filesProcessed++
				results = append(results, TargetResult{
					Group: group, Recon: reconResult, Recipe: rec,
					Error: fmt.Errorf("failed to install required runtime"),
				})
				continue
			}

			// Check required tools — auto-install if missing
			needed := e.tools.ToolsNeeded(rec.RequiredTools())
			if len(needed) > 0 {
				// Wire download progress to pipeline events
				installCb := &tools.InstallCallbacks{
					OnProgress: func(tool string, bytesDown, bytesTotal int64) {
						if events != nil {
							pct := 0
							if bytesTotal > 0 {
								pct = int(bytesDown * 100 / bytesTotal)
							}
							events <- PipelineEvent{
								Phase:          "download",
								Target:         filePath,
								Message:        fmt.Sprintf("Downloading %s... %d%%", tool, pct),
								FilesTotal:     filesTotal,
								FilesProcessed: filesProcessed,
							}
						}
					},
					OnExtract: func(tool string) {
						if events != nil {
							events <- PipelineEvent{
								Phase:          "extract",
								Target:         filePath,
								Message:        fmt.Sprintf("Extracting %s...", tool),
								FilesTotal:     filesTotal,
								FilesProcessed: filesProcessed,
							}
						}
					},
				}

				installFailed := false
				for _, name := range needed {
					emit("install", filePath, fmt.Sprintf("Installing %s...", name))
					if _, err := e.tools.Install(name, installCb); err != nil {
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
			if err := os.MkdirAll(targetOutput, 0755); err != nil {
				filesProcessed++
				emitErr("execute", filePath, fmt.Errorf("create output dir: %w", err))
				results = append(results, TargetResult{
					Group: group, Recon: reconResult, Recipe: rec, Error: err,
				})
				continue
			}

			progressCh := make(chan recipe.StepProgress, 20)
			logCh := make(chan string, 50)

			// Forward progress events
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						log.Printf("progress forwarder panic: %v", r)
					}
				}()
				progressOpen := true
				logOpen := true
				for progressOpen || logOpen {
					select {
					case p, ok := <-progressCh:
						if !ok {
							progressOpen = false
							continue
						}
						if events != nil {
							events <- PipelineEvent{
								Phase: "execute", Target: filePath, Progress: &p,
								FilesTotal: filesTotal, FilesProcessed: filesProcessed,
							}
						}
					case msg, ok := <-logCh:
						if !ok {
							logOpen = false
							continue
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
				// Emit complete with updated file count
				if events != nil {
					events <- PipelineEvent{
						Phase: "execute", Target: filePath, Message: "Complete",
						FilesTotal: filesTotal, FilesProcessed: filesProcessed,
					}
				}
				// Post-execution: scan output and report stats
				stats := scanOutputDir(targetOutput)
				if events != nil {
					events <- PipelineEvent{
						Phase: "stats", Target: filePath,
						OutputStats: stats,
						FilesTotal: filesTotal, FilesProcessed: filesProcessed,
					}
				}
			}
		}
	}

	// Write summary.json
	summary := buildSummary(results, time.Since(startTime))
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

func buildSummary(results []TargetResult, elapsed time.Duration) PipelineSummary {
	entries := make([]summaryEntry, 0, len(results))
	stats := SummaryStats{
		ByKind:   make(map[string]int),
		ByRecipe: make(map[string]int),
	}

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

		stats.Total++
		if e.Kind != "" {
			stats.ByKind[e.Kind]++
		}
		if e.Recipe != "" {
			stats.ByRecipe[e.Recipe]++
		}

		switch {
		case r.Skipped:
			stats.Skipped++
		case r.Error != nil:
			stats.Failed++
		default:
			stats.Success++
		}
	}

	stats.Duration = elapsed.Round(time.Millisecond).String()

	return PipelineSummary{
		Stats:   stats,
		Results: entries,
	}
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
	if base == "" {
		return name
	}
	return base
}

func scanOutputDir(dir string) []string {
	var lines []string
	extCounts := map[string]int{}
	totalFiles := 0
	totalDirs := 0
	var totalSize int64

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			totalDirs++
			return nil
		}
		totalFiles++
		if info, err := d.Info(); err == nil {
			totalSize += info.Size()
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == "" {
			ext = "(no ext)"
		}
		extCounts[ext]++
		return nil
	})

	if totalDirs > 0 {
		totalDirs-- // exclude root directory itself
	}
	lines = append(lines, fmt.Sprintf("Output: %d files in %d directories (%.1f MB)", totalFiles, totalDirs, float64(totalSize)/1024/1024))

	type extCount struct {
		ext   string
		count int
	}
	var sorted []extCount
	for ext, count := range extCounts {
		sorted = append(sorted, extCount{ext, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	for _, ec := range sorted {
		lines = append(lines, fmt.Sprintf("  %s: %d files", ec.ext, ec.count))
	}

	return lines
}
