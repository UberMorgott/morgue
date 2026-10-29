# Morgue

Binary decompilation orchestrator for Windows — a hybrid desktop GUI + CLI tool that
auto-detects, deobfuscates, and decompiles binaries into structured, AI-readable
project layouts. Point it at a file or a game folder; Morgue classifies the target,
downloads whatever external tooling it needs, runs the right pipeline, and writes the
result as decompiled source, structured strings, and navigable indexes.

## Hybrid GUI/CLI model

- Run `morgue` with no arguments → launches the desktop GUI (Wails v3 + Svelte SPA,
  system-tray app).
- Run `morgue <command>` → CLI mode (Cobra). `morgue run` works fully standalone, no
  GUI required.
- While the GUI is running it starts an HTTP API on `127.0.0.1:19876`. The
  `morgue api ...` subcommands drive that running instance, so an AI agent (or any
  HTTP client) can queue jobs and the user watches progress live in the window.
- One run at a time: `POST /api/run` answers `202 {"status":"started"}` or
  `409 {"error":"busy"}`; `POST /api/stop` cancels the active run (`409` if idle).
  `morgue api run --wait` exits non-zero when the run fails or is cancelled.
- API auth: on every launch the GUI writes a random token to
  `data\api-token` next to `morgue.exe` (override with `MORGUE_API_TOKEN_FILE`; set it
  to the GUI's file when the CLI is a different copy of morgue). Every endpoint except `GET /api/status` and
  `GET /api/instructions` requires it in `X-Morgue-Token`; POST/PUT bodies must be
  `application/json`; browser requests from non-localhost origins and non-loopback
  `Host` headers are rejected. The `morgue api` commands read the token
  automatically. With curl (PowerShell):

  ```powershell
  $tok = Get-Content "C:\path\to\morgue\data\api-token"
  curl.exe -H "X-Morgue-Token: $tok" -H "Content-Type: application/json" `
    -d '{\"path\":\"C:\\Games\\App.exe\"}' http://127.0.0.1:19876/api/run
  curl.exe -N -H "X-Morgue-Token: $tok" http://127.0.0.1:19876/api/events   # SSE, pings every 20 s
  ```

## Features

- **Auto-detection** — PE parsing (`saferwall/pe`) + Detect It Easy + heuristics
  classify binary kind, compiler, and obfuscator.
- **On-demand tooling** — decompilers, deobfuscators, and runtimes are downloaded
  automatically on first use; nothing is pre-bundled.
- **Auto runtimes** — required .NET and Java runtimes are fetched and managed locally
  when a recipe needs them.
- **ConfuserEx handling** — detects ConfuserEx (including marker-stripped variants) and
  deobfuscates via de4dot, plus built-on-demand passes for what de4dot leaves behind:
  custom resource-keyed string decryption on the host assembly and embedded children
  (`cfxstrings`), dynamic embedded-assembly extraction (`cfxextract`, gated behind
  `--allow-dynamic`), and opt-in control-flow deobfuscation of constant-provider switch
  flattening (`cfxcflow`, enabled with `--cflow`).
- **AI-optimized output** — per-target layout with decompiled `src/`, structured
  `strings.json`, `recon.json`, and call-graph/name-resolution indexes designed for
  LLM consumption.
- **Memory-safe** — the process installs a Windows Job Object memory cap
  (min of 4 GiB / 75% of physical RAM) so a runaway decompile fails instead of
  thrashing the machine.

## Supported targets (recipes)

Recipes are matched automatically from the recon result; you can also force one with
`--recipe <name>`.

| Recipe | Target | Toolchain |
|--------|--------|-----------|
| `dotnet-confuserex` | Obfuscated .NET assemblies (ConfuserEx) | de4dot-cex + ilspycmd |
| `dotnet-generic` | Clean .NET managed assemblies | ilspycmd |
| `unity-mono` | Unity Mono builds (code + game data) | ilspycmd + AssetRipper (AssetStudioMod optional) |
| `unity-il2cpp` | Unity IL2CPP builds | Il2CppInspectorRedux (Il2CppDumper fallback) + ilspycmd + AssetRipper |
| `native` | Native PE binaries | Ghidra (headless; NativeAOT extension when detected) |
| `delphi` | Delphi binaries | IDR + Ghidra |
| `ue5` | Unreal Engine 5 games | retoc (PAK/IoStore), `.usmap` SDK dump |
| `java` | `.jar` / `.war` / `.class` | Vineflower (Java runtime auto-installed) |
| `nsis` | Nullsoft (NSIS) installers | built-in unpacker; extracted files are processed recursively |

HashLink/Haxe targets are not supported yet (see [docs/ROADMAP.md](docs/ROADMAP.md)).

## Managed tools

Morgue downloads and version-checks these external tools on demand (GitHub releases,
NuGet, or direct URLs). Inspect and manage them with `morgue tools check` /
`morgue tools install`.

- **diec** — Detect It Easy (console detector)
- **ilspycmd** — ILSpy command-line .NET decompiler (run via .NET)
- **de4dot-cex** — de4dot fork for ConfuserEx
- **ghidra** — NSA Ghidra (native decompilation, headless). Needs the JDK version its `application.java.min` declares (fails clearly otherwise). Resource caps: JVM heap `ghidra_max_mem_gb` (default min(8, 25% RAM), at least 2) and analysis threads `ghidra_max_cpu` (default half the CPUs) in `morgue.yaml`; `GHIDRA_HEADLESS_MAXMEM` env overrides the heap. Output is labelled pseudocode, never original source.
- **ghidra-nativeaot** — Ghidra extension for .NET NativeAOT metadata recovery
- **il2cppinspector** — Il2CppInspectorRedux (IL2CPP metadata v16–v106)
- **il2cppdumper** — Unity IL2CPP metadata extractor (fallback)
- **assetripper** — Unity asset / ScriptableObject extractor (headless)
- **assetstudiomod** — Unity asset inventory (optional)
- **idr** — Interactive Delphi Reconstructor
- **retoc** — Unreal Engine PAK/IoStore extractor
- **vineflower** — Java bytecode decompiler
- **strings** — Sysinternals Strings
- **goresym**, **nofuserex**, **confuserex-killer**, **proxycall-remover** — installable
  via `morgue tools install`, currently not used by any recipe

### Updating tools

```text
morgue tools check --updates           Query upstream; adds LATEST and UPDATE columns
morgue tools install <name> --force    Reinstall/update one tool
morgue tools install --force           Reinstall/update every tool
```

- Reinstalls are staged: the new copy is downloaded and validated next to the old one
  and swapped in only on success, so a failed update keeps the working tool.
  `tools install` exits non-zero if any tool failed.
- Pinned tools always install their pinned version; `--updates` flags them only when
  the installed version differs from the pin. Tools without a version (e.g. `strings`)
  are compared by the download's Last-Modified date.
- One update policy for GUI, CLI and API: there is no startup or registry-wide
  update. Before a run first uses a managed tool (detector tools in analysis
  included), Morgue checks it once against upstream (15 s timeout) and updates it if
  a newer release exists. The run then holds that exact payload: its identity
  (version, payload SHA-256, runtime) is frozen in `<output>/.morgue/tools.lock.json`.
  Offline or on a failed check a working installed tool is kept and the run logs a
  freshness warning naming the tool; a missing required tool fails.
- Pin (operator override / rollback): `morgue tools install <name> --version X`
  installs exactly X and shows the tool as pinned; runs keep using X instead of
  updating. `morgue tools install <name>` without `--version` clears the pin and
  resumes automatic updates once the install succeeds (a failure keeps the pin).
  `--version` works for GitHub-release and NuGet tools; unversioned direct-URL tools
  reject it before changing anything.
- A tool in use by an active run is never swapped or deleted: install/delete is
  refused (CLI error, GUI message, HTTP `409 {"error":"in use"}`); retry after the run.
- `prefer_system_tools: true` (default `false`) is the only way to use system installs:
  a tool found on `PATH` (e.g. a global `dotnet tool install -g ilspycmd`) instead of
  the managed copy, and system runtimes (`java`/`JAVA_HOME`, `dotnet`, `GHIDRA_HOME`).
  Such tools show version `system` and are never updated by Morgue.

## Install

Windows only (amd64 and arm64). Download the latest release from
[GitHub Releases](https://github.com/UberMorgott/morgue/releases):

- `morgue_windows_amd64.zip` (64-bit Intel/AMD)
- `morgue_windows_arm64.zip` (Windows on ARM)

Unzip and run `morgue.exe`. On first launch the GUI installs WebView2 if it is missing
(system-wide or portable). External tools and runtimes download automatically when a
job needs them.

Morgue is portable: it keeps everything in its own folder and must be able to write
there (it refuses to start otherwise). Tools and runtimes go to `tools\`, the GUI's
WebView2 profile to `.webview2\`, and runtime state to `data\`: `data\tmp\p<pid>`
(TEMP/TMP for morgue and every tool it spawns, removed on exit, stale ones swept on
start), `data\home` (`DOTNET_CLI_HOME`), `data\config` / `data\cache` (XDG dirs —
Ghidra's settings and cache), NuGet caches and .NET bundle extraction under
`data\cache`, and `data\api-token`. .NET telemetry/first-run are disabled for spawned
tools. The Sysinternals `strings` EULA key (`HKCU\Software\Sysinternals\Strings`)
cannot be redirected; morgue deletes it after use unless it already existed.
Choosing the **system-wide** WebView2 install in the first-launch dialog installs
Microsoft's runtime machine-wide — pick **portable** to keep it in the folder.

Every runtime and tool runs from Morgue's folder: the Java JDK (`tools\runtimes\java`,
the feature release the installed Ghidra declares in `application.java.min`, at least 25),
the .NET runtime/SDK and every decompiler. A system Java on `PATH`/`JAVA_HOME`, a system
`dotnet` SDK (also for building embedded helpers) and `GHIDRA_HOME` are ignored; a
missing managed runtime is installed automatically, otherwise the run fails with a clear
error. Spawned tools do not inherit `JAVA_HOME`, `JAVA_TOOL_OPTIONS`, `DOTNET_ROOT`,
`PYTHONHOME`/`PYTHONPATH` or `PATH` directories holding a system `java`/`dotnet`/`python`.
`prefer_system_tools: true` opts back into system tools and runtimes; an explicit
`ghidra_home` in `morgue.yaml` is always honored.

There are no Linux or macOS builds — the GUI backend depends on Windows-only WebView2
and Win32 APIs.

## Usage

```text
morgue                                 Launch the desktop GUI (hybrid mode)

morgue info <file>                     Classify a target without decompiling
morgue info <file> --format text       Human-readable classification (default: json)

morgue run <target>                    Decompile a file or directory
  -o, --output <dir>                   Output directory (default: <binary dir>/output)
  -q, --quiet                          Suppress stderr; emit only JSON to stdout
      --watch                          Show TUI progress on stderr (interactive only)
      --force-watch                    Force the TUI even when stderr is not a terminal
      --recipe <name>                  Force a specific recipe
      --no-skip                        Disable the auto skip-list (process everything)
      --exclude <a,b>                  Additional exclude patterns
      --ref <dir>                      Dependency search dir for .NET decompilation
                                       (repeatable)
      --allow-dynamic                  Allow recipe steps that EXECUTE target code
                                       (e.g. ConfuserEx embedded-assembly extraction)
      --cflow                          Enable control-flow deobfuscation pass
                                       (experimental, off by default)
      --code-only                      Skip Unity asset extraction (AssetRipper);
                                       decompiled code only. Persistent equivalent:
                                       unity_extract_assets: false in morgue.yaml
                                       (Settings -> Unity in the GUI)
      --gamedata-out <dir>             Organized game-data tree destination
                                       (unity-mono; default <output>/GameData)
      --tools-mirror <host>            Rewrite tool download hosts to a mirror

morgue gamedata <exportDir> -o <dir>   Organize an existing AssetRipper Unity export
  [--inventory <csv>] [--full <dir>]   into a game-data tree

morgue tools check [--updates]         List managed tools and their install status
morgue tools install [name] [--force] [--version X]  Install all missing tools, or one (pin with --version)

morgue self-update [--check]           Update the binary to the latest release
morgue version                         Print version and commit

# Control a running GUI instance over its HTTP API:
morgue api status                      Check whether the GUI is running
morgue api stop                        Cancel the GUI's active run
morgue api run <path> [-o <dir>] [--wait]   Queue a job (shown live in the GUI)
morgue api tools [--wait]              List tools via the GUI
morgue api tools install [name] [--version X]  Install tool(s) via the GUI (409 while in use)
morgue api tools delete <name>         Remove a tool via the GUI
morgue api settings                    Show settings
morgue api settings set <key> <value>  Update a setting
```

## Output layout

A run writes a self-describing workspace: one subdirectory per target plus run-level
manifest, journal and navigation files.

```text
<output>/
├── AGENTS.md                   # how to read this output (read order, fidelity, quality)
├── INDEX.md                    # every target: outcome, quality, failures/skips
├── morgue.json                 # manifest (docs/schemas/morgue-v1.schema.json)
├── summary.json                # legacy v0.7 summary
├── .morgue/
│   ├── journal.ndjson          # ordered events v2 (crash-safe; `morgue inspect <output>`)
│   └── tools.lock.json         # exact tool/runtime identities (version, payload SHA-256)
└── <target-name>/
    ├── INDEX.md                # entry candidates, indexes, quality, reading suggestions
    ├── quality.json            # measured quality report (docs/schemas/quality-v1.schema.json)
    ├── provenance.ndjson       # one row per generated file/function/rename
    ├── recon.json              # classification (kind, compiler, obfuscator, recipe)
    ├── original/               # preserved copy of the input binary
    ├── src/                    # decompiled source / pseudocode / stubs
    │   └── indexes/            # call-graph / duplicate / name-resolution indexes
    ├── logs/  evidence/        # raw tool logs and recipe evidence the collectors read
    └── data/                   # extracted game data (Unity IL2CPP layout)
```

The exact contents under `src/` and the indexes vary by recipe.

### Fidelity labels

Every artifact in `morgue.json` has a `kind`: `decompiled-source` (ILSpy/Vineflower —
readable, not original source), `pseudocode` (Ghidra — approximate control flow),
`stub` (IL2CPP DummyDll C# — signatures only, no bodies), `metadata`, `asset`,
`index`, `log`, `original`, `container`. No output is original source code, whatever
its extension.

### Quality report

Each finished target gets `quality.json` (also `targets[].quality` in `morgue.json`)
from its recipe's family collector (managed, java, native, delphi, ue5, il2cpp,
unitydata, container). Status `pass` / `degraded` / `unknown` / `not-applicable`;
there is no weighted score. Every metric states `measured`, `estimated`, `unknown`
or `not-applicable` plus its basis — `unknown` is `null`, never 0, and a coverage
ratio exists only for two measured counts of the same identity universe (e.g. Ghidra
functions found vs emitted). `names` reports the share of tool-default names
(Ghidra `FUN_<addr>`) and how many were renamed by inference. Unity targets merge the
asset-export section (checks prefixed `unitydata/`); the IL2CPP companion
`global-metadata.dat` and NSIS containers are `not-applicable`. A skipped, cancelled
or interrupted target has `quality: null` (not collected).

### Provenance

`<target>/provenance.ndjson` (`targets[].provenance`) streams one JSON record per
generated file, function or rename (docs/schemas/provenance-v1.schema.json): module,
input SHA-256, token/RVA/address as hex strings, original and recovered name, name
source (`metadata`, `export`, `single-string-candidate`, ...), confidence and the
producing tool/version/step. Stream it; do not load it whole.

## Build from source

Requires Go 1.27.3 and Node 20.19+ / 22.12+. The frontend is embedded into the Go binary at compile
time via `//go:embed`, so it must be built first.

```bat
:: Full production build → dist\morgue.exe
::   regenerates Wails bindings, builds the Svelte SPA, embeds the icon, links the EXE
build.bat
```

Or with [Task](https://taskfile.dev):

```bash
task dev        # Wails dev mode (Vite + Go backend, hot reload; needs wails3 in PATH)
task build      # build frontend + morgue.exe with version/commit ldflags
task frontend   # npm install + npm run build
task bindings   # regenerate Wails bindings from Go services
```

## Releases

Releases are produced by GoReleaser on a Windows GitHub Actions runner. Pushing a `v*`
tag builds the frontend, then runs `goreleaser release --clean` to publish the
Windows amd64 + arm64 zip archives and checksums. See
[GitHub Releases](https://github.com/UberMorgott/morgue/releases) for the latest version.

## Stack

- **Backend:** Go 1.27.3, Wails v3.0.0, Cobra CLI
- **Frontend:** Svelte 5 (runes), TypeScript 6, Vite 8, custom CSS (no framework)
- **Desktop:** Wails 3 — Go backend + embedded Svelte SPA, WebView2, system tray
- **Release:** GoReleaser (Windows-only, amd64 + arm64)

## License

Non-commercial research license (`LicenseRef-Morgue-NC-Research`). See
[LICENSE](LICENSE) for the full terms.
