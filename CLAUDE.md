# Morgue — Project Rules

## Stack
- **Backend:** Go 1.27.3, Wails v3.0.0 (wails3 CLI v3.0.0, @wailsio/runtime 3.0.0), Cobra v1.10.2
- **Frontend:** Svelte 5.57 (runes: $props, $state, $derived, $effect), TypeScript 6.0 (svelte-check not TS7-ready), Vite 8.3, Vitest 5, custom CSS utilities (no CSS framework)
- **Desktop:** Wails 3 (Go backend + embedded SPA)

## Architecture
- `cmd/morgue/main.go` — entry point (CLI args → Cobra, no args → Wails GUI + HTTP API)
- `internal/` — core Go packages (api, cli, config, engine, gamedata, instructions, metadata, odin, recipe, recon, scanner, selfupdate, services, skiplist, tools, util, webview2, tui [legacy, only `run --watch`])
- `internal/api/` — HTTP API server (`127.0.0.1:19876`) for hybrid mode. Handlers, SSE events, AI instructions; per-launch token (`internal/api/apitoken`).
- `internal/engine/` — pipeline execution engine. Emits `PipelineEvent` (`internal/engine/types.go`): Phase, Target, Message, Tool, Progress, Done, RunOutcome, TargetOutcome, Severity, ReconKind, Compiler, Obfuscator, Deobfuscator, FileSize, RecipeName, RecipeDesc, ToolsNeeded, OutputPath, OutputStats, FilesTotal, FilesProcessed. Go `Error` is not serialized; the wire carries its text as `error`. Run outcome: success | error | cancelled; target outcome adds skipped.
- `internal/recipe/` — one file per recipe (dotnet-generic, dotnet-confuserex, unity-mono, unity-il2cpp, native, delphi, ue5, java, nsis); see `AGENTS.md` for the recipe contract
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

### Shell Components (frontend/src/components/)
- `Sidebar.svelte` — page navigation
- `Header.svelte` — top bar with app-update / tool-update badges
- `ToolRow.svelte` — one row on the Tools page
- `SettingsToggle.svelte` — settings switch
- `CopyInstructions.svelte` — copies AI agent instructions
- `UpdateOverlay.svelte` — self-update overlay

### Pipeline State (frontend/src/lib/pipeline.ts)
- Phases: `idle → analysis → tools → execute → done | error | cancelled`
- `analysis` = merged scan + recon (backend still emits separate `scan`/`recon`/`match` events, frontend maps all → `analysis`)
- State includes: reconResults, obfuscations [{name, deobfuscator, affectedFiles}], toolsNeeded/Installed, downloadBytes, execCounters [{count, unit}], logs
- `updateFromEvent(data)` maps backend events to reactive store

### i18n (frontend/src/lib/i18n.ts)
- Languages: en, ru
- `t(lang, key)` function — no interpolation, components do `.replace()` themselves
- Key prefixes: about.*, composition.*, dropzone.*, execution.*, header.*, home.*, jobs.*, pipeline.*, runtimes.*, scan.*, settings.*, sidebar.*, stats.*, stepper.*, tool.*, tools.*, update.*

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
- GUI starts HTTP API on `127.0.0.1:19876` automatically
- CLI: `morgue api status|stop|run|tools|settings` — control running GUI (CLI `morgue run` stays standalone and invisible to the GUI)
- Auth: per-launch token written to `os.UserCacheDir()/morgue/api-token` (override `MORGUE_API_TOKEN_FILE`), sent as `X-Morgue-Token`; exempt: `GET /api/status`, `GET /api/instructions`
- Admission is synchronous: `PipelineService.Start` / `ToolsService.StartInstall` reserve the slot, then run in a goroutine; busy → `409 {"error":"busy"}`. One run at a time; `POST /api/stop` cancels it
- Wails events emitted from any goroutine DO reach the webview since v3 beta (`Emit` → `DispatchWailsEvent` → `ExecJS`/`InvokeSync` marshals to the main thread) — the old `PollAPICommand()` command queue is gone
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
| `docs/ROADMAP.md` | Release roadmap + removal candidates | tracked |
| `docs/specs/` | Design specs (IL2CPP, Unity gamedata, HashLink) | tracked |
| `docs/superpowers/` | Local implementation plans | gitignored |

## Conventions
- External tools download on-demand via `tools.Manager` — not pre-packaged
- Config file: `morgue.yaml` next to executable
- GitHub tokens in config — never commit `morgue.yaml`
- Pipeline events: backend emits granular phases (scan, recon, match, target, tools, download, extract, unpack, execute, stats, log) → frontend coalesces into 4 UI phases
- Obfuscation: detected during recon, `Deobfuscator` field maps to tool (e.g. ConfuserEx → de4dot). If no deobfuscator → UI shows error + GitHub issue link
