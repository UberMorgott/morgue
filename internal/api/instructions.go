package api

import "net/http"

// AIInstructions contains Markdown instructions for AI agents
// that control Morgue via the CLI/API.
const AIInstructions = `# Morgue — AI Control Instructions

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

### Tools Management
  morgue tools check                    — list all tools with status
  morgue tools install                  — install all missing tools
  morgue tools install <name>           — install specific tool

### Control Running GUI (API mode)
  morgue api status                     — check if GUI is running
  morgue api run <file>                 — decompile (visible in GUI window)
  morgue api run <file> -o <dir>        — with custom output
  morgue api run <file> --wait          — decompile and wait for completion
  morgue api tools                      — list tools via GUI
  morgue api tools --wait               — wait for tool operations
  morgue api tools install              — install all missing tools via GUI
  morgue api tools install <name>       — install specific tool via GUI
  morgue api tools delete <name>        — remove tool via GUI
  morgue api settings                   — get all settings
  morgue api settings set <Key> <Value> — change a setting

## HTTP API (localhost:19876)

  GET  /api/status              — {"port":"19876","running":true}
  GET  /api/info?path=<file>    — binary classification JSON
  POST /api/run                 — queue decompilation (GUI shows progress)
  POST /api/run?direct=true     — start decompilation directly
  GET  /api/run/status          — pipeline progress (phase, step, files)
  GET  /api/tools               — tool list with install status
  GET  /api/tools?wait=30       — long-poll until tool state changes (max 120s)
  POST /api/tools/install       — install tool {"name":"..."} or all {"name":""}
  POST /api/tools/delete        — remove tool {"name":"..."}
  GET  /api/settings            — all settings JSON
  PUT  /api/settings            — update settings {"key":"...","value":"..."}
  GET  /api/events              — SSE event stream (real-time progress)
  GET  /api/instructions        — this text

### Tool Status Response

GET /api/tools returns enriched status:
  {"tools":[{"Name":"diec","Installed":true,"Version":"3.09"},
            {"Name":"nofuserex","Installed":false,"installing":true,
             "progress":45,"lastActivity":"..."}],
   "busy":true}

Fields: installing (bool), progress (0-100), lastActivity (timestamp), busy (any op running).
If installing=true but lastActivity > 2 min ago, the operation is likely hung.

## Supported Recipes
  dotnet-generic      — .NET managed assemblies (ilspycmd)
  dotnet-confuserex   — ConfuserEx obfuscated .NET (de4dot + ilspycmd)
  unity-mono          — Unity Mono game DLLs (ilspycmd)
  unity-il2cpp        — Unity IL2CPP builds (not yet implemented)
  native              — native binaries (Ghidra headless)
  delphi              — Delphi binaries (IDR + Ghidra)
  ue5                 — Unreal Engine 5 (PAK extraction)

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
- Default output: ./decompiled/
- GUI must be running for 'morgue api' commands
- 'morgue run' works standalone (no GUI needed)
`

func (s *Server) handleInstructions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(AIInstructions))
}
