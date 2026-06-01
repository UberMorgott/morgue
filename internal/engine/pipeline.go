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

// pauseChecker returns nil interface if pg is nil, avoiding the nil-pointer-in-interface trap.
func pauseChecker(pg *PauseGate) recipe.PauseChecker {
	if pg == nil {
		return nil
	}
	return pg
}

// fileTask holds the analysis result for a single file (pass 1), used in pass 2.
type fileTask struct {
	group    scanner.TargetGroup
	filePath string
	recon    recon.Result
	recipe   recipe.Recipe
}

// Run executes the full pipeline: scan → analyse all → emit tools → execute all → save.
func (e *Engine) Run(ctx context.Context, opts Options, events chan<- PipelineEvent) error {
	startTime := time.Now()
	em := emitter{ch: events}
	defer func() {
		em.send(PipelineEvent{Phase: "done", Done: true, OutputPath: opts.Output})
	}()

	// Phase 1: Scan
	em.emit("scan", opts.Input, "Scanning for binaries...")
	scanResult, err := e.Scan(opts.Input)
	if err != nil {
		em.emitErr("scan", opts.Input, err)
		return fmt.Errorf("scan: %w", err)
	}
	// Count target files across all groups. This includes Unreal pak groups,
	// which are collapsed to a single representative file and are NOT part of
	// scanResult.Files (so a paks-only dir still reports a non-zero total).
	targetFiles := 0
	for _, g := range scanResult.Groups {
		targetFiles += len(g.Files)
	}
	em.send(PipelineEvent{
		Phase:      "scan",
		Target:     opts.Input,
		Message:    fmt.Sprintf("Found %d files in %d groups", targetFiles, len(scanResult.Groups)),
		FilesTotal: targetFiles,
	})

	var results []TargetResult
	var tasks []fileTask

	// Pass 1 (analysis): recon + match for every file, collect required tools.
	for _, group := range scanResult.Groups {
		for _, filePath := range group.Files {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

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
					em.emit("skip", filePath, fmt.Sprintf("Skipped: %s", reason))
					continue
				}
			}

			// Exclude patterns
			if matchesExclude(filepath.Base(filePath), opts.Exclude) {
				results = append(results, TargetResult{
					Group: group, Skipped: true, SkipReason: "excluded",
				})
				em.emit("skip", filePath, "Excluded by pattern")
				continue
			}

			// Recon + kind override
			reconResult, err := e.classifyTarget(ctx, group, filePath, em)
			if err != nil {
				results = append(results, TargetResult{Group: group, Error: err})
				continue
			}

			// Match recipe
			rec := e.MatchRecipe(&reconResult, opts.Recipe)
			if rec == nil {
				em.emit("match", filePath, "No matching recipe found")
				results = append(results, TargetResult{
					Group: group, Recon: reconResult,
					Skipped: true, SkipReason: "no matching recipe",
				})
				continue
			}
			em.send(PipelineEvent{
				Phase:      "match",
				Target:     filePath,
				Message:    fmt.Sprintf("Recipe: %s", rec.Name()),
				RecipeName: rec.Name(),
				RecipeDesc: rec.Description(),
			})

			tasks = append(tasks, fileTask{
				group:    group,
				filePath: filePath,
				recon:    reconResult,
				recipe:   rec,
			})
		}
	}

	// Emit ONE aggregated tools event with all unique tools in execution order.
	// A recipe may expose DisplayTools() to list tools it runs that are not
	// downloadable RequiredTools (e.g. the built-on-demand cfxextract), so the
	// UI shows every participating tool up front instead of popping one in
	// mid-run. Downloads still use RequiredTools only (see ensureTools).
	allToolsSeen := map[string]bool{}
	var allTools []string
	for _, t := range tasks {
		toolsForDisplay := t.recipe.RequiredTools()
		if dp, ok := t.recipe.(interface{ DisplayTools() []string }); ok {
			if dt := dp.DisplayTools(); len(dt) > 0 {
				toolsForDisplay = dt
			}
		}
		for _, name := range toolsForDisplay {
			if !allToolsSeen[name] {
				allToolsSeen[name] = true
				allTools = append(allTools, name)
			}
		}
	}
	if len(allTools) > 0 {
		em.send(PipelineEvent{
			Phase:       "tools",
			Target:      opts.Input,
			Message:     fmt.Sprintf("%d tools needed", len(allTools)),
			ToolsNeeded: allTools,
		})
	}

	// Pass 2 (execution): install tools + run recipes.
	// Native recipe tasks are batched: all strings first, then all ghidra,
	// so the user sees a clean phase progression instead of interleaving.
	var nativeTasks []fileTask

	for _, t := range tasks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if opts.Pause != nil {
			if err := opts.Pause.WaitIfPaused(ctx); err != nil {
				return err
			}
		}

		// Check runtime dependencies
		if err := e.ensureRuntimeDeps(t.filePath, t.recipe, em); err != nil {
			results = append(results, TargetResult{
				Group: t.group, Recon: t.recon, Recipe: t.recipe,
				Error: fmt.Errorf("failed to install required runtime"),
			})
			continue
		}

		// Check required tools
		if err := e.ensureTools(t.filePath, t.recipe, em); err != nil {
			results = append(results, TargetResult{
				Group: t.group, Recon: t.recon, Recipe: t.recipe, Error: err,
			})
			continue
		}

		// Pause check before execution
		if opts.Pause != nil {
			if err := opts.Pause.WaitIfPaused(ctx); err != nil {
				return err
			}
		}

		// Batch native recipe tasks for phased execution
		if t.recipe.Name() == "native" {
			nativeTasks = append(nativeTasks, t)
			continue
		}

		// Execute non-native recipes immediately
		tr := e.executeRecipe(ctx, t.filePath, &opts, t.recipe, t.recon, t.group, em)
		results = append(results, tr)
	}

	// Execute native tasks in two phases: strings first, then ghidra.
	// This prevents confusing interleaving (strings→ghidra→strings→ghidra)
	// and shows a clean progression: all strings done, then all ghidra.
	if len(nativeTasks) > 1 {
		// Phase 1: strings for all native files
		for _, t := range nativeTasks {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if opts.Pause != nil {
				if err := opts.Pause.WaitIfPaused(ctx); err != nil {
					return err
				}
			}
			tr := e.executeRecipeWithFilter(ctx, t.filePath, &opts, t.recipe, t.recon, t.group, em, "strings")
			if tr.Error != nil {
				// Strings failure is non-fatal; still attempt ghidra later
				em.emitErr("execute", t.filePath, tr.Error)
			}
		}
		// Phase 2: ghidra for all native files
		for _, t := range nativeTasks {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if opts.Pause != nil {
				if err := opts.Pause.WaitIfPaused(ctx); err != nil {
					return err
				}
			}
			tr := e.executeRecipeWithFilter(ctx, t.filePath, &opts, t.recipe, t.recon, t.group, em, "ghidra")
			results = append(results, tr)
		}
	} else if len(nativeTasks) == 1 {
		// Single native file — no batching needed, run all steps
		t := nativeTasks[0]
		tr := e.executeRecipe(ctx, t.filePath, &opts, t.recipe, t.recon, t.group, em)
		results = append(results, tr)
	}

	// Write summary.json
	summary := buildSummary(results, time.Since(startTime))
	summaryJSON, _ := json.MarshalIndent(summary, "", "  ")
	os.WriteFile(filepath.Join(opts.Output, "summary.json"), summaryJSON, 0644)

	return nil
}

// classifyTarget runs recon, applies scanner group kind override, and emits events.
func (e *Engine) classifyTarget(
	ctx context.Context,
	group scanner.TargetGroup,
	filePath string,
	em emitter,
) (recon.Result, error) {
	em.emit("recon", filePath, "Classifying...")
	reconResult, err := e.Classify(ctx, filePath)
	if err != nil {
		em.emitErr("recon", filePath, err)
		return reconResult, err
	}

	// Override Kind based on scanner group classification
	switch group.Kind {
	case scanner.GroupUnreal:
		reconResult.Kind = recon.UnrealEngine
		reconResult.Fallback = false
	case scanner.GroupUnityMono:
		reconResult.Kind = recon.UnityMono
		reconResult.Fallback = false
	case scanner.GroupUnityIL2CPP:
		reconResult.Kind = recon.UnityIL2CPP
		reconResult.Fallback = false
	}

	// Emit enriched recon event
	em.send(PipelineEvent{
		Phase:        "recon",
		Target:       filePath,
		Message:      reconResult.Kind.String(),
		ReconKind:    reconResult.Kind.String(),
		Compiler:     reconResult.Compiler,
		Obfuscator:   reconResult.Obfuscator,
		Deobfuscator: resolveDeobfuscator(reconResult.Obfuscator),
		FileSize:     reconResult.Size,
	})

	return reconResult, nil
}

// ensureRuntimeDeps installs missing runtime dependencies for a recipe.
func (e *Engine) ensureRuntimeDeps(filePath string, rec recipe.Recipe, em emitter) error {
	runtimeSeen := map[tools.RuntimeKind]bool{}
	for _, tName := range rec.RequiredTools() {
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
			em.emit("runtime", filePath, fmt.Sprintf("Installing runtime %s...", rk))
			rtCb := &tools.InstallCallbacks{
				OnProgress: func(name string, bytesDown, bytesTotal int64) {
					if em.ch != nil {
						pct := 0
						if bytesTotal > 0 {
							pct = int(bytesDown * 100 / bytesTotal)
						}
						em.send(PipelineEvent{
							Phase:   "download",
							Target:  filePath,
							Message: fmt.Sprintf("Downloading runtime %s... %d%%", name, pct),
						})
					}
				},
				OnExtract: func(name string) {
					if em.ch != nil {
						em.send(PipelineEvent{
							Phase:   "extract",
							Target:  filePath,
							Message: fmt.Sprintf("Extracting runtime %s...", name),
						})
					}
				},
			}
			if err := e.tools.InstallRuntime(rk, rtCb); err != nil {
				em.emitErr("runtime", filePath, fmt.Errorf("auto-install runtime %s: %w", rk, err))
				return err
			}
			em.emit("runtime", filePath, fmt.Sprintf("Runtime %s installed", rk))
		}
	}
	return nil
}

// ensureTools installs missing tools for a recipe.
func (e *Engine) ensureTools(filePath string, rec recipe.Recipe, em emitter) error {
	needed := e.tools.ToolsNeeded(rec.RequiredTools())
	if len(needed) == 0 {
		return nil
	}

	// Wire download progress to pipeline events
	installCb := &tools.InstallCallbacks{
		OnProgress: func(tool string, bytesDown, bytesTotal int64) {
			if em.ch != nil {
				pct := 0
				if bytesTotal > 0 {
					pct = int(bytesDown * 100 / bytesTotal)
				}
				em.send(PipelineEvent{
					Phase:   "download",
					Target:  filePath,
					Message: fmt.Sprintf("Downloading %s... %d%%", tool, pct),
				})
			}
		},
		OnExtract: func(tool string) {
			if em.ch != nil {
				em.send(PipelineEvent{
					Phase:   "extract",
					Target:  filePath,
					Message: fmt.Sprintf("Extracting %s...", tool),
				})
			}
		},
	}

	installFailed := false
	for _, name := range needed {
		em.emit("install", filePath, fmt.Sprintf("Installing %s...", name))
		if _, err := e.tools.Install(name, installCb); err != nil {
			em.emitErr("tools", filePath, fmt.Errorf("auto-install %s: %w", name, err))
			installFailed = true
			break
		}
	}
	if installFailed {
		return fmt.Errorf("failed to auto-install required tools")
	}

	// Re-check after install
	needed = e.tools.ToolsNeeded(rec.RequiredTools())
	if len(needed) > 0 {
		err := fmt.Errorf("still missing after install: %v", needed)
		em.emitErr("tools", filePath, err)
		return err
	}
	return nil
}

// executeRecipe runs the recipe, forwards progress/log events, saves recon.json.
func (e *Engine) executeRecipe(
	ctx context.Context,
	filePath string,
	opts *Options,
	rec recipe.Recipe,
	reconResult recon.Result,
	group scanner.TargetGroup,
	em emitter,
) TargetResult {
	targetOutput := filepath.Join(opts.Output, sanitizeName(filepath.Base(filePath)))
	if err := os.MkdirAll(targetOutput, 0755); err != nil {
		em.emitErr("execute", filePath, fmt.Errorf("create output dir: %w", err))
		return TargetResult{
			Group: group, Recon: reconResult, Recipe: rec, Error: err,
		}
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
		var currentTool string
		progressOpen := true
		logOpen := true
		for progressOpen || logOpen {
			select {
			case p, ok := <-progressCh:
				if !ok {
					progressOpen = false
					continue
				}
				if p.Tool != "" {
					currentTool = p.Tool
				}
				em.send(PipelineEvent{
					Phase: "execute", Target: filePath, Tool: p.Tool, Progress: &p,
				})
			case msg, ok := <-logCh:
				if !ok {
					logOpen = false
					continue
				}
				tool := currentTool
				logMsg := msg
				// Parse [tool] prefix if present — recipes tag log messages
				// with their tool to avoid misattribution from channel races.
				if strings.HasPrefix(msg, "[") {
					if idx := strings.Index(msg, "] "); idx > 0 {
						tool = msg[1:idx]
						logMsg = msg[idx+2:]
					}
				}
				em.send(PipelineEvent{
					Phase: "log", Target: filePath, Tool: tool, Message: logMsg,
				})
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
		Pause:    pauseChecker(opts.Pause),

		AllowDynamic: opts.AllowDynamic,
	}

	execErr := rec.Execute(rctx)
	close(progressCh)
	close(logCh)
	<-done

	// Save recon.json per target
	reconJSON, _ := json.MarshalIndent(reconResult, "", "  ")
	os.WriteFile(filepath.Join(targetOutput, "recon.json"), reconJSON, 0644)

	// Cleanup intermediates if configured and execution succeeded.
	// NOTE: original/ is intentionally NOT removed here — recipes always
	// persist a single copy of the target binary for reproducibility, and
	// keeping it consistent across all recipes is worth the small disk cost.
	if execErr == nil && !e.cfg.KeepIntermediates {
		// For IL2CPP: remove DummyDll (already decompiled to src/)
		os.RemoveAll(filepath.Join(targetOutput, "metadata", "DummyDll"))
		// Remove raw strings.txt (structured strings.json is kept)
		os.Remove(filepath.Join(targetOutput, "strings.txt"))
	}

	tr := TargetResult{
		Group:  group,
		Recon:  reconResult,
		Recipe: rec,
		Output: targetOutput,
		Error:  execErr,
	}

	if execErr != nil {
		em.emitErr("execute", filePath, execErr)
	} else {
		// Emit complete
		em.send(PipelineEvent{
			Phase: "execute", Target: filePath, Message: "Complete",
		})
		// Post-execution: scan output and report stats
		stats := scanOutputDir(targetOutput)
		em.send(PipelineEvent{
			Phase: "stats", Target: filePath,
			OutputStats: stats,
		})
	}

	return tr
}

// executeRecipeWithFilter runs a recipe with a StepFilter for batch/phased execution.
// Used by native recipe batching to run strings-only or ghidra-only passes.
func (e *Engine) executeRecipeWithFilter(
	ctx context.Context,
	filePath string,
	opts *Options,
	rec recipe.Recipe,
	reconResult recon.Result,
	group scanner.TargetGroup,
	em emitter,
	stepFilter string,
) TargetResult {
	targetOutput := filepath.Join(opts.Output, sanitizeName(filepath.Base(filePath)))
	if err := os.MkdirAll(targetOutput, 0755); err != nil {
		em.emitErr("execute", filePath, fmt.Errorf("create output dir: %w", err))
		return TargetResult{
			Group: group, Recon: reconResult, Recipe: rec, Error: err,
		}
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
		var currentTool string
		progressOpen := true
		logOpen := true
		for progressOpen || logOpen {
			select {
			case p, ok := <-progressCh:
				if !ok {
					progressOpen = false
					continue
				}
				if p.Tool != "" {
					currentTool = p.Tool
				}
				em.send(PipelineEvent{
					Phase: "execute", Target: filePath, Tool: p.Tool, Progress: &p,
				})
			case msg, ok := <-logCh:
				if !ok {
					logOpen = false
					continue
				}
				tool := currentTool
				logMsg := msg
				if strings.HasPrefix(msg, "[") {
					if idx := strings.Index(msg, "] "); idx > 0 {
						tool = msg[1:idx]
						logMsg = msg[idx+2:]
					}
				}
				em.send(PipelineEvent{
					Phase: "log", Target: filePath, Tool: tool, Message: logMsg,
				})
			}
		}
	}()

	rctx := &recipe.Context{
		Target:     filePath,
		Output:     targetOutput,
		Progress:   progressCh,
		Log:        logCh,
		Tools:      e.tools,
		Ctx:        ctx,
		Config:     &e.cfg,
		Pause:      pauseChecker(opts.Pause),
		StepFilter: stepFilter,

		AllowDynamic: opts.AllowDynamic,
	}

	execErr := rec.Execute(rctx)
	close(progressCh)
	close(logCh)
	<-done

	tr := TargetResult{
		Group:  group,
		Recon:  reconResult,
		Recipe: rec,
		Output: targetOutput,
		Error:  execErr,
	}

	// For the final phase (ghidra), emit completion events and save recon
	if stepFilter == "ghidra" {
		reconJSON, _ := json.MarshalIndent(reconResult, "", "  ")
		os.WriteFile(filepath.Join(targetOutput, "recon.json"), reconJSON, 0644)

		// original/ intentionally kept (see note in executeRecipe cleanup).
		if execErr == nil && !e.cfg.KeepIntermediates {
			os.Remove(filepath.Join(targetOutput, "strings.txt"))
		}

		if execErr != nil {
			em.emitErr("execute", filePath, execErr)
		} else {
			em.send(PipelineEvent{
				Phase: "execute", Target: filePath, Message: "Complete",
			})
			stats := scanOutputDir(targetOutput)
			em.send(PipelineEvent{
				Phase: "stats", Target: filePath,
				OutputStats: stats,
			})
		}
	}

	return tr
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

// resolveDeobfuscator maps a detected obfuscator name to its deobfuscator tool.
// Returns empty string when no automated deobfuscator is available.
func resolveDeobfuscator(obfuscator string) string {
	if obfuscator == "" {
		return ""
	}
	lower := strings.ToLower(obfuscator)
	switch {
	case strings.Contains(lower, "confuserex"), strings.Contains(lower, "confuser"):
		return "de4dot"
	default:
		return ""
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
