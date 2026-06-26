# Design: Unity Mono game-DATA extraction + organizer

**Date:** 2026-06-26
**Status:** Approved design → implementation
**Branch:** `feat/unity-mono-gamedata`

## Problem

The `unity-mono` recipe only decompiles managed DLLs (ilspycmd) and extracts
strings. It produces no serialized **DATA**: ScriptableObject "Defs" (BaseDef
subclasses), `InputMapDef`, prefab/MonoBehaviour serialized field values, or
localization. Modders (e.g. Phoenix Point camera/input mods) need all game
values in a readable, greppable text tree, complementing the existing C# code
decompile.

Phoenix Point is Unity **Mono** 2019.4.31f1. Its defs are **Odin-serialized**:
the exported `.asset` YAML carries a binary Odin blob in
`serializationData.SerializedBytes` (`SerializedFormat: 0`), **not** named YAML
fields. AssetRipper exports the blob verbatim (it does not understand Odin), so
named-field output requires decoding the Odin blob — which Morgue's existing
`internal/odin` package already does (it powers the IL2CPP recipe's Step 4).

## Goals

Extend Morgue so `unity-mono` produces a deterministic, UTF-8, greppable text
tree of all game DATA, organized by type, with an index. Reusable for any Unity
Mono game; validated against Phoenix Point.

Output tree (at the path given by `--gamedata-out`, e.g.
`E:\DEV\PhoenixPoint\extracted\GameData\`):

```
GameData/
  defs/<TypeName>/<DefName>.json      # one file per BaseDef instance, named fields
  input/inputmap.md                   # human-readable Action→Chords→Keys table
  input/inputmap.json                 # raw decoded InputMapDef
  prefabs/<name>.json                 # GameObject hierarchy + named MonoBehaviour fields
  loc/<lang>.json                     # key→string per language
  inventory/assets.csv                # Name,Type,Container,PathID,SourceBundle
  INDEX.md                            # tree layout + per-type counts + versions
  manifest.json                       # machine-readable counts/versions/date
  README.md                           # where each kind of data lives
```

## Non-goals

- Exporting binary assets (textures/audio/meshes) **into** the text tree. The
  full Unity export is a prunable working dir; only generated text is delivered.
- Re-decompiling C# code (already done by ilspycmd step / existing decompile).
- Scene-by-scene dumps in v1 (best-effort only if trivially available).

## Decisions (locked)

1. **Export mode = full UnityProject** (`full=true`). Guarantees prefabs +
   scenes + ScriptableObjects + TextAssets are present (PrimaryContent may omit
   prefabs, which would break the camera smoke test). The raw export is heavy
   (textures/audio land as files) but lives in a working dir and never enters the
   text tree.
2. **Inventory = full, via AssetStudioMod CLI.** Produces Name, Type, Container,
   PathID, source bundle — which AssetRipper's file export cannot. Requires
   adding `assetstudiomod` to the tool registry and running it in
   inventory/dump-info mode (no binary export).
3. **Output location = `--gamedata-out` CLI flag.** Plumbed through
   `recipe.Context`; organizer writes the tree directly to the given path. Falls
   back to `ctx.Output/GameData` when the flag is absent.

## Architecture

Three layers, all inside Morgue.

### A. `internal/odin` — add structured decode

Today: `DecodeAssetFile(path) (text string, blobLen int, err error)` renders a
human text tree. Add a structured path that returns Go values for JSON:

- `DecodeAssetValue(path string) (value any, meta AssetMeta, err error)` —
  parses the `.asset` YAML envelope for `m_Name`, `m_Script {guid,fileID,type}`,
  and the `SerializedBytes` hex blob, then walks the **same** Odin token stream
  into `map[string]any` / `[]any` / scalars. `AssetMeta` carries `Name`, `Guid`,
  `FileID`, `OdinFormat`.
- External Odin references (opcodes `0x0B–0x0E`, `0x32–0x33`) become
  `{"$ref": "<guid|pathID>"}` placeholders for later name resolution.
- Reuse the existing binary reader / opcode walk; add only a tree builder
  alongside the existing text printer. Text path untouched.
- If `SerializedBytes` is absent (plain Unity-serialized asset), return the
  YAML `MonoBehaviour:` map directly (handled by the gamedata layer, see below).

### B. `internal/gamedata` — the organizer (new package)

`Organize(opts Options) (Report, error)` reads the raw AssetRipper export +
AssetStudio inventory output and writes the tree. Options: `ExportDir`,
`OutDir`, `InventoryCSV`, `UnityVersion`, `ToolVersions`, progress/log hooks.

- `scripts.go` — build `guid → ClassName` from `Assets/Scripts/**/*.cs.meta`
  (AssetRipper writes one `.meta` per script with its guid). Also `guid → script
  path` for type provenance.
- `defs.go` — walk `Assets/**/*.asset`. For each: resolve `TypeName` via
  `m_Script.guid`, `DefName` via `m_Name`; decode fields via
  `odin.DecodeAssetValue` (Odin) or plain YAML map (non-Odin). Write
  `defs/<TypeName>/<DefName>.json`. Collect `{guid/pathID → DefName}` into a
  global index.
- `refs.go` — second pass: rewrite `{"$ref": ...}` placeholders to
  `{"$ref": "<id>", "$name": "<resolved DefName>"}` using the global index
  (unresolved refs keep `$name: null`).
- `inputmap.go` — find the `InputMapDef` def(s); walk
  `Actions[] → Chords[] → Keys[]` (`InputKey{Name, InputSource, DeadzoneOverride}`),
  map `InputSource {0=Key,1=AxisTriggerPositive,2=AxisTriggerNegative,3=Axis}`,
  include modifier/super chords. Emit `input/inputmap.md` (table) +
  `input/inputmap.json`.
- `prefabs.go` — walk `Assets/**/*.prefab`. Parse GameObject hierarchy + each
  MonoBehaviour's named YAML fields (these are plain Unity serialization, e.g.
  `PlanarScrollCamera.MaxZoomInLimit`). Emit `prefabs/<name>.json`.
- `loc.go` — find localization TextAssets (CSV: `Keys, English, …, Russian, …`).
  Parse with `encoding/csv`; emit `loc/<lang>.json` (key→string) per column.
- `inventory.go` — read AssetStudioMod's exported asset-info dump; normalize to
  `inventory/assets.csv` (Name, Type, Container, PathID, SourceBundle).
- `index.go` — write `INDEX.md`, `manifest.json` (per-type def counts, action
  count, loc key counts, asset counts by type, Unity version, tool+versions,
  source-bundle list, extraction date, failed-type list), and `README.md`.

`Report` returns counts + a list of types/files that failed to deserialize
(non-fatal).

### C. Recipe + tooling wiring

- `internal/recipe/unity_mono.go` — add two steps after the existing four,
  mirroring `il2cpp.go` Steps 3–4:
  - **AssetRipper export:** `ripperPath := ctx.Tools.Resolve("assetripper")`;
    `gameDataDir := monoGameDataDir(ctx.Target)` (parent of `Managed/`, i.e.
    `dir(dir(ctx.Target))`); export to `ctx.Output/raw-export` with `full=true`;
    `.ripped` marker for skip; non-fatal.
  - **Organize game data:** resolve `assetstudiomod`, run inventory dump → temp
    CSV; call `gamedata.Organize(...)` with `OutDir = ctx.GameDataOut` (flag) or
    `ctx.Output/GameData`. Non-fatal per-item; report counts via `ctx.Progress`.
  - `Steps()` gains two `StepInfo` entries; `RequiredTools()` gains
    `"assetripper"`, `"assetstudiomod"`.
- `internal/tools/registry.go` — add `assetstudiomod` ToolDef (GitHub release of
  AssetStudioModCLI, category Extractor, binary `AssetStudioModCLI.exe`).
- `recipe.Context` — add `GameDataOut string`.
- CLI (`internal/cli` + `cmd/morgue/main.go`) — add `--gamedata-out` flag,
  plumb into `Context.GameDataOut`.

## Data flow

```
morgue run <…>/Managed/Assembly-CSharp.dll --recipe unity-mono \
           -o <work> --gamedata-out E:\DEV\PhoenixPoint\extracted\GameData
  recon → Kind=UnityMono → recipe unity-mono
    existing: copy / strings / ilspycmd decompile / build indexes
    NEW AssetRipper export:  <Data> → <work>/.../raw-export   (full Unity project)
    NEW Organize:
      AssetStudioMod inventory dump → temp assets CSV
      gamedata.Organize(raw-export, gamedataOut, inventoryCSV, versions)
        guid→ClassName (.cs.meta)
        defs:  *.asset → odin/plain decode → defs/<Type>/<Name>.json  (+name index)
        refs:  resolve $ref → $name
        input: InputMapDef → inputmap.md + .json
        prefabs: *.prefab → prefabs/<name>.json
        loc:   TextAsset CSV → loc/<lang>.json
        inventory: assets.csv
        INDEX.md + manifest.json + README.md
```

`monoGameDataDir`: for mono, `Assembly-CSharp.dll` is at `<Data>/Managed/…`, so
the Data folder is `filepath.Dir(filepath.Dir(ctx.Target))`. Validate it ends in
`*_Data` / contains `globalgamemanagers`.

## Def file format

```json
{
  "typeName": "WeaponDef",
  "defName": "PR_AR_AssaultRifle_WeaponDef",
  "guid": "f585717f167d85604408057422d30104",
  "fileID": 11400000,
  "odinFormat": "binary",
  "fields": { "...": "named Odin tree (map/array/scalar)" }
}
```

Non-Odin assets: `odinFormat: "none"`, `fields` = the plain YAML `MonoBehaviour`
map. Def→def references: `{"$ref": "<guid|pathID>", "$name": "<DefName|null>"}`.

## Error handling

- AssetRipper, AssetStudio, and organize steps are **non-fatal** (match
  `il2cpp.go` Steps 3–4): a failure logs + reports but never aborts the run.
- `odin` structured decode is `recover`-wrapped per file; a failing def is
  skipped, counted, and listed under "failed types" in `INDEX.md`/`manifest.json`.
- Missing inventory tool → skip inventory, note in report (don't fail the tree).

## Testing

**Unit:**
- `internal/odin`: golden test for `DecodeAssetValue` on
  `testdata/GemComposeAsset.asset` — assert the JSON tree has named fields
  (`mFurnaceCostIron`, nested list/dict) matching the existing text golden.
- `internal/gamedata`: guid→class mapping; InputMap table rendering
  (fixture InputMapDef tree → expected md); loc CSV → JSON; ref resolution.

**Smoke (manual, against real Phoenix Point) — report pass/fail:**
- InputMap: Mouse ScrollWheel (`InputSource=Axis`) → actions "Change Level"
  AND "Scroll Zoom"; Ctrl+Mouse ScrollWheel → overwatch cone; Discrete Zoom
  In/Out = `t`/`g`; Change Level Ascend/Descend = `z`/`c`.
- Camera prefab: `PlanarScrollCamera` `MaxZoomInLimit=10`, `MaxZoomOutLimit=25`,
  `VerticalAngle=45`.
- Report total counts (defs by type, actions, loc keys, assets by type) + any
  types that failed to deserialize.

**Spike first (fail-fast):** before building the full organizer, run a small
AssetRipper export against Phoenix Point and confirm real PP `.asset` files do
carry `serializationData.SerializedBytes` (Odin). If some defs are plain
Unity-serialized, confirm the non-Odin path handles them.

## Deliverables

- Code: `internal/odin` structured decode, `internal/gamedata` package,
  `unity_mono.go` two steps, `assetstudiomod` ToolDef, `--gamedata-out` flag.
- Populated `extracted/GameData/` tree for Phoenix Point with INDEX/README/
  manifest.
- New section in `E:\DEV\PhoenixPoint\docs\research\source-provenance.md`
  (what's here, how produced, how to re-run).
- Short report: counts, Unity version (2019.4.31f1), tool versions, smoke-test
  results, anything that didn't extract.

## Implementation notes / reality corrections (2026-06-26)

Corrections discovered during implementation against real Phoenix Point data:

1. **PP defs are plain Unity-serialized YAML, NOT Odin-serialized.** 0 of 22,031
   MonoBehaviour `.asset` files have `serializationData/SerializedBytes`. All defs
   carry named YAML fields directly; no Odin blob decode was needed for PP. The
   organizer's Odin decode path remains for other games that do use Odin.

2. **AssetRipper PrimaryContent vs UnityProject endpoints.** PrimaryContent
   exports only primary/visual content (meshes, textures, sprites) -- it does NOT
   export scripts, ScriptableObjects, or TextAssets. The full UnityProject
   endpoint is required to get defs/SO/scripts. The original design correctly
   chose UnityProject (`full=true`), but the comment in
   `internal/recipe/assetripper.go` `ExportPrimaryContent` was inverted
   (claimed scripts/SO/TextAsset); corrected.

3. **Localization is I2 Localization.** The real terms live in the game's `.assets`
   bundles as `Resources/I2Languages.asset` (a `LanguageSourceData` containing
   all terms + translations). `LanguageSourcesDef.asset` in the AssetRipper
   export is only a pointer to it, not the data itself. The organizer parses
   `I2Languages` directly.

4. **Camera base-game values.** The design's smoke test listed
   `MaxZoomInLimit=10`, `MaxZoomOutLimit=25` -- those are modded values. Base game
   `GameCameras.prefab` -> `PlanarScrollCamera` has `VerticalAngle=45`,
   `MaxZoomInLimit=5`, `MaxZoomOutLimit=26`.

5. **OOM fix -- defs walk filtering.** Walking all `.asset` files caused OOM on
   large exports (Texture2DArray assets can be hundreds of MB). Fix: filter to
   MonoBehaviour only (Unity class ID 114) via YAML header peek before loading
   the full file. Giant non-MonoBehaviour assets are skipped entirely.

### Final counts

- 22,068 defs across 900 types; 0 deserialization failures.
- 143 input actions (InputMapDef "PhoenixInput").
- 1,141 prefabs.
- 10 localization languages (en/de/es/fr/it/pl/ru/UI_Tester = 12,084 terms; zh-CN = 9,207; zh = 2,878).
- `inventory/assets.csv`: pending (AssetStudioMod run in progress).
