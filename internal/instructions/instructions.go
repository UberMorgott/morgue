// Package instructions holds the AI control instructions text as a
// compile-time constant. It is a neutral leaf package so that both the
// HTTP API handler and the Wails service can reference it without import
// cycles.
package instructions

// Text contains Markdown instructions for AI agents
// that control Morgue via the CLI/API.
const Text = `# Morgue — AI Control Instructions

You are controlling Morgue, a universal binary decompilation tool.
The GUI is running and the user can see your actions in real-time.

## CLI Commands (terminal)

### Quick Inspect (no decompile)
  morgue info <file>                    — binary classification (kind, compiler, obfuscator, recipe)
  morgue info <file> --format text      — human-readable format

### Decompile
  morgue run <target>                   — decompile file or directory
  morgue run <target> -o <dir>          — custom output directory
  morgue run <target> -q                — quiet mode (JSON only, no stderr)
  morgue run <target> --watch           — TUI progress display
  morgue run <target> --recipe <name>   — force specific recipe
  morgue run <target> --no-skip         — process all files (skip nothing)
  morgue run <target> --exclude a,b     — additional exclude patterns
  morgue run <target> --ref <dir>       — .NET dependency directory (repeat for multiple directories)

### Tools Management
  morgue tools check                    — list all tools with status
  morgue tools install                  — install all missing tools
  morgue tools install <name>           — install specific tool (clears a pin)
  morgue tools install <name> --version X — install exactly X and pin it

Tool update policy (GUI, CLI and API identical): no startup update. Each run
checks every managed tool it uses once before first use and updates it when a
newer release exists, then freezes the exact identities (version, payload
SHA-256, runtime) in <output>/.morgue/tools.lock.json. Offline or on a failed
check, a working installed tool is kept and a warn log naming the tool is
emitted; a missing required tool fails the run. A pin (--version X) is honored
by every run until a plain install of that tool succeeds. A tool leased by an
active run is never swapped or deleted: install/delete is refused (HTTP 409
{"error":"in use"}); retry after the run.

### Control Running GUI (API mode)
  morgue api status                     — check if GUI is running
  morgue api run <file>                 — decompile (visible in GUI window)
  morgue api run <file> -o <dir>        — with custom output
  morgue api run <file> --wait          — decompile and wait for completion
  morgue api stop                       — request cancellation of the active GUI run
  morgue api tools                      — list tools via GUI
  morgue api tools --wait               — wait for tool operations
  morgue api tools install              — install all missing tools via GUI
  morgue api tools install <name>       — install specific tool via GUI (clears a pin)
  morgue api tools install <name> --version X — install exactly X via GUI and pin it
  morgue api tools delete <name>        — remove tool via GUI
  morgue api settings                   — get all settings
  morgue api settings set <Key> <Value> — change a setting

## HTTP API (localhost:19876)

The GUI generates a fresh per-launch token at <morgue dir>\data\api-token (next
to morgue.exe; or the path in MORGUE_API_TOKEN_FILE when set). The CLI reads
this file and sends X-Morgue-Token automatically when it is the same morgue.exe
as the GUI; for another copy set MORGUE_API_TOKEN_FILE to the GUI's file. Direct HTTP clients must send the
current token in X-Morgue-Token; do not print or share the token. GET /api/status
and GET /api/instructions are token-exempt. POST/PUT require Content-Type:
application/json, including POST /api/stop with an empty JSON object {}.

  GET  /api/status              — {"port":"19876","running":true}
  GET  /api/info?path=<file>    — binary classification JSON
  POST /api/run                 — start decompilation (GUI shows progress)
  POST /api/stop                — request cancellation of the active pipeline
  GET  /api/run/status          — pipeline progress (phase, step, files)
  GET  /api/tools               — tool list with install status
  GET  /api/tools?wait=30       — long-poll until tool state changes (max 120s)
  POST /api/tools/install       — install tool {"name":"..."} or all {"name":""};
                                  {"name":"...","version":"X"} pins X, without
                                  version clears the pin; 409 {"error":"in use"}
                                  while an active run uses the tool
  POST /api/tools/delete        — remove tool {"name":"..."}
  GET  /api/settings            — all settings JSON
  PUT  /api/settings            — update settings {"key":"...","value":"..."}
  GET  /api/events              — SSE event stream (real-time progress)
  GET  /api/instructions        — this text

POST /api/run and POST /api/tools/install return 202 Accepted when admitted;
202 means the operation started, not that it completed. Concurrent submissions
return 409 Conflict with {"error":"busy"}. POST /api/stop returns 202 Accepted
with {"status":"stopping"}, or 409 Conflict with {"error":"not running"} when
idle. Poll GET /api/run/status until running=false to observe the final phase
and error; cancellation is asynchronous. Missing/invalid tokens return 401.

--ref applies to the standalone morgue run command; repeat it for each .NET
dependency directory, for example --ref <game_Data/Managed> --ref <BepInEx/core>.
The original assembly directory and extracted sibling DLL directories are added
automatically. The HTTP /api/run request does not currently accept --ref paths.

### Tool Status Response

GET /api/tools returns enriched status:
  {"tools":[{"Name":"diec","Installed":true,"Version":"3.09"},
            {"Name":"ilspycmd","Installed":false,"installing":true,
             "progress":45,"lastActivity":"..."}],
   "busy":true}

Fields: installing (bool), progress (0-100), lastActivity (timestamp), busy (any op running).
If installing=true but lastActivity > 2 min ago, the operation is likely hung.

## Supported Recipes
  dotnet-generic      — .NET managed assemblies (ilspycmd)
  dotnet-confuserex   — ConfuserEx obfuscated .NET (de4dot + ilspycmd)
  unity-mono          — Unity Mono game DLLs (ilspycmd)
  unity-il2cpp        — Unity IL2CPP builds (Il2CppInspectorRedux or Il2CppDumper + ilspycmd)
  native              — native binaries (Ghidra headless)
  delphi              — Delphi binaries (IDR + Ghidra)
  ue5                 — Unreal Engine 5 (PAK extraction)
  java                — .jar/.war/.class (Vineflower)
  nsis                — NSIS installers (unpack, then each extracted file is processed)

## Recommended AI Workflow
  1. morgue api status              — verify GUI is running
  2. morgue info <file>             — inspect target (determine kind/recipe)
  3. morgue api run <file>          — start decompilation (user sees progress in GUI)
     OR morgue run <file> -q        — headless mode (JSON output)
  4. Read summary.json from output dir for results
  5. Read decompiled source from <output>/<name>/src/

## Notes
- All tools auto-download on first use (ilspycmd, Ghidra, strings, etc.)
- Runtimes (.NET, Java) auto-install when needed
- Default output: <binary dir>/output/  (override with -o/--output)
- GUI must be running for 'morgue api' commands
- 'morgue run' works standalone (no GUI needed)
`
