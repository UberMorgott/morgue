# Morgue

Portable binary decompilation tool. Auto-detects, deobfuscates, and decompiles .NET, Delphi, and Unity binaries into AI-readable project layouts.

## Usage

```bash
morgue                           # TUI wizard
morgue run ./project -o ./out    # CLI headless
morgue run --watch ./project     # CLI + progress
morgue tools check               # tool status
morgue tools install              # download tools
morgue self-update                # update binary
```

## Build

```bash
go build -o morgue.exe ./cmd/morgue
```
