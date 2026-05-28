# Structural Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve project structure by splitting god-functions, extracting reusable components, fixing broken imports, and cleaning .gitignore gaps.

**Architecture:** Surgical extractions preserving all existing behavior. No new features, no API changes. Each task is independently testable and committable. Backend splits use method extraction on Engine. Frontend splits use Svelte 5 component extraction with `$props()`.

**Tech Stack:** Go 1.25, Svelte 5, Vite 6, TypeScript, Cobra CLI

---

## File Map

### Will be modified
- `internal/engine/pipeline.go` — extract phases from `Run()` into sub-methods
- `frontend/src/components/PipelineProgress.svelte` — extract stepper, fix broken AnalysisPanel import
- `frontend/src/pages/SettingsPage.svelte` — extract SettingsToggle (18 repetitions)
- `.gitignore` — add missing patterns

### Will be created
- `internal/engine/emitter.go` — event emitter helper type
- `frontend/src/components/PipelineStepper.svelte` — stage indicator circles
- `frontend/src/components/SettingsToggle.svelte` — reusable toggle row

### Will NOT be changed (audit decisions)
- `internal/tools/` — well-factored, no split needed
- `internal/services/tools_service.go` — thin wrappers are Wails-mandated
- `internal/recipe/il2cpp.go` — fully functional recipe (256 lines of working code), NOT a stub
- `internal/tui/` — legacy but functional for `--watch`, leave for separate decision
- `cmd/morgue/main.go` — 343 lines, Cobra commands co-located with handlers in `internal/cli/` already; splitting further adds navigation overhead without proportional benefit
- `frontend/src/lib/pipeline.ts` — 315 lines, well-structured (types → store → events → history); splitting into 4 files creates circular dependency risk and navigation overhead for minimal gain

---

## Task 1: Extract `SettingsToggle.svelte` + `PipelineStepper.svelte` (P0 — Highest Value)

**Why first:** SettingsToggle eliminates 18x copy-paste. PipelineStepper reduces PipelineProgress by ~120 lines. Both are mechanical extractions with immediate DRY payoff.

**Files:**
- Create: `frontend/src/components/SettingsToggle.svelte`
- Create: `frontend/src/components/PipelineStepper.svelte`
- Modify: `frontend/src/pages/SettingsPage.svelte`
- Modify: `frontend/src/components/PipelineProgress.svelte`

### 1A: SettingsToggle

- [ ] **Step 1: Create `SettingsToggle.svelte`**

The toggle pattern appears **18 times** in SettingsPage.svelte. Each instance follows this pattern (example from line 94):

```svelte
<div class="toggle" class:active={config.AutoUpdateCheck} onclick={() => toggleField('AutoUpdateCheck')} onkeydown={(e) => handleToggleKey(e, 'AutoUpdateCheck')} role="switch" tabindex="0" aria-checked={config.AutoUpdateCheck}>
  <div class="toggle-slider"></div>
</div>
```

Extract to reusable component:

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
       tabindex="0" onclick={onToggle}
       onkeydown={(e) => e.key === 'Enter' && onToggle()}>
    <div class="toggle-slider"></div>
  </div>
</div>

<style>
  /* toggle + setting-row styles extracted from SettingsPage */
</style>
```

- [ ] **Step 2: Update `SettingsPage.svelte` to use `SettingsToggle`**

Replace each of 18 toggle instances with:

```svelte
<SettingsToggle label={t(lang, 'settings.autoUpdate')} active={config.AutoUpdateCheck} onToggle={() => toggleField('AutoUpdateCheck')} />
```

Toggle instances at lines: 94, 98, 102, 113, 117, 130, 134, 138, 174, 178, 192, 199, 206, 213, 220, 227, 234, 254.

- [ ] **Step 3: Verify visually**

Run: `task dev`
Expected: settings page identical. All 18 toggles work.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/SettingsToggle.svelte frontend/src/pages/SettingsPage.svelte
git commit -m "refactor(ui): extract SettingsToggle, eliminate 18x repeated pattern

SettingsPage reduced by ~130 lines. Toggle now reusable component
with proper ARIA attributes."
```

### 1B: PipelineStepper

- [ ] **Step 5: Create `PipelineStepper.svelte`**

Extract stepper from PipelineProgress.svelte: template lines 92-116, CSS lines 300-401.

Props: `stages` (computed stage status map), `stageIds` (ordered list), `lang` (i18n locale).

```svelte
<script lang="ts">
  import { t } from '$lib/i18n';
  // ... type StageStatus, $props()
</script>

<!-- stage circles, connector lines, labels from PipelineProgress lines 92-116 -->

<style>
  /* stepper styles from PipelineProgress lines 300-401 */
</style>
```

- [ ] **Step 6: Fix broken AnalysisPanel import in PipelineProgress.svelte**

Line 5 of PipelineProgress.svelte imports `AnalysisPanel.svelte` which **does not exist**. Either:
- Remove the import and any references to `<AnalysisPanel>` in template, OR
- If component is used in template, create a minimal placeholder

Investigate usage first, then fix.

- [ ] **Step 7: Update `PipelineProgress.svelte` to use `PipelineStepper`**

Replace template lines 92-116 with `<PipelineStepper {stages} {stageIds} {lang} />`. Remove stepper CSS (lines 300-401). Total reduction: ~120 lines.

- [ ] **Step 8: Build and verify**

Run: `cd frontend && npm run build`
Expected: clean build, no errors. Pipeline progress renders identically.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/components/PipelineStepper.svelte frontend/src/components/PipelineProgress.svelte
git commit -m "refactor(ui): extract PipelineStepper, fix broken AnalysisPanel import

Stage indicator circles/lines/labels now in own component.
PipelineProgress reduced by ~120 lines. Removed dead AnalysisPanel import."
```

---

## Task 2: Extract `Engine.Run()` phases (P1 — Tech Debt)

**Files:**
- Create: `internal/engine/emitter.go`
- Modify: `internal/engine/pipeline.go`

The `Run()` method is **~365 lines (lines 46-411)** containing 7 inline phases. Extract into sub-methods.

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

Extract lines 93-110 (skip-list check + exclude patterns):

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

Extract lines 113-142 (recon + kind override + event emission):

```go
// classifyTarget runs recon and applies scanner group kind override.
func (e *Engine) classifyTarget(
	ctx context.Context,
	group scanner.TargetGroup,
	filePath string,
	em emitter,
) (recon.Result, error) {
	// ... recon logic from Run(), lines 113-142
}
```

- [ ] **Step 4: Extract `ensureRuntimeDeps()` from pipeline.go**

Extract lines 174-227 (runtime dependency installation loop):

```go
// ensureRuntimeDeps installs missing runtime dependencies for a recipe.
func (e *Engine) ensureRuntimeDeps(filePath string, rec recipe.Recipe, em emitter) error {
	// ... runtime deps logic from Run(), lines 174-227
}
```

- [ ] **Step 5: Extract `ensureTools()` from pipeline.go**

Extract lines 236-291 (tool installation with download/extract callbacks):

```go
// ensureTools installs missing tools for a recipe.
func (e *Engine) ensureTools(filePath string, rec recipe.Recipe, em emitter) error {
	// ... tool install logic from Run(), lines 236-291
}
```

- [ ] **Step 6: Extract `executeRecipe()` from pipeline.go**

Extract lines 301-401 (output dir creation, progress/log goroutine, recipe execution, stats emission):

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
	// ... execution logic from Run(), lines 301-401
}
```

- [ ] **Step 7: Rewrite `Run()` as orchestrator**

Replace the ~365-line body with ~60 lines calling extracted methods:

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

These types (around lines 21-35 of pipeline.go) belong with other type definitions in `types.go`.

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

Split ~365-line god-function into shouldSkipTarget, classifyTarget,
ensureRuntimeDeps, ensureTools, executeRecipe. Add emitter helper type.
Run() is now ~60 lines orchestrating the phases."
```

---

## Task 3: Fix .gitignore gaps (P2 — Hygiene)

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Add missing .gitignore entries**

Append to `.gitignore`:

```gitignore
# WebView2 runtime cache
.webview2/

# Test/build artifacts
coverage.out
summary.json

# Design mockups
mockup-analysis.html
```

Note: BepInEx/, Cosmoteer/, HalflingCore/ are NOT tracked by git — no `git rm --cached` needed.

- [ ] **Step 2: Commit**

```bash
git add .gitignore
git commit -m "fix: add missing .gitignore entries for webview2 cache, coverage, mockups"
```

---

## Removed from original plan (with reasoning)

### ~~Move Cobra commands to internal/cli/~~ — REMOVED
`main.go` is 343 lines. Commands already delegate to `cli.*` handlers. Splitting command *builders* into separate files from their *handlers* adds navigation overhead. Not worth it for this project size.

### ~~Split pipeline.ts into 4 modules~~ — REMOVED
315 lines, well-structured with clear sections. Splitting creates circular dependency risk (`pipeline-events.ts` → `pipeline.ts` → `pipeline-types.ts`). Navigation cost exceeds DRY benefit at this size.

### ~~Unregister IL2CPP recipe~~ — REMOVED
IL2CPP is a **fully functional recipe** (256 lines, 4 steps, uses il2cppdumper/ilspycmd/strings). Original plan incorrectly called it a "stub". No change needed.

### ~~git rm stray dirs~~ — REMOVED
BepInEx/, Cosmoteer/, HalflingCore/ were **never tracked** by git. `git rm --cached` would be a no-op.

---

## Verification Checklist

After all tasks complete:

- [ ] `go build ./cmd/morgue/` — clean build
- [ ] `go test ./...` — all tests pass
- [ ] `cd frontend && npm run build` — clean frontend build
- [ ] `git status` — no untracked stray files
- [ ] Visual check: pipeline progress, settings page all look identical
