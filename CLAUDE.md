# Morgue — Project Rules

## Stack
- **Backend:** Go 1.26.4, Wails v3 (v3.0.0-alpha.98), Cobra CLI
- **Frontend:** Svelte 5 (runes: $props, $state, $derived, $effect), TypeScript 6, Vite 8, custom CSS utilities (no CSS framework)
- **Desktop:** Wails 3 (Go backend + embedded SPA)

## Architecture
- `cmd/morgue/main.go` — entry point (CLI args → Cobra, no args → Wails GUI + HTTP API)
- `internal/` — core Go packages (api, cli, config, engine, instructions, recipe, recon, scanner, selfupdate, services, skiplist, tools, util, webview2, tui [legacy])
- `internal/api/` — HTTP API server (localhost:19876) for hybrid mode. Handlers, SSE events, AI instructions.
- `internal/engine/` — pipeline execution engine. Emits `PipelineEvent` with fields: Phase, Target, Message, Obfuscator, Deobfuscator, Tool, ReconKind, Compiler, FileSize, RecipeName, RecipeDesc, Progress, Done, Error, Output.
- `internal/tools/` — tool download/install manager; tools stored in `BaseDir()/tools/<name>/`
- `frontend/src/` — Svelte SPA (components, pages, lib, stores)
- `BaseDir()` = directory of the running executable (`internal/util/paths.go`)

## Frontend Architecture

### Pages (frontend/src/pages/)
- `HomePage.svelte` — pipeline UI: DropZone (idle) → pipeline progress (running)
- `ToolsPage.svelte` — tool management
- `SettingsPage.svelte` — app settings
- `AboutPage.svelte` — version info

### Pipeline Components (frontend/src/components/)
- `PipelineProgress.svelte` — thin orchestrator, delegates to sub-panels
- `PipelineStepper.svelte` — 4-step visual stepper (Analysis → Tools → Execution → Done)
- `StatsStrip.svelte` — horizontal bar: platform, file count, size, obfuscation count
- `CompositionPanel.svelte` — aggregated file types + obfuscation detection blocks
- `ToolsPanel.svelte` — tool list with fill-based progress rings
- `ExecutionPanel.svelte` — per-tool execution rows with progress + counters + mini log
- `PipelineSummary.svelte` — completion stats, output path, full log
- `ProgressRing.svelte` — reusable SVG circular progress (fill-based + indeterminate)
- `DropZone.svelte` — drag-and-drop file/folder picker
- `PipelineHistory.svelte` — recent runs (localStorage)

### Pipeline State (frontend/src/lib/pipeline.ts)
- Phases: `idle → analysis → tools → execute → done | error | cancelled`
- `analysis` = merged scan + recon (backend still emits separate `scan`/`recon`/`match` events, frontend maps all → `analysis`)
- State includes: reconResults, obfuscations [{name, deobfuscator, affectedFiles}], toolsNeeded/Installed, downloadBytes, execCounters [{count, unit}], logs
- `updateFromEvent(data)` maps backend events to reactive store

### i18n (frontend/src/lib/i18n.ts)
- Languages: en, ru
- `t(lang, key)` function — no interpolation, components do `.replace()` themselves
- Key prefixes: home.*, pipeline.*, dropzone.*, tools.*, runtimes.*, stats.*, composition.*, execution.*, stepper.*

### CSS Architecture (frontend/src/app.css)
Theme: "Molten Forge" — dark charcoal + amber glass + neon orange.

**Global utility classes (use these, don't duplicate):**
- `.glass` — glass morphism panel (bg, blur, border, shadow, radius)
- `.neon-border` — orange neon glow border
- `.pipeline-panel` — standard pipeline section (flex-col, gap 14px, padding 18px 20px)
- `.panel-title` — Orbitron uppercase heading with accent glow
- `.font-accent` — Orbitron font family (numbers, counters, tool names)
- `.font-mono` — Consolas monospace (logs, paths, code)
- `.text-xs` / `.text-sm` / `.text-base` — font size scale
- `.section-label` — small uppercase muted label
- `.card-sm` — small card block (padding, radius, bg)
- `.alert-block` + `.alert-warning` / `.alert-error` — alert containers
- `.log-area` — monospace scrollable output with text selection
- `.row-separator` — bottom border with :last-child removal
- `.animate-in` — slide-up fade entrance animation

**CSS variables:** `--accent`, `--accent-hot/warm/bright`, `--success`, `--error`, `--warning`, `--info`, `--text-primary/secondary/muted/heading`, `--bg-page/glass/card`, `--border`, `--glass-*`, `--radius-sm/md/lg/xl`

**Rule:** New components MUST use global classes. No copy-pasting font-family, padding, border, or animation styles into scoped CSS.

### Svelte Conventions
- **Svelte 5 runes ONLY:** `$props()`, `$state()`, `$derived()`, `$effect()`. NO `export let`, NO `$:` reactive statements.
- Props: destructured from `$props()` with types
- Stores: `$storeName` subscription syntax

## Hybrid Mode
- GUI starts HTTP API on `localhost:19876` automatically
- CLI: `morgue api status|run|tools|settings` — control running GUI
- Command queue: API pushes commands → frontend polls `PollAPICommand()` → executes via Wails binding (ensures progress events reach webview)
- Wails events from HTTP goroutines DON'T reach webview — always use command queue pattern
- SSE endpoint `/api/events` works for external clients (curl/CLI), NOT from Wails webview

## Build & Test

### Build (`build.bat`)
Builds production binary into **`dist/`**:
1. Frontend: `npm install` + `npm run build`
2. Go binary: compiled directly into `dist/morgue.exe` with version/commit ldflags

### Test (`test.bat`)
Copies build from `dist/` into isolated **`testbed/`** and launches:
- All downloaded tools, caches, decompilation artifacts stay in `testbed/`
- Does NOT touch project root or `dist/`
- Always run `build.bat` before `test.bat`

### Dev mode (`task dev`)
Uses Wails dev server with hot reload. Requires `wails3` in PATH.

## Directories
| Path | Purpose | Git |
|------|---------|-----|
| `dist/` | Clean distributable (exe + config only) | ignored |
| `testbed/` | Isolated test sandbox (tools, artifacts, everything) | ignored |
| `frontend/dist/` | Built SPA (embedded in Go binary) | ignored |
| `docs/specs/` | Design specs (UX, decompiler, pipeline) | tracked |
| `docs/superpowers/` | Implementation plans | gitignored |

## Conventions
- External tools download on-demand via `tools.Manager` — not pre-packaged
- Config file: `morgue.yaml` next to executable
- GitHub tokens in config — never commit `morgue.yaml`
- Pipeline events: backend emits granular phases (scan, recon, match, tools, install, download, extract, execute, log) → frontend coalesces into 4 UI phases
- Obfuscation: detected during recon, `Deobfuscator` field maps to tool (e.g. ConfuserEx → de4dot). If no deobfuscator → UI shows error + GitHub issue link
