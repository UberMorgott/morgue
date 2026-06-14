# morgue v0.4.5 — bug report (CLI stdout silently dropped)

Date: 2026-06-14
Binary: `dist/morgue.exe` from GitHub release `v0.4.5` (asset `morgue_windows_amd64.zip`,
sha256 `275dfc636011cbff8f64c37e4b450a37e687e58813460796d2daa1ede04a617b`, checksum verified OK).
Host: Windows 11, PowerShell 7, run non-interactively via `Start-Process -NoNewWindow -Wait`
with `-RedirectStandardOutput`/`-RedirectStandardError`.

## Symptom — every command emits 0 bytes on stdout

`exit code = 0` in all cases, files written correctly to disk, but **stdout is empty**:

| Command | stdout | stderr |
|---|---|---|
| `morgue version` | 0 bytes (expected: version + commit) | only `memory cap: limited process to 4096 MiB` |
| `morgue info <dll>` (json default) | 0 bytes (expected: JSON classification) | only memcap line |
| `morgue info <dll> --format text` | 0 bytes | only memcap line |
| `morgue run <dll> -o <dir>` | 0 bytes | only memcap line |
| `morgue run <dll> -o <dir> --quiet` | **0 bytes** — README says `--quiet` must "emit only JSON to stdout" | only memcap line |

stderr IS captured through the same redirect (the `log` memcap line comes through), so the
redirect itself works. Only **os.Stdout** content never appears.

Reproduced with both plain `& morgue.exe version 2>&1` and `Start-Process ... -RedirectStandardOutput`.

## Impact

- `morgue info` / `morgue version` give no machine- or human-readable output on the CLI.
- `--quiet` JSON contract is broken → any AI agent / HTTP-less script that parses stdout
  for the run result gets nothing. (`run` still writes `summary.json`/`recon.json` to the
  output dir, so on-disk results are fine — only the stdout channel is dead.)
- Regression vs v0.3.0: the previous local binary printed `morgue v0.3.0-99-g3d0f241 (3d0f241)`
  on `version` correctly from the same shell.

## Likely cause (for the fixing agent to verify)

Most probable: the GoReleaser build links the EXE as a **Windows GUI subsystem** binary
(`-H windowsgui` / `-ldflags "-H=windowsgui"`), so when launched as a CLI it has no attached
console and `os.Stdout` writes go to an invalid handle and are dropped. (stderr appearing is
inconsistent with that and should be double-checked — possibly the memcap `log` writes before
std handles are reassigned, or stdout uses a tty-aware renderer that no-ops on non-tty while
`--quiet` is supposed to bypass it but doesn't.)

Suggested fixes to consider:
- Ship/console-link the CLI path (console subsystem), or call `AttachConsole(ATTACH_PARENT_PROCESS)`
  / `AllocConsole` on CLI startup before writing stdout; or
- Provide a separate console-subsystem CLI exe; and
- Add a smoke test asserting `morgue version` and `morgue run --quiet` produce non-empty stdout
  when invoked with redirected handles.

## What WORKED (so the regression scope is clear)

`morgue run E:\DEV\Replacer\Texture_Replacer_BE5.dll -o E:\DEV\Replacer` fully succeeded:
- recon: kind `Managed`, runtime `CLR 2.5`, recipe `dotnet-generic`, no dynamic step needed.
- `summary.json`: total 1 / success 1 / failed 0 / skipped 0, duration 1.553s.
- Decompiled C# written under `Texture_Replacer_BE5/src/` (loader.cs, Const.cs,
  Texture_Replacer.cs ~27KB, AssemblyInfo.cs, .csproj, index.json) + `strings.json`.

So the decompile pipeline and on-disk output are healthy; the bug is confined to the CLI
stdout channel.

---

## Resolution (v0.4.6)

Fixed by guarding `attachParentConsole()` (cmd/morgue/console_windows.go): the GUI-subsystem
release binary now attaches to the parent terminal's console for interactive CLI use, but
**no-ops when stdout is already a valid handle** (redirected pipe/file). This restores the
`--quiet` JSON-to-stdout contract and all redirected-handle scenarios (`Start-Process
-RedirectStandardOutput`, `cmd > file`, pipes) while keeping the GUI launch console-free.
Regression guarded by `TestAttachParentConsoleKeepsRedirectedStdout`. Verified on
`Texture_Replacer_BE5.dll`: version/info/run/`--quiet` all emit non-empty stdout.
