# HashLink / Haxe support — design notes

- **Date:** 2026-10-28
- **Status:** Planned (see `docs/ROADMAP.md`). Not implemented: no HLB detection, recipe or tool in the code.
- **Sources:** GitHub issues #1, #2; field report on Wartales (2026-09-17).

## Problem

HashLink games (Haxe/Heaps: Wartales, Northgard, Dune: Spice Wars, Dead Cells, Evoland) are not supported.

- `hlboot.dat` (the whole game, e.g. 19 MB of HL bytecode in Wartales) → `kind: Unknown`, no recipe.
- `Wartales.exe` (142 KB `libhl` launcher, no game code) → `native` recipe → pointless Ghidra run.
- `*.hdll` (`steam.hdll`, `heaps.hdll`, `fmt.hdll`, `ui.hdll`) are ordinary native DLLs but are not tagged as
  HashLink plugins.

## Detection

1. `hlboot.dat` / `*.hl`: magic `48 4C 42` (`"HLB"`) + 1 version byte (Wartales: `48 4C 42 04` = v4).
2. `-hl` builds with embedded bytecode: same `HLB` magic in a PE section or overlay.
3. Folder hints: `libhl.dll` + `*.hdll` + `hlboot.dat` (Windows); `libhl.so` / `libhl.dylib` elsewhere.
4. `*.pak` next to it (`res.pak`, `assets.pak`) = Heaps ResFS asset packs, not bytecode.

Target redirection: when the input is the launcher `.exe` and a sibling `hlboot.dat` exists, process
`hlboot.dat` (and say so in `morgue info`). Tag `*.hdll` via skip-list / recon instead of sending them to Ghidra.

## Tool

`hlbc` (Rust, MIT, https://github.com/Gui-Yom/hlbc): parses HLB v4/v5, dumps types, functions, globals,
strings, disassembly, partial Haxe-like decompiler. Prebuilt GitHub releases exist (no cargo needed) → fits
`MethodGitHubRelease`. Commands: `info`, `entrypoint`, `fn <idx>`, `decomp <idx>`, `strings`, `types`,
`search`. Official `hl --dump` (https://github.com/HaxeFoundation/hashlink) is a fallback for structure only.

Known upstream defects in hlbc 0.7.0 (GH #2) — plan: ship a vendored fork with patches:

| Id | Defect | Handling |
|----|--------|----------|
| X-01 | CLI REPL hangs on stdin | feed commands + `exit` on stdin |
| X-02 | `saveto` NulError on strings containing NUL | fork patch |
| X-03 | `decomp` panic `types.rs:256` (inherited proto index) | fork patch: walk `super_` matching `pindex` |
| X-04 | `decomp` drops calls (e.g. `api.networkOperation`) | also emit per-function disassembly |
| X-05 | wrong proto-call names | same |
| X-06 | `[missing expr]` in output | same |

## Recipe `hashlink` (minimum)

- Match: `kind: HashLink` (HLB magic in file or PE overlay).
- Output per target: `types.txt`, `functions.txt`, `strings.txt`, `natives.txt`, `decomp/<pkg>/<Class>.hx`,
  plus disassembly per function (because of X-04..X-06).
- `natives.txt` = list of `lib@symbol` → which `*.hdll` is called (e.g. `steam@steam_send_p2p_packet` →
  `steam.hdll`); the key link between bytecode and native plugins for reverse engineering.
- Optional later: Heaps `*.pak` (ResFS) unpack as a game-data step.
