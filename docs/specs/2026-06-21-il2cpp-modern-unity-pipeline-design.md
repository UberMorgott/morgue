# Modern Unity IL2CPP Pipeline — Design Spec

- **Date:** 2026-06-21
- **Status:** Approved design → implementation plan
- **Scope:** Make morgue decompile modern Unity IL2CPP games (metadata v39+, Unity 6) end-to-end with zero manual steps, including the data layer (ScriptableObject / Odin config). Add + fall back; do not remove existing behavior.

## 1. Problem & Context (verified)

- Game **Lost Castle 2** (Unity `6000.3.16f1`, IL2CPP, **metadata version 39**) fails in morgue today.
- Exact error from the current dumper (Il2CppDumper v6.7.46): `System.NotSupportedException: ERROR: Metadata file supplied is not a supported version[39].`
- Root cause: morgue's IL2CPP recipe only knows Il2CppDumper, which does not support metadata v39. Go never reads the metadata version, so the pipeline can't detect or route around it.
- A working one-off toolchain was hand-assembled at `D:\LC2-Decompile\` proving the path:
  - **Il2CppInspectorRedux 2026.2 CLI** — supports metadata v16→v106 incl. v39 — produced `dump/cs/il2cpp.cs` (35 MB) + `dump/dll/*.dll` (193 DummyDLLs, game code `LC2.Core.dll` 10.6 MB). Needs .NET 10 / ASP.NET Core runtime (installed per-user at `D:\LC2-Decompile\tools\dotnet-aspnet10`).
  - **AssetRipper 1.3.14** — extracted ScriptableObject `.asset` config from UnityFS bundles.
  - **Custom Odin decoder** (`D:\LC2-Decompile\odindec\`, C# net8.0) — decoded Sirenix Odin binary blobs (config numbers live in DATA, not code). Round-tripped 10/10 files, 0 errors.
- Key IL2CPP limitation: the C# dump gives class/field/enum signatures + RVA offsets, NOT method bodies. Numeric game logic was recoverable only because it lived in Odin ScriptableObject data.

## 2. Goal & Principle

Turn the hand-assembled success into a **permanent, native, first-class** part of morgue — wired the same way as every other capability (tools download on-demand via `tools.Manager`, runtimes bootstrapped, binary parsers native Go). After a normal morgue update, the user drops a modern Unity game in and it "just works": auto-detect → correct dumper → data extraction → readable config. No separate script, no manual steps.

## 3. Constraints / Non-goals

- **Output disk:** decompile output AND all intermediate/temp files go to the disk with the most free space (= **D:** on this machine). C: and E: have limited space and WILL overflow (LC2 full export was 37 GB + 27 GB). Spawned tools (AssetRipper, dumpers) write large temp to `%TEMP%` (C:) by default → must redirect TEMP/working dir to the output disk. Explicit `--output` override allowed; never default to C: or E:.
- **No admin.** All runtime/tool installs are per-user, self-contained, relative to the toolchain root.
- **Don't break existing games.** Keep Il2CppDumper as a fallback. Old recipes unchanged. Add + fall back, don't rip out.
- **Pin tool versions** in a manifest for reproducible setup.
- Ghidra/IDA native-body hook: stub + document only, do not fully build.

## 4. Architecture — Integration Points (current code, mapped this session)

| Area | File:line | Role / change |
|------|-----------|---------------|
| Tool catalog | `internal/tools/registry.go` (existing `il2cppdumper` ~112) | Append `il2cppinspector`, `assetripper` ToolDefs |
| ToolDef shape | `internal/tools/types.go:57` | Add a pinned-version field (tag/version + lock) |
| Tool install | `internal/tools/manager.go:130` (`Install`) | Honor pinned version; resolve assets |
| Runtime bootstrap | `internal/tools/runtimes.go:189` (`InstallRuntime`), `:241` (`installDotnetSDK`) | Add `.NET 10 ASP.NET runtime` kind (mirror existing dotnet/java pattern → `baseDir/runtimes/`) |
| Runtime trigger | `internal/engine/pipeline.go:314` (`ensureRuntimeDeps`) | Ensure ASP.NET runtime before InspectorRedux runs |
| IL2CPP detection | `internal/scanner/grouper.go:65` (`findUnityIL2CPP`) | Unchanged (already groups GameAssembly.dll + global-metadata.dat) |
| IL2CPP recipe | `internal/recipe/il2cpp.go` (`Execute`, `findGlobalMetadata:338`) | Read metadata version; version-aware dumper selection + fallback; add data-layer steps |
| Tool exec | `internal/util/exec.go:70` (`runCmdStreaming`) | Reuse for new tool spawns; set TEMP/cwd to output disk |
| New parser | `internal/odin/` (new pkg) | Native Go Odin BinaryDataReader decoder |
| New data step | `internal/recipe/` (extend IL2CPP recipe / helper) | AssetRipper headless export + Odin decode |

morgue's recipe registry is **first-match-wins, no fallback-chain** — so the dumper fallback lives *inside* `IL2CPP.Execute()`, not as competing recipes.

## 5. Components

### 5.1 Metadata version detection (native Go)
- New helper (in `internal/recon` or alongside the recipe) reads `global-metadata.dat` header: magic `0xFAB11BAF` (LE) at offset 0, **`int32` version at offset 4**. Validate magic; surface version into recon results / recipe context.
- Best-effort Unity version detection where cheaply available (e.g. from `globalgamemanagers` / asset bundle header) — optional, version number alone drives routing.

### 5.2 Version-aware dumper selection + fallback chain (in `IL2CPP.Execute`)
- **Default → Il2CppInspectorRedux** (supports v16–24.5, 27–29.1, 31, 35, 38, **39**, 104–106). Covers modern + most legacy.
- **Fallback → Il2CppDumper** (kept installed) when InspectorRedux is unavailable or exits non-zero, or for any version it demonstrably handles better.
- InspectorRedux 2026.x CLI (verified against the LC2 regression run): `Il2CppInspector.Redux.CLI.exe process <GameAssembly.dll> <global-metadata.dat> -o <dumpRoot> -s -d`. The `process` subcommand takes the binary + metadata as **positional** input paths (auto-detected in any order), `-o` is the single output **folder**, and `-s` (`--output-csharp-stub`) / `-d` (`--output-dummy-dlls`) are **bare switches** selecting the C# stub + DummyDLL outputs only (no Python/C++/JSON). InspectorRedux writes `<dumpRoot>/cs/il2cpp.cs` + `<dumpRoot>/dll/*.dll`, so `<dumpRoot>` is set to `<out>/dump` and the `cs/`+`dll/` subdirs land exactly on the structured `dump/cs`,`dump/dll` layout. (The older `-i/-m/--select-outputs/-c` form does NOT exist in this release.) Requires `DOTNET_ROOT` to point at a real **.NET 10** ASP.NET Core runtime — a system dotnet < 10 is rejected so the bundled `runtimes/dotnet-aspnet10` is used.
- On unsupported version from BOTH tools: print detected version + which tool(s) were tried + the exact tool error. Never fail silently.

### 5.3 Tool registry + version pinning
- Add ToolDefs: `il2cppinspector` (GitHub release `LukeFZ/Il2CppInspectorRedux`, win-x64 CLI asset), `assetripper` (GitHub release `AssetRipper/AssetRipper`, win-x64 Free build — self-contained, no runtime dep).
- Add a **pinned version/tag** to ToolDef + a manifest lock (e.g. `tools.lock.json` or fields in registry) so installs are reproducible. Current registry always pulls latest; pinning is a new capability requested by spec.

### 5.4 Runtime bootstrap — .NET 10 ASP.NET (no admin)
- New `RuntimeKind` (e.g. `RuntimeAspNet`) mirroring `installDotnetSDK`/`installJavaJRE`: download `aspnetcore-runtime-win-x64` (.NET 10) from `aka.ms/dotnet/...` → extract to `baseDir/runtimes/dotnet-aspnet10/`. Set `DOTNET_ROOT` for the InspectorRedux spawn. (Reproduces the known-good `D:\LC2-Decompile\tools\dotnet-aspnet10`; ASP.NET bundle is a superset of the base runtime → de-risks.)
- InspectorRedux is `--no-self-contained` net10.0 → needs this runtime. AssetRipper Free is self-contained → needs nothing.

### 5.5 Data layer — AssetRipper headless (config-only)
- AssetRipper Free GUI runs headless as an ASP.NET web app: `AssetRipper.GUI.Free.exe --headless --port <p>`, then HTTP POST (form-urlencoded):
  - `POST /LoadFolder` field `Path=<game>_Data`
  - optional `POST /Settings/Update` (script export mode etc.)
  - `POST /Export/PrimaryContent` field `Path=<out>/data` — **PrimaryContent = scripts / ScriptableObject / TextAsset only; skips textures/meshes/audio/video** (this is the selective "config/data only" mode; avoids the 37 GB dump).
  - `POST /Reset` then shut the process down.
- A small native Go HTTP driver orchestrates load→export→reset on the pinned local port. No granular per-type filter exists in Free → PrimaryContent is the selective mode; **full export is opt-in** via a flag (`/Export/UnityProject`).

### 5.6 Native Odin decoder (`internal/odin`)
- Port the proven C# `odindec` (Sirenix Odin `BinaryDataReader` format: node/array/int/float/string/dict entries) to a native Go package — consistent with morgue's existing native binary parsers (e.g. the `.uasset` FName parser).
- Input: AssetRipper-exported MonoBehaviour/.asset files containing `SerializedBytes:` hex blobs. Output: structured readable text trees into `<out>/data`.
- **Verification:** round-trip against the golden reference `D:\LC2-Decompile\odindec\out.txt` (the proven 10/10, 0-error output) — Go output must match the C# output for the LC2 config files.

### 5.7 Output layout, disk selection, temp redirect
- Output root chosen as: explicit `--output` if given, else the **fixed disk** = disk with most free space (D:). Helper enumerates drives, picks max free, refuses C:/E: as default.
- Structure:
  ```
  <out>/<game>/
    dump/cs/   il2cpp.cs
    dump/dll/  *.dll (DummyDLLs)
    data/      decoded Odin config + extracted ScriptableObjects
    log/       run log
  ```
- Set `TEMP`/`TMP` + working dir of spawned tools to `<out>/.tmp` so no large temp lands on C:.

### 5.8 Single entry point
- Reuse existing morgue CLI/GUI. Input = game install path (auto-locates `GameAssembly.dll` + `*_Data/global-metadata.dat`) OR explicit pair. Output = the structured folder above.
- **Idempotent / resumable:** skip stages whose outputs already exist (unless `--force`). Clear per-stage logging.

### 5.9 Ghidra hook (stub + doc only)
- Optional path: feed `GameAssembly.dll` + the dumped RVA map into Ghidra (morgue already integrates Ghidra) for cases needing actual method BODIES. Provide a documented stub/flag; do not fully build.

## 6. Error handling
- Missing runtime → bootstrap; if download fails, report exact URL + fall back to any usable local dotnet.
- Dumper unsupported version → try fallback, then emit detected version + tool errors (no silent fail).
- AssetRipper headless port busy → pick next free port; load/export HTTP non-200 → capture body into log.
- Odin blob it can't parse → log file + offset, continue with the rest (don't abort the whole data stage).

## 7. Testing & Regression (LC2)
- Unit: Odin Go decoder vs `D:\LC2-Decompile\odindec\out.txt` golden (byte/line-level compare on the LC2 config set).
- Unit: metadata-version reader returns 39 for the LC2 `global-metadata.dat`.
- **End-to-end regression:** run the full pipeline against `D:\Steam\steamapps\common\Lost Castle 2\LostCastle2_Data`, output to D:. Confirm it reproduces: `il2cpp.cs`, `LC2.Core.dll` (in DummyDLLs), and the decoded gem/inscription config sheets — config-only (no 37 GB), zero manual steps, exit 0.

## 8. Open gaps / risks
- Metadata versions still unsupported by BOTH dumpers (>v106 future Unity) → reported, not handled.
- Native-logic limit: dump gives signatures + RVA, not method bodies — Ghidra hook is the (stubbed) escape hatch.
- AssetRipper Free has no per-type filter finer than PrimaryContent; if a future game stores config in an excluded type, revisit.
- Odin format drift across Sirenix versions: the Go port targets the LC2-observed format; new variants may need extension (logged, not crashed).
