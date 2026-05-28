# Morgue UX Redesign — Reactive Single-Page Pipeline

> **SUPERSEDED (2026-05-28):** This spec was the initial UX redesign proposal. The pipeline page was further redesigned in `docs/superpowers/specs/2026-05-28-pipeline-page-redesign.md` which merges Scan+Recon into a single Analysis step (4 steps instead of 5), adds granular progress rings, composition/tools/execution panels, and obfuscation handling. The current implementation follows the newer spec. This document is kept for historical reference only.

**Date:** 2026-05-26
**Status:** Superseded

## Overview

Redesign the Morgue frontend UX to a reactive single-page pipeline flow. No "Next" buttons — each step appears automatically as the previous one completes. Background operations (tool downloads, pipeline execution) show progress in a global StatusBar visible on all pages.

## Pages

### 1. Home Page

Minimal entry point. Two ways to select targets:

- **DropZone** — drag & drop file or folder
- **"Open" button** — native file/folder dialog via `ReconService.PickDirectory()`

After selection → automatic navigation to Pipeline view.

### 2. Pipeline View (single page, reactive)

All steps appear sequentially on one page. Previous steps collapse as the next begins.

**Step 1 — Scan**
- Auto-starts on entry
- Spinner: "Scanning..."
- Result: list of binaries found with type tags (managed, Delphi, native, IL2CPP, unity_mono)

**Step 2 — Recon**
- Auto-starts after scan completes
- For each binary: detect obfuscator, select recipe
- Tags appear: "ConfuserEx", "Delphi VCL", "Generic .NET", etc.

**Step 3 — Tools Check**
- Auto-starts after recon completes
- Shows which tools are required for the detected recipes
- Status per tool: installed / missing
- If any missing: "Install missing" button. Blocks Step 4 until resolved.
- If all present: auto-proceeds to Step 4

**Step 4 — Execute**
- Auto-starts if all tools present, or starts after missing tools installed
- Progress bar showing current recipe step: unpack → anti-tamper → string decrypt → control flow → proxy removal → rename → decompile
- Step label: `"Step 3/7: string decrypt"`
- Log viewer below progress bar

**Step 5 — Done**
- Result summary: output path, file count, total time
- Button to open output folder (optional)

### 3. Tools Page

Shows **all** registered tools (not just those needed for current recipe).

**Per tool row:**
- Tool name + type tag (deobfuscator, decompiler, classifier, strings)
- **Installed**: bright/full color. Shows version. Buttons: "Update" / "Delete"
- **Not installed**: dimmed/gray. Button: "Download"
- Latest version from GitHub (checked on page open). Badge if update available.

**Top actions:**
- "Download All" — installs all missing tools
- "Update All" — updates all outdated tools

**Download/update progress** is shown in the global **StatusBar**, not on the tool row. User can navigate away and still see progress.

### 4. Settings Page

Same sections as current, with one change:

- **Output directory**: text input + 📁 button → native directory picker (`PickDirectory()`)

All other settings unchanged:
- Pipeline (timeout, concurrent targets, stop on error, keep intermediates)
- Skip-list (system libs)
- Decompiler (C# version, PDB, project mode, callgraph)
- Network (GitHub token, retries, auto-update check)
- Logging (level, log to file)

### 5. StatusBar (global, all pages)

Bottom panel visible on every page. Shows current background operation:

- **Tool download/update**: `"Downloading de4dot-cex... 45%"` + progress bar
- **Pipeline execution**: `"ConfuserEx → string decrypt [3/7]"` + progress bar
- **Idle**: `"Ready"` / `"Готов"`

StatusBar is display-only. No click actions.

## Technical Notes

### Events (Go → Frontend)

- `pipeline:progress` — phase, target, step index, total steps, step name, done flag
- `pipeline:log` — log line from current step
- `tool:download:progress` — tool name, bytes downloaded, total bytes, percent
- `tool:download:complete` — tool name, version
- `tool:check:result` — tool name, installed, current version, latest version, update available

### New Go Methods Needed

- `ToolsService.CheckAllWithUpdates()` — returns tool status + latest GitHub version + update available flag
- `ToolsService.Delete(name)` — removes a tool
- `ReconService.PickDirectory()` — already implemented
- `ReconService.PickFile()` — native file picker (single binary)

### Config Model Field Names

Frontend uses PascalCase matching the generated Wails bindings:
`DefaultOutputDir`, `StepTimeoutMinutes`, `ConcurrentTargets`, `LogLevel`, etc.

## i18n

All new UI text must have en/ru translations in `i18n.ts`.
