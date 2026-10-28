# Morgue — Agent Guide

Project rules, architecture and build/test workflow live in [CLAUDE.md](CLAUDE.md); read it first.
Planned work: [docs/ROADMAP.md](docs/ROADMAP.md). This file covers the most common agent task:
**adding or changing a recipe**.

## Recipe contract (`internal/recipe/recipe.go`)

```go
type Recipe interface {
    Name() string                 // CLI/API id, e.g. "java"; used by --recipe and summary.json
    Description() string
    Match(r *recon.Result) bool   // pure; decides on Kind/Compiler/Obfuscator/Runtime/SubType
    Steps() []StepInfo            // ordered step list shown in GUI/CLI progress
    RequiredTools() []string      // tool registry names; engine installs them before Execute
    Execute(ctx *Context) error
}
```

- Registration: `func init() { Register(&X{}) }` in the recipe file. `recipe.Match` returns the **first**
  match in registry order; `RegisterFirst` prepends (used by `dotnet-confuserex`, `nsis`). Init order is
  filename order, so keep `Match` conditions disjoint instead of relying on order.
- `RequiredTools` must list only tools the recipe cannot run without: the engine aborts the run if any
  entry is still missing after install. Optional tools (e.g. `assetstudiomod` in `unity-mono`) are resolved
  inside `Execute` and the step degrades gracefully.
- `Context` (same file) carries `Target`, `Output` (per-target dir), `SharedOut` (run root, for per-game
  products), `Tools` (`*tools.Manager`), `Config`, `ReferencePaths` (`--ref`), `AllowDynamic`, `Cflow`,
  `CodeOnly` / `SkipAssets()`, `GameDataOut`, `StepFilter`, `Pause`, and the `Progress` / `Log` channels.
- Report every step on `ctx.Progress` as `StepProgress{Step, Total, Name, Tool, Status, Duration, Error,
  Count, CountTotal, Unit}` (`Running` → `Success|Failed|Skipped`). Prefix log lines with `[tool] `.
- Steps that execute target code must be gated by `ctx.AllowDynamic`.
- Resolve binaries with `ctx.Tools.Resolve("<tool>")` and runtimes with
  `ctx.Tools.RuntimePath(tools.RuntimeJava|...)`. Run them via `internal/util` (`RunCmd*`,
  `RunCmdStreaming*`) so the context, memory cap and logging behave the same everywhere.
- Smallest complete example: `internal/recipe/java.go` (copy original → decompile → build indexes).

## Adding a recipe — checklist

1. **Detection.** New target type → append a `Kind` at the **end** of the enum in
   `internal/recon/result.go` and add its name to `kindNames` (integer values must stay stable). Classify it
   in `recon.Classify` (`internal/recon/pe.go`); new file extensions also go into the scanner allow-list
   (`internal/scanner/scanner.go`).
2. **Tool.** Add a `ToolDef` to `internal/tools/registry.go` (`Name`, `Category`, `Method`
   = `MethodGitHubRelease|MethodDirectURL|MethodNuGet`, `Repo`/`URL`, `AssetGlob`, `Binary`, optional
   `Version` pin, `SHA256` for pinned assets, `RuntimeDeps`). Tools install on demand into
   `BaseDir()/tools/<name>/`; never vendor binaries into the repo.
3. **Recipe.** New file `internal/recipe/<name>.go` implementing the interface above; write output under
   `ctx.Output` and keep the common layout (`original/`, `src/`, indexes) where it applies.
4. **Skip-list.** Runtime/engine files that are not game code → `internal/skiplist/builtin.go`.
5. **Tests.** Add a match test (pattern: `TestMatchDelphi` in `internal/recipe/recipe_test.go`) plus unit
   tests for argument building / output layout (see `il2cpp_*_test.go`, `nsis_test.go`). No network or real
   tool binaries in unit tests.
6. **Docs.** Update the recipe table in `README.md`, the recipe list in
   `internal/instructions/instructions.go` (served at `/api/instructions`), and the tools list in
   `README.md` if a tool was added.

## Verify

```powershell
go build ./... ; go vet ./... ; go test ./...
cd frontend; npm run test     # only if frontend files changed
```

Real end-to-end runs: `build.bat` then `test.bat` (sandboxed in `testbed/`), or
`dist\morgue.exe run <target> -o <dir>` and inspect `<dir>\summary.json` + `<dir>\<target>\recon.json`.
