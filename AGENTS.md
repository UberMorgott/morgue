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
   `README.md` if a tool was added.. **Quality.** Map the recipe to a quality family and stream provenance (section below).

## Quality and provenance (how a recipe reports them)

- **Family.** Map the recipe to its collector family in `recipeFamilies`
  (`internal/quality/collector.go`); the engine test `TestEveryRegisteredRecipeHasQualityFamily` fails
  otherwise. Reuse a family (managed, java, native, delphi, ue5, il2cpp, unitydata, container) or add
  `internal/quality/<family>.go` with `func init() { Register("<family>", X{}) }`. A secondary family
  (e.g. unitydata) is merged with checks prefixed `<family>/`.
- **Collector.** `Collect(ctx, Input{Target, Output})` reads only files under `Input.Output` (the target
  dir) and returns `runmodel.QualityReport` that passes `Validate()`. Rules: no evidence → `unknown` with a
  basis (value `null`, never 0); nothing to measure → `not-applicable`; a ratio only for two measured counts
  of one identity universe; fill `names` (`runmodel.NamesNotApplicable` when names come from metadata).
  Stable check IDs: `output.present`, `functions.coverage`, `types.coverage`, `decompiler.errors`,
  `types.unknown-result`, `references.unresolved`, `obfuscation.residual`, `strings.decryption`,
  `provenance.complete`, `artifact.fidelity`. The engine writes `quality.json` and publishes it; never
  write it from the recipe.
- **Evidence.** Persist what the collector needs while executing: raw tool logs (`logs/`), a step sidecar
  (`quality_evidence.json` / `evidence/*.json`) saying ran / skipped / failed and why, per-function status
  files. Parser drift must yield `unknown`, not a measured zero.
- **Provenance.** Write `runmodel.ProvenanceRecord` rows to `ctx.Provenance` (nil in unit tests — guard it)
  while products are generated: one per file/function/rename, with module, artifact path (target-relative)
  and kind (`decompiled-source`/`pseudocode`/`stub`/...), token/RVA/address as hex strings, original and
  recovered name, name source, confidence and `Producer{Tool, Version, StepID, Args}` (version from the run
  tool lock). Leave `TargetID`/`InputSHA256` empty for the current target — the engine fills them. Use the
  frozen step IDs in `docs/schemas/quality-provenance.md`.
- **Artifacts.** Record products via `ctx.Artifacts.Record(path, kind, stepID, tool, shared)` with the
  honest fidelity kind. The engine already records `original/`, `src/`, `dump/`, `indexes/`, `data/`,
  `logs/`, `evidence/`, `provenance.ndjson`, `quality.json`.
## Verify

```powershell
go build ./... ; go vet ./... ; go test ./...
cd frontend; npm run test     # only if frontend files changed
```

Real end-to-end runs: `build.bat` then `test.bat` (sandboxed in `testbed/`), or
`dist\morgue.exe run <target> -o <dir>` and inspect `<dir>\summary.json` + `<dir>\<target>\recon.json`.
