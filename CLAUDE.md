# Morgue — Project Rules

## Stack
- **Backend:** Go 1.25, Wails v3, Cobra CLI
- **Frontend:** Svelte 4, TypeScript, Vite 5, Tailwind CSS 4
- **Desktop:** Wails 3 (Go backend + embedded SPA)

## Architecture
- `cmd/morgue/main.go` — entry point (CLI args → Cobra, no args → Wails GUI)
- `internal/` — core Go packages (app, cli, config, engine, recipe, recon, scanner, selfupdate, services, skiplist, tools, tui [legacy])
- `frontend/src/` — Svelte SPA (components, pages, lib, stores)
- `internal/tools/` — tool download/install manager; tools stored in `BaseDir()/<name>/`
- `BaseDir()` = directory of the running executable (`internal/util/paths.go`)

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
