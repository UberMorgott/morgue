# Universal Decompiler — Design Spec

## Vision
Morgue should decompile any Windows binary — .NET, native C++, UE5 games, Unity games — with auto-detection, auto-tool-install, and AI-optimized output.

## Core Workflow
1. User drops file/folder → auto-detect binary type (PE parse + DiE + heuristics)
2. Determine required tools and recipe
3. Auto-download missing tools with progress
4. Run analysis pipeline with real-time progress for each step
5. Output structured, AI-readable project

## Supported Targets

### Already Working
- .NET managed (generic) — ILSpy decompilation
- .NET obfuscated (ConfuserEx) — multi-step deobfuscation + decompilation
- Delphi — IDR + Ghidra
- Unity Mono — ILSpy on managed DLLs (stub)
- Unity IL2CPP — IL2CPP dump (stub)

### To Add
- **UE5 (Unreal Engine 5)** — full pipeline
- **Native C/C++** — improved Ghidra pipeline with indexing
- **Go binaries** — GoReSym + Ghidra

## UE5 Pipeline Steps

Each step is a toggle in Settings with tooltip explanation.

| # | Step | Default | Description for users |
|---|------|---------|----------------------|
| 1 | Extract PAK assets | ON | Extract game assets from .pak/.utoc containers. Gives access to Blueprints, data tables, and asset structure |
| 2 | SDK class dump | ON | Dump all class names, functions, properties, and inheritance. The foundation — AI uses this to understand game structure |
| 3 | String extraction | ON | Find debug strings and source file paths in the binary. Helps AI locate specific functions by name |
| 4 | Full Ghidra decompilation | OFF | Complete binary decompilation to C code. Takes hours but gives full function bodies. Enable when you need to understand HOW something works, not just WHAT exists |
| 5 | Name resolution | ON | Replace auto-generated function names (FUN_12345) with real names from SDK dump and debug strings. Critical for readability |
| 6 | Build search indexes | ON | Create cross-reference indexes (who calls what, string references, class hierarchy). Enables AI to navigate the codebase instantly |
| 7 | Export hookable symbols | ON | List all functions that can be hooked from Lua/UE4SS. Essential for mod development |

## Output Structure

```
<project>/
  README.md              — game name, version, analysis date, what was analyzed
  classes/               — one file per class with properties + methods
    APlayerCharacter.h
    UInventoryComponent.h
  decompiled/            — C code organized by source module (when Ghidra enabled)
    R5Movement/
    R5Combat/
  indexes/
    string_refs.csv      — function → strings it references
    callers.csv          — caller → callee graph
    class_hierarchy.json — inheritance tree
    hookable.json        — hookable functions for modding
  sdk/
    CXXHeaders/          — full SDK header dump
    ObjectDump.txt       — all reflected UE objects
  raw/
    strings_ascii.txt
    strings_utf16.txt
```

## Settings UI

New section "Unreal Engine" in Settings page:
- Toggle for each pipeline step
- (?) icon next to each toggle → expands full description
- Descriptions explain: what the step does, why it matters, how AI uses it
- Default: steps 1-3, 5-7 ON; step 4 (Ghidra) OFF

Similar sections for other engines (Unity, .NET) with engine-specific steps.

## Auto-Detection

When user drops a file:
1. PE parse → .NET or Native
2. DiE scan → compiler, packer, obfuscator
3. Heuristics → UE5 markers, Unity markers, Delphi markers
4. Match recipe → show detected type + recommended recipe
5. Check required tools → auto-install missing
6. Start pipeline with progress

## Tools to Add

| Tool | Purpose | Source |
|------|---------|--------|
| retoc | UE5 PAK/IoStore extractor | GitHub release |
| FModel | UE asset inspector | GitHub release |
| extract_strings | Binary string extractor | Custom (from Windrose) |
| dump_all_names | UE FName table dumper | Custom (from Windrose) |
| il2cppdumper | Unity IL2CPP metadata | GitHub release |

## Progress & Transparency

- Each pipeline step visible in Operations panel
- Step name + percentage + elapsed time
- Expandable log showing tool output
- Non-blocking — UI stays responsive during analysis

## AI Optimization

Output format designed for Serena navigation:
- `.h` files → `get_symbols_overview` finds classes/methods
- `.json` indexes → `search_for_pattern` finds cross-references
- `.csv` files → greppable for specific strings/addresses
- Flat file structure → `find_file` works efficiently
