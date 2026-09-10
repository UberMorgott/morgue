# Morgue issues — 2026-06-25

## build.bat: `cmd.exe /c "build.bat"` from Git Bash silently no-ops (returns exit 0, does NOT build)

During the v0.6.0 release, the first build attempt invoked from the Git Bash tool:

```
cmd.exe /c "build.bat" > release-build.log 2>&1   # EXITCODE=0
```

Returned exit code 0 but did NOT execute the script body. The captured log
contained only the cmd.exe banner (`Microsoft Windows [Version 10.0.26100.8037]`
+ a fresh `E:\DEV\Morgue>` prompt) and none of build.bat's own output. The
resulting `dist\morgue.exe` was STALE (mtime 20:56:41, built earlier at commit
3bc85b7 before the v0.6.0 tag existed), so `morgue.exe version` reported the
wrong string:

```
morgue v0.5.0-41-g3bc85b7 (3bc85b7)
```

Root cause: invoking the `.bat` via `cmd.exe /c "build.bat"` through Git Bash
spawned an interactive-style cmd that did not run the batch file to completion.
Workaround that worked: run it from PowerShell instead —

```
& cmd.exe /c "E:\DEV\Morgue\build.bat" *> $log   # EXIT=0, real build
```

After the PowerShell invocation the build ran correctly (`Version: v0.6.0`,
`Commit: 8fae54a`, fresh exe mtime 21:20:46) and `morgue.exe version` reported
`morgue v0.6.0 (8fae54a)`.

Note (not a defect, expected behavior): `build.bat` step 1 (`wails3 generate
bindings`) regenerates `frontend/bindings/.../*.js` with non-deterministic churn,
leaving the working tree dirty after every build. Because build.bat captures the
version via `git describe` at the START (line 19, before regen), the embedded
ldflags stay clean (no `-dirty`); the leftover churn is generated noise and was
discarded with `git checkout -- frontend/bindings` to keep the tree matching the
tagged commit.
