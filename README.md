# Morgue

Portable binary decompilation tool with desktop GUI. Auto-detects, deobfuscates, and decompiles .NET, Delphi, and Unity binaries into AI-readable project layouts.

## Features

- **Auto-detection** — PE parsing + Detect It Easy + heuristics identify binary type, compiler, and obfuscation
- **4-step pipeline** — Analysis → Tools → Execution → Done, with real-time progress tracking
- **Obfuscation handling** — ConfuserEx auto-deobfuscation via de4dot; unknown obfuscators flagged with GitHub issue link
- **On-demand tools** — decompilers and deobfuscators downloaded automatically when needed
- **Supported targets** — .NET (generic & obfuscated), Delphi, Unity Mono, Unity IL2CPP, native PE
- **AI-optimized output** — structured project layouts designed for LLM consumption
- **Hybrid mode** — GUI exposes HTTP API on `localhost:19876` for CLI control of running instance

## Stack

- **Backend:** Go 1.25, Wails v3, Cobra CLI
- **Frontend:** Svelte 5 (runes), TypeScript, Vite 6, custom CSS (no framework)
- **Desktop:** Wails 3 (Go backend + embedded SPA)

## Usage

```bash
morgue                           # Launch desktop GUI
morgue run ./project -o ./out    # CLI headless
morgue run --watch ./project     # CLI + progress
morgue tools check               # Tool status
morgue tools install              # Download tools
morgue self-update                # Update binary
morgue api status                # Check running GUI status
morgue api run ./project         # Send run command to GUI
```

## Build

```bash
# Full production build (frontend + Go binary → dist/)
build.bat

# Dev mode with hot reload
task dev
```

## License

Non-commercial research license. See [LICENSE](LICENSE) for details.
