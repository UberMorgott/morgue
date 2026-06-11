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

## Features

- **Auto-detection** — PE parsing (`saferwall/pe`) + Detect It Easy + heuristics
  classify binary kind, compiler, and obfuscator.
- **On-demand tooling** — decompilers, deobfuscators, and runtimes are downloaded
  automatically on first use; nothing is pre-bundled.
- **Auto runtimes** — required .NET and Java runtimes are fetched and managed locally
  when a recipe needs them.
- **ConfuserEx handling** — detects ConfuserEx (including marker-stripped variants) and
  deobfuscates via de4dot before decompiling.
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
| `dotnet-confuserex` | Obfuscated .NET assemblies (ConfuserEx) | de4dot + ilspycmd |
| `dotnet-generic` | Clean .NET managed assemblies | ilspycmd |
| `unity-mono` | Unity Mono builds | ilspycmd |
| `unity-il2cpp` | Unity IL2CPP builds | Il2CppDumper + ilspycmd |
| `native` | Native PE binaries | Ghidra (headless) |
| `delphi` | Delphi binaries | IDR + Ghidra |
| `ue5` | Unreal Engine 5 games | retoc (PAK/IoStore), `.usmap` SDK dump |

## Managed tools

Morgue downloads and version-checks these external tools on demand (GitHub releases,
NuGet, or direct URLs). Inspect and manage them with `morgue tools check` /
`morgue tools install`.

- **diec** — Detect It Easy (console detector)
- **ilspycmd** — ILSpy command-line .NET decompiler (run via .NET)
- **de4dot-cex** — de4dot fork for ConfuserEx
- **ghidra** — NSA Ghidra (native decompilation, headless)
- **il2cppdumper** — Unity IL2CPP metadata extractor
- **idr** — Interactive Delphi Reconstructor
- **retoc** — Unreal Engine PAK/IoStore extractor
- **goresym** — Go symbol/type parser
- **strings** — Sysinternals Strings
- **nofuserex**, **confuserex-killer**, **proxycall-remover** — ConfuserEx
  anti-tamper / unpacking helpers

## Install

Windows only (amd64 and arm64). Download the latest release from
[GitHub Releases](https://github.com/UberMorgott/morgue/releases):

- `morgue_windows_amd64.zip` (64-bit Intel/AMD)
- `morgue_windows_arm64.zip` (Windows on ARM)

Unzip and run `morgue.exe`. On first launch the GUI installs WebView2 if it is missing
(system-wide or portable). External tools and runtimes download automatically when a
job needs them.

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
      --recipe <name>                  Force a specific recipe
      --no-skip                        Disable the auto skip-list (process everything)
      --exclude <a,b>                  Additional exclude patterns
      --allow-dynamic                  Allow recipe steps that EXECUTE target code
                                       (e.g. ConfuserEx embedded-assembly extraction)

morgue tools check                     List managed tools and their install status
morgue tools install [name]           Install all missing tools, or a specific one

morgue self-update [--check]           Update the binary to the latest release
morgue version                         Print version and commit

# Control a running GUI instance over its HTTP API:
morgue api status                      Check whether the GUI is running
morgue api run <path> [-o <dir>] [--wait]   Queue a job (shown live in the GUI)
morgue api tools [--wait]              List tools via the GUI
morgue api tools install [name]        Install tool(s) via the GUI
morgue api tools delete <name>         Remove a tool via the GUI
morgue api settings                    Show settings
morgue api settings set <key> <value>  Update a setting
```

## Output layout

A run writes one subdirectory per target into the output directory, plus a top-level
`summary.json`:

```text
<output>/
├── summary.json                # aggregate run result (targets, success/fail/skipped)
└── <target-name>/
    ├── recon.json              # classification (kind, compiler, obfuscator, recipe)
    ├── original/               # preserved copy of the input binary
    ├── strings.json            # structured, categorized string extraction
    └── src/                    # decompiled source
        └── indexes/            # call-graph / duplicate / name-resolution indexes
```

The exact contents under `src/` and the indexes vary by recipe (e.g. native/UE5 runs
produce navigable call-graph and name-resolution indexes; `.NET` runs produce
decompiled C#).

## Build from source

Requires Go 1.26.4 and Node 20+. The frontend is embedded into the Go binary at compile
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
Windows amd64 + arm64 zip archives and checksums. Latest release: **v0.4.2**.

## Stack

- **Backend:** Go 1.26.4, Wails v3 (alpha), Cobra CLI
- **Frontend:** Svelte 5 (runes), TypeScript 6, Vite 8, custom CSS (no framework)
- **Desktop:** Wails 3 — Go backend + embedded Svelte SPA, WebView2, system tray
- **Release:** GoReleaser (Windows-only, amd64 + arm64)

## License

Non-commercial research license (`LicenseRef-Morgue-NC-Research`). See
[LICENSE](LICENSE) for the full terms.
