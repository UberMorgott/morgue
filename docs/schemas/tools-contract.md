# M3 tool policy contract

Owned by the tools track: `internal/tools/**`. Callers and UI must not edit it.
Existing `Context.Tools` stays `*tools.Manager`. No new process environment logic.

Public APIs (implementation lands after this contract commit):

```go
func (m *Manager) InstallVersion(context.Context, string, InstallOptions, *InstallCallbacks) (ToolIdentity, error)
func (m *Manager) NewRunSelection(context.Context) (*RunSelection, error)
func (s *RunSelection) Ensure(context.Context, []string, *InstallCallbacks) ([]ToolIdentity, error)
func (s *RunSelection) Bind() *Manager
func (s *RunSelection) Snapshot(complete bool) RunToolsLock
func (s *RunSelection) Close() error
func (m *Manager) RecordHelper(ctx context.Context, name, path, sourceSHA256 string) (ToolIdentity, error)
func (m *Manager) WithHelper(ctx context.Context, name string, build func() (path, sourceSHA256 string, err error)) (ToolIdentity, error)
func ReadInstalledLock(baseDir string) (InstalledLock, error)
```

- Bind before recon. Bound Resolve auto-Ensures a not-yet-selected name with the
  selection context, and verifies external payload identity on every Resolve.
  Explicit Ensure is preferred for callbacks and required/optional diagnostics.
  Bound Install delegates to Ensure (it cannot clear an operator override).
- `InstallCallbacks.OnStatus func(ToolPolicyStatus)` reports checking, pinned,
  current, updated, system, warning, error. Existing progress/extract callbacks stay.
  `ToolStatus` adds `Freshness`, `FreshnessWarning`, `OperatorVersion`, `InUse`
  using existing PascalCase JSON convention. Wire identities use lowerCamelCase.
- Snapshot includes tools, runtimes (`runtime:<kind>`) and embedded helpers.
  Complete=true seals acquisition; Close releases leases. Callers persist a
  provisional snapshot before each invocation, a complete snapshot before terminal
  run publication, and Close on every exit. A closed selection cannot execute.
- WithHelper runs cache validation/build under an exclusive helper lock, hashes
  returned output, then retains a shared lease before returning. Build callback
  must use its SDK via a previously selected RuntimePath; its source digest must
  cover source + project file + SDK identity + application commit. RecordHelper
  hashes an already-built payload under the same lock, then leases it. WithHelper
  is required for publication; no target execution occurs while compiling.
- Automatic selection forces one uncached upstream version probe per distinct
  used tool/run, at most three concurrent probes, 15 seconds per request. Pinned
  definitions and operator overrides use the exact requested version. Working
  offline installs are leased with a warning; missing installs fail.
- Managed mutations use exclusive nonblocking cross-process locks and return
  ErrInUse on an active lease. Selection acquisition waits cancellably; no upgrade
  from one's own shared lease. Runtime installs follow the same protocol.
- Installed `tools.lock.json`: schemaVersion=2, tools map of ToolIdentity,
  operatorVersions map of exact requested versions. Legacy string maps migrate
  on write. ReadLock keeps its legacy Go map view for existing callers.
- Explicit install Version=X records an operator override only on success.
  Empty Version clears it only on success. An unversioned direct URL rejects X.
  A failed download, hash validation or lock write preserves old install and pin.
- Payload inventory: sorted slash-relative path + SHA-256 per regular file, hashed
  as UTF-8 `path + NUL + hexHash + LF`. Exclude root `.version` and documented
  mutable directories `.cache`, `cache`, `logs`, `temp`, `tmp` at any depth.
  Reject reparse/symlink escapes. System scope is the binary's containing payload
  directory (runtime scope is its home); inventory is verified before each use.
- `source` is github/nuget/direct/dotnet/system/embedded/runtime; `version` is
  observed exact version or `unknown`, never `latest`. No secrets/URLs in identities.
- HTTP fake servers: existing tests replace http.DefaultTransport or use mirror
  rewriting; no global endpoint registry or live network in -short tests.

Workspace tools lock uses RunToolsLock (schemaVersion=1); complete only after all
acquisition is sealed. The callers track owns workspace projection/instructions.
