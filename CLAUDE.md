# Morgue — Project Rules

## Stack
- **Backend:** Go 1.25, Wails v3, Cobra CLI
- **Frontend:** Svelte 4, TypeScript, Vite 5, Tailwind CSS 4
- **Desktop:** Wails 3 (Go backend + embedded SPA)

## Architecture
- `cmd/morgue/main.go` — entry point (CLI args → Cobra, no args → Wails GUI + HTTP API)
- `internal/` — core Go packages (api, app, cli, config, engine, recipe, recon, scanner, selfupdate, services, skiplist, tools, tui [legacy])
- `internal/api/` — HTTP API server (localhost:19876) for hybrid mode. Handlers, SSE events, AI instructions.
- `internal/tools/` — tool download/install manager; tools stored in `BaseDir()/tools/<name>/`
- `frontend/src/` — Svelte SPA (components, pages, lib, stores)
- `BaseDir()` = directory of the running executable (`internal/util/paths.go`)

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

## Conventions
- External tools download on-demand via `tools.Manager` — not pre-packaged
- Config file: `morgue.yaml` next to executable
- GitHub tokens in config — never commit `morgue.yaml`
