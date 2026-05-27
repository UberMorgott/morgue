# Structural Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve project structure by splitting god-functions, extracting reusable components, cleaning stray files, and fixing .gitignore gaps.

**Architecture:** Surgical extractions preserving all existing behavior. No new features, no API changes. Each task is independently testable and committable. Backend splits use method extraction on Engine. Frontend splits use Svelte component extraction with prop drilling.

**Tech Stack:** Go 1.25, Svelte 4, TypeScript, Cobra CLI

---

## File Map

### Will be modified
- `internal/engine/pipeline.go` — extract phases from `Run()` into sub-methods
- `cmd/morgue/main.go` — move Cobra command builders to `internal/cli/`
- `internal/cli/run.go` — add `NewRunCmd()` export
- `internal/cli/tools_cmd.go` — add `NewToolsCmd()` export
- `internal/cli/api_cmd.go` — add `NewApiCmd()` export
- `internal/cli/info.go` — add `NewInfoCmd()` export
- `internal/recipe/il2cpp.go` — remove `init()` registration
- `frontend/src/components/PipelineProgress.svelte` — extract sub-components
- `frontend/src/pages/SettingsPage.svelte` — extract SettingsToggle
- `frontend/src/lib/pipeline.ts` — split into modules
- `.gitignore` — add missing patterns

### Will be created
- `internal/engine/emitter.go` — event emitter helper type
- `internal/cli/selfupdate_cmd.go` — Cobra command builder for selfupdate
- `internal/cli/version_cmd.go` — Cobra command builder for version
- `frontend/src/components/PipelineStepper.svelte` — stage indicator circles
- `frontend/src/components/SettingsToggle.svelte` — reusable toggle row
- `frontend/src/lib/pipeline-types.ts` — pure type definitions
- `frontend/src/lib/pipeline-events.ts` — event handler logic
- `frontend/src/lib/history.ts` — run history store

### Will NOT be changed (audit corrections)
- `internal/tools/` — well-factored, no split needed (Sonnet wrongly suggested sub-packages)
- `internal/services/tools_service.go` — thin wrappers are Wails-mandated (Sonnet wrongly suggested merging selfupdate)
- `frontend/src/components/AnalysisPanel.svelte` — leaf component, 316/575 lines are CSS, logic is 145 lines. No high-value split.
- `internal/tui/` — legacy but functional for `--watch`, leave for separate decision

---

## Task 1: Extract `Engine.Run()` phases (P0 — Critical)

**Files:**
- Create: `internal/engine/emitter.go`
- Modify: `internal/engine/pipeline.go`

The `Run()` method is 355 lines (lines 38-393) containing 7 inline phases. Extract into sub-methods.

- [ ] **Step 1: Create `emitter.go`**

```go
// internal/engine/emitter.go
package engine

// emitter wraps the events channel with convenience methods.
type emitter struct {
	ch chan<- PipelineEvent
}

func (em emitter) emit(phase, target, msg string) {
	if em.ch == nil {
		return
	}
	em.ch <- PipelineEvent{Phase: phase, Target: target, Message: msg}
}

func (em emitter) emitErr(phase, target string, err error) {
	if em.ch == nil {
		return
	}
	em.ch <- PipelineEvent{Phase: phase, Target: target, Error: err.Error()}
}

func (em emitter) send(ev PipelineEvent) {
	if em.ch == nil {
		return
	}
	em.ch <- ev
}
```

- [ ] **Step 2: Extract `shouldSkipTarget()` from pipeline.go**

Extract lines 85-102 (skip-list check + exclude patterns) into:

```go
// shouldSkipTarget checks skip-list and exclude patterns.
// Returns (skip bool, reason string).
func (e *Engine) shouldSkipTarget(filePath string, opts *Options) (bool, string) {
	if reason, skip := e.ShouldSkip(filepath.Base(filePath)); skip {
		return true, reason
	}
	if matchesExclude(filepath.Base(filePath), opts.Exclude) {
		return true, "excluded by pattern"
	}
	return false, ""
}
```

- [ ] **Step 3: Extract `classifyTarget()` from pipeline.go**

Extract lines 104-134 (recon + kind override + event emission) into:

```go
// classifyTarget runs recon and applies scanner group kind override.
func (e *Engine) classifyTarget(
	ctx context.Context,
	group scanner.TargetGroup,
	filePath string,
	em emitter,
) (recon.Result, error) {
	// ... extract recon logic from Run(), lines 104-134
}
```

- [ ] **Step 4: Extract `ensureRuntimeDeps()` from pipeline.go**

Extract lines 168-226 (runtime dependency installation loop) into:

```go
// ensureRuntimeDeps installs missing runtime dependencies for a recipe.
func (e *Engine) ensureRuntimeDeps(filePath string, rec recipe.Recipe, em emitter) error {
	// ... extract runtime deps logic from Run(), lines 168-226
}
```

- [ ] **Step 5: Extract `ensureTools()` from pipeline.go**

Extract lines 228-283 (tool installation with download/extract callbacks) into:

```go
// ensureTools installs missing tools for a recipe.
func (e *Engine) ensureTools(filePath string, rec recipe.Recipe, em emitter) error {
	// ... extract tool install logic from Run(), lines 228-283
}
```

- [ ] **Step 6: Extract `executeRecipe()` from pipeline.go**

Extract lines 292-383 (output dir creation, progress/log goroutine, recipe execution, stats emission) into:

```go
// executeRecipe runs the recipe, forwards progress/log events, saves recon.json.
func (e *Engine) executeRecipe(
	ctx context.Context,
	filePath string,
	opts *Options,
	rec recipe.Recipe,
	reconResult recon.Result,
	em emitter,
) (TargetResult, error) {
	// ... extract execution logic from Run(), lines 292-383
}
```

- [ ] **Step 7: Rewrite `Run()` as orchestrator**

Replace the 355-line body with ~60 lines that call the extracted methods:

```go
func (e *Engine) Run(ctx context.Context, opts Options, events chan<- PipelineEvent) error {
	start := time.Now()
	em := emitter{ch: events}
	defer em.send(PipelineEvent{Phase: "done", Done: true, OutputPath: opts.Output})

	scanResult, err := e.Scan(opts.Input)
	if err != nil {
		return err
	}
	em.emit("scan", "", fmt.Sprintf("Found %d files in %d groups", ...))

	var results []TargetResult
	for _, group := range scanResult.Groups {
		for _, filePath := range group.Files {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			opts.Pause.WaitIfPaused(ctx)

			if skip, reason := e.shouldSkipTarget(filePath, &opts); skip {
				em.emit("skip", filePath, reason)
				results = append(results, TargetResult{Path: filePath, Skipped: true, SkipReason: reason})
				continue
			}

			result := e.processTarget(ctx, group, filePath, &opts, em)
			results = append(results, result)
		}
	}

	summary := buildSummary(results, time.Since(start))
	// write summary.json ...
	return nil
}
```

- [ ] **Step 8: Move `PipelineSummary` and `SummaryStats` types to types.go**

These types (lines 21-35 of pipeline.go) belong with the other type definitions in `types.go`.

- [ ] **Step 9: Build and verify**

Run: `go build ./...`
Expected: clean build, no errors.

- [ ] **Step 10: Run existing tests**

Run: `go test ./internal/engine/...`
Expected: all tests pass.

- [ ] **Step 11: Commit**

```bash
git add internal/engine/
git commit -m "refactor(engine): extract Run() phases into sub-methods

Split 355-line god-function into shouldSkipTarget, classifyTarget,
ensureRuntimeDeps, ensureTools, executeRecipe. Add emitter helper type.
Run() is now ~60 lines orchestrating the phases."
```

---

## Task 2: Move Cobra command builders to `internal/cli/` (P2)

**Files:**
- Modify: `cmd/morgue/main.go`, `internal/cli/run.go`, `internal/cli/tools_cmd.go`, `internal/cli/api_cmd.go`, `internal/cli/info.go`
- Create: `internal/cli/selfupdate_cmd.go`, `internal/cli/version_cmd.go`

Currently `main.go` has 6 Cobra command builder functions (lines 145-342) that just define flags and delegate to `cli.*` functions. Move them so `main.go` only has `main()`, `runGUI()`, `runCLI()`.

- [ ] **Step 1: Add `NewRunCmd()` to `internal/cli/run.go`**

Move `runCmd()` from main.go (lines 145-178) → `cli.NewRunCmd()`. The function builds a `*cobra.Command` with flags and calls `cli.Run()` in its RunE. Export the version/commit as parameters.

```go
// NewRunCmd creates the 'run' cobra command.
func NewRunCmd(version string) *cobra.Command {
	// ... moved from main.go runCmd(), lines 145-178
}
```

- [ ] **Step 2: Add `NewToolsCmd()` to `internal/cli/tools_cmd.go`**

Move `toolsCmd()` from main.go (lines 180-207) → `cli.NewToolsCmd()`.

- [ ] **Step 3: Create `internal/cli/version_cmd.go` with `NewVersionCmd()`**

Move `versionCmd()` from main.go (lines 209-217). Pass `version`, `commit` as parameters.

```go
func NewVersionCmd(version, commit string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("morgue %s (commit %s)\n", version, commit)
		},
	}
}
```

- [ ] **Step 4: Create `internal/cli/selfupdate_cmd.go` with `NewSelfUpdateCmd()`**

Move `selfUpdateCmd()` from main.go (lines 219-235).

- [ ] **Step 5: Add `NewApiCmd()` to `internal/cli/api_cmd.go`**

Move `apiCmd()` from main.go (lines 237-325) — this is the largest block, 88 lines of Cobra subcommand tree.

- [ ] **Step 6: Add `NewInfoCmd()` to `internal/cli/info.go`**

Move `infoCmd()` from main.go (lines 327-342).

- [ ] **Step 7: Simplify `runCLI()` in main.go**

```go
func runCLI() {
	root := &cobra.Command{Use: "morgue", Short: "Decompilation pipeline"}
	root.AddCommand(
		cli.NewRunCmd(Version),
		cli.NewToolsCmd(),
		cli.NewVersionCmd(Version, Commit),
		cli.NewSelfUpdateCmd(Version),
		cli.NewApiCmd(),
		cli.NewInfoCmd(Version, Commit),
	)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 8: Build and verify**

Run: `go build ./cmd/morgue/`
Expected: clean build.

- [ ] **Step 9: Commit**

```bash
git add cmd/morgue/main.go internal/cli/
git commit -m "refactor(cli): move Cobra command builders from main.go to internal/cli

main.go reduced from 343 to ~120 lines. Each command is now
co-located with its handler logic in internal/cli/."
```

---

## Task 3: Extract `PipelineStepper.svelte` and `SettingsToggle.svelte` (P1)

**Files:**
- Create: `frontend/src/components/PipelineStepper.svelte`
- Create: `frontend/src/components/SettingsToggle.svelte`
- Modify: `frontend/src/components/PipelineProgress.svelte`
- Modify: `frontend/src/pages/SettingsPage.svelte`

### 3A: PipelineStepper

- [ ] **Step 1: Create `PipelineStepper.svelte`**

Extract the stage stepper (PipelineProgress.svelte template lines 92-116, style lines 300-401) into a standalone component.

Props: `stages` (computed stage status map), `stageIds` (ordered list), `lang` (i18n).

```svelte
<script lang="ts">
  import { t } from '$lib/i18n';
  // ... type StageStatus, props
</script>

<!-- stage circles, connector lines, labels from lines 92-116 -->

<style>
  /* stepper styles from lines 300-401 */
</style>
```

- [ ] **Step 2: Update `PipelineProgress.svelte` to use `PipelineStepper`**

Replace lines 92-116 with `<PipelineStepper {stages} {stageIds} {lang} />`. Remove stepper styles (lines 300-401) from PipelineProgress.

- [ ] **Step 3: Verify visually**

Run: `task dev` or `npm run dev`
Expected: pipeline progress looks identical. Stage stepper renders correctly.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/PipelineStepper.svelte frontend/src/components/PipelineProgress.svelte
git commit -m "refactor(ui): extract PipelineStepper from PipelineProgress

Stage indicator circles/lines/labels now in own component.
PipelineProgress reduced by ~120 lines."
```

### 3B: SettingsToggle

- [ ] **Step 5: Create `SettingsToggle.svelte`**

The toggle pattern appears 15+ times in SettingsPage. Each instance is:

```svelte
<div class="setting-row">
  <span class="setting-label">{t(lang, 'key')}</span>
  <div class="toggle" class:active={config.field} onclick={() => toggleField('field')} onkeydown={...}>
    <div class="toggle-slider"></div>
  </div>
</div>
```

Extract to:

```svelte
<script lang="ts">
  let { label, active, onToggle }: {
    label: string;
    active: boolean;
    onToggle: () => void;
  } = $props();
</script>

<div class="setting-row">
  <span class="setting-label">{label}</span>
  <div class="toggle" class:active role="switch" aria-checked={active}
       onclick={onToggle} onkeydown={(e) => e.key === 'Enter' && onToggle()}>
    <div class="toggle-slider"></div>
  </div>
</div>

<style>
  /* toggle styles from SettingsPage */
</style>
```

- [ ] **Step 6: Update `SettingsPage.svelte` to use `SettingsToggle`**

Replace each toggle pattern with:

```svelte
<SettingsToggle label={t(lang, 'settings.autoUpdate')} active={config.autoUpdate} onToggle={() => toggleField('autoUpdate')} />
```

This replaces ~45 lines of repeated markup with ~15 lines of component usage.

- [ ] **Step 7: Verify visually**

Run: `task dev` or `npm run dev`
Expected: settings page looks identical. All toggles work.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/components/SettingsToggle.svelte frontend/src/pages/SettingsPage.svelte
git commit -m "refactor(ui): extract SettingsToggle, eliminate 15x repeated pattern

SettingsPage reduced by ~130 lines. Toggle now reusable component
with proper ARIA attributes."
```

---

## Task 4: Split `pipeline.ts` into modules (P2)

**Files:**
- Create: `frontend/src/lib/pipeline-types.ts`
- Create: `frontend/src/lib/pipeline-events.ts`
- Create: `frontend/src/lib/history.ts`
- Modify: `frontend/src/lib/pipeline.ts` (becomes pipeline store only)
- Modify: all importers of `pipeline.ts`

- [ ] **Step 1: Create `pipeline-types.ts`**

Move from pipeline.ts lines 4-44: `PipelinePhase`, `PipelineTarget`, `PipelineState`.

```typescript
// frontend/src/lib/pipeline-types.ts
export type PipelinePhase = 'idle' | 'scan' | 'recon' | 'tools' | 'execute' | 'done' | 'error' | 'cancelled';

export interface PipelineTarget { /* ... lines 8-12 */ }

export interface PipelineState { /* ... lines 14-44, all 25 fields */ }
```

- [ ] **Step 2: Create `history.ts`**

Move from pipeline.ts lines 284-315: `HistoryEntry`, `history` store, `addHistoryEntry()`, `loadHistory()`. This module has zero coupling to pipeline state.

```typescript
// frontend/src/lib/history.ts
import { writable } from 'svelte/store';

export interface HistoryEntry { /* ... lines 284-293 */ }
// ... loadHistory, history store, addHistoryEntry
```

- [ ] **Step 3: Create `pipeline-events.ts`**

Move from pipeline.ts lines 87-282: `startPipeline()`, `updateFromEvent()`. These import from `pipeline-types.ts` and update the store from `pipeline.ts`.

```typescript
// frontend/src/lib/pipeline-events.ts
import type { PipelineState } from './pipeline-types';
import { pipelineState } from './pipeline';
// ... startPipeline, updateFromEvent
```

- [ ] **Step 4: Slim down `pipeline.ts` to store-only**

Keep lines 46-85: `initial` const, `pipelineState` writable store, `isRunning` derived, `resetPipeline()`. Re-export types from `pipeline-types.ts` for backward compatibility.

```typescript
// frontend/src/lib/pipeline.ts
import type { PipelineState } from './pipeline-types';
export type { PipelinePhase, PipelineTarget, PipelineState } from './pipeline-types';

const initial: PipelineState = { /* ... */ };
export const pipelineState = writable<PipelineState>(initial);
export const isRunning = derived(pipelineState, $s => ...);
export function resetPipeline() { pipelineState.set(initial); }
```

- [ ] **Step 5: Update imports in consuming files**

Find all files importing from `pipeline.ts` and update:
- Components importing `startPipeline`/`updateFromEvent` → import from `pipeline-events`
- Components importing `HistoryEntry`/`history` → import from `history`
- Components importing types only → can import from `pipeline-types` or keep `pipeline` (re-exports)

- [ ] **Step 6: Build and verify**

Run: `cd frontend && npm run build`
Expected: clean build, no errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/lib/
git commit -m "refactor(frontend): split pipeline.ts into types, events, history modules

pipeline.ts: 315 -> ~40 lines (store only).
pipeline-types.ts: pure type definitions.
pipeline-events.ts: startPipeline + updateFromEvent.
history.ts: independent run history store."
```

---

## Task 5: Fix IL2CPP stub and .gitignore (P2-P3)

**Files:**
- Modify: `internal/recipe/il2cpp.go`
- Modify: `.gitignore`

- [ ] **Step 1: Remove IL2CPP from registry**

In `internal/recipe/il2cpp.go`, remove or comment out the `init()` function that calls `Register(&IL2CPP{})`. Keep the type definition for future implementation. This prevents the pipeline from matching IL2CPP targets and failing with a misleading error.

```go
// init registers IL2CPP when implementation is ready.
// func init() { Register(&IL2CPP{}) }
```

- [ ] **Step 2: Build and verify**

Run: `go build ./...`
Expected: clean build. IL2CPP recipe no longer in registry.

- [ ] **Step 3: Add missing .gitignore entries**

Append to `.gitignore`:

```gitignore
# WebView2 runtime cache
.webview2/

# Test/build artifacts
coverage.out
summary.json

# Design mockups
mockup-analysis.html

# Decompilation output (should use testbed/)
BepInEx/
Cosmoteer/
HalflingCore/
```

- [ ] **Step 4: Remove stray files from git tracking**

Run: `git rm --cached -r BepInEx/ Cosmoteer/ HalflingCore/ .webview2/ coverage.out summary.json mockup-analysis.html 2>/dev/null; true`

Note: only removes from git tracking, files stay on disk.

- [ ] **Step 5: Commit**

```bash
git add .gitignore internal/recipe/il2cpp.go
git commit -m "fix: unregister IL2CPP stub recipe, update .gitignore

IL2CPP init() disabled — stub was registering a recipe that always
fails. gitignore now covers webview2 cache, coverage output,
mockups, and stray decompilation dirs."
```

---

## Verification Checklist

After all tasks complete:

- [ ] `go build ./cmd/morgue/` — clean build
- [ ] `go test ./...` — all tests pass
- [ ] `cd frontend && npm run build` — clean frontend build
- [ ] `git status` — no untracked stray files
- [ ] Visual check: pipeline progress, settings page, analysis panel all look identical
