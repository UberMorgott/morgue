# Morgue issues — 2026-09-10

## unity-mono: AssetRipper full-project export re-runs once PER managed assembly (disk blowup)

**Severity: high — fills the output drive.**

The `unity-mono` recipe treats the AssetRipper asset export as a per-assembly
step instead of a per-game step. `internal/recipe/unity_mono.go:209` builds the
export directory from the per-assembly output root:

```go
rawExportDir := filepath.Join(ctx.Output, "raw-export")
```

and step 4 (`unity_mono.go:204-232`, "Extract game assets (AssetRipper)") then
runs a **full** `ExportedProject` export of the entire game asset tree into it.
Since `ctx.Output` is `<out>\<Assembly>\`, every managed assembly in
`valheim_Data\Managed` gets its own near-identical multi-GB copy of the same
assets:

```
<out>\<Assembly>\raw-export\ExportedProject\Assets\...   # ~15-20 GB each
```

### Measured on Valheim (2026-09-10)

| | |
|---|---|
| Managed assemblies in `valheim_Data\Managed` | 141 |
| Assemblies completed before the run was killed | 3 |
| Disk consumed by those 3 | ~53 GB (50.3 GB reclaimed by deleting `raw-export`) |
| `raw-export` for `Assembly-CSharp` alone | 13.47 GB / 76,586 files |
| Projected total for all 141 | **~2.5 TB** |
| Free space on the output drive (E:) at kill time | ~172 GB |

The run had to be killed (`morgue.exe`, PID 30864, from
`E:\DEV\Morgue\dist\morgue.exe`) to stop it filling the disk.

Trigger — exact command line, read off the live process via
`Get-CimInstance Win32_Process`:

```
"E:\DEV\Morgue\dist\morgue.exe" run D:\Steam\steamapps\common\Valheim\Valheim_Data\Managed\assembly_valheim.dll -o "E:\DEV Valheim\ValheimDecompiled"
```

Note this is the SINGLE-assembly form, and it still produced a full 15-20 GB
`ExportedProject` export. The blowup therefore does not need the whole-folder
invocation: every single `morgue run <one.dll>` pays the entire asset export.
Three such runs were observed alive concurrently (PIDs 30864, 30444, 28788),
each independently re-exporting the same asset tree into its own output
folder.

### The duplication is total, not partial

For `Assembly-CSharp` the `raw-export` tree contained exactly 16 `.cs` files,
all of them already present in `src\` (18 files). So the 13.47 GB bought
zero unique code — it was assets plus a duplicate of the decompiled source.

### Expected behaviour

1. Run the AssetRipper `ExportedProject` export **once per game**, into a
   shared `<out>\_assets\raw-export\`, and have per-assembly outputs reference
   it rather than re-export it.
2. Skip asset export entirely for assemblies that are not the game's own code.
   Engine and third-party assemblies (`UnityEngine.*`, `System.*`, `Mono.*`,
   `Unity.*`, `Autodesk.Fbx`, `Newtonsoft.Json`, …) carry no game assets; for
   them the recipe should do code-only decompilation. A cheap heuristic: export
   assets only for assemblies referenced by `ScriptingAssemblies.json` /
   matching the game's own assembly names, skip the rest.
3. Provide an explicit opt-out flag (`--no-assets` / `--code-only`) regardless.
4. Guard against the failure mode: check free disk before starting and abort
   with a clear message when the projected footprint exceeds it.

### Side note found while cleaning up

For Valheim, `Assembly-CSharp.dll` is a 23 KB stub (Xbox GDK wrapper +
LuxParticles demo). The actual game code — `Player`, `Character`, `SEMan` —
lives in `assembly_valheim.dll` (2.45 MB). Any docs or defaults that assume
`Assembly-CSharp` is "the game code" are wrong for this title.
