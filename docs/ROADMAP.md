# Morgue Roadmap

Primary goal: **LLM-readable output with measured quality and provenance.** A tidy workspace and a green
`done` are not enough; every release must make it easier to trust what an agent reads.

Fixed constraints (owner decisions, 2026-10-28):

- One-shot usage (decompile a game, read it, move on). **No MCP server, no daemon / background service.**
- Keep the hybrid: GUI (Wails) + CLI (Cobra) + local HTTP API that exists only while the GUI is open.
- Tools update themselves; nobody should hand-maintain versions.
- Recipes are added often, frequently by AI agents (see [AGENTS.md](../AGENTS.md)).

## v0.7.10 — hybrid-mode correctness (shipped in this release)

Atomic run admission (`409 busy`), `POST /api/stop`, per-launch API token + origin/Host checks, SSE
keepalive, typed run/target outcomes with wire-safe error strings, API-started runs adopted by the GUI,
explicit `--ref` dependency paths, tool update/pin fixes, CI on push/PR.

## v0.8 — runs you can trust and reuse (done)

Shipped: unified RunSpec/run ID across CLI/GUI/API, durable journal + `morgue.json` + `morgue inspect`,
AGENTS.md/INDEX.md navigation, frozen run tool lock (detector included) with `--version` pins, per-target
measured `quality.json` (incl. name quality) and `provenance.ndjson`, fidelity labels, portable-only
runtimes (managed JDK matched to Ghidra `application.java.min`, no system fallback by default), Ghidra
heap/thread caps.

1. **RunSpec + run ID.** One run description shared by CLI, GUI and API (path, output, recipe, excludes,
   `--ref`, code-only, dynamic, cflow…). Today `/api/run` accepts only `path`/`output`. Every run gets an ID.
   Nested runs (NSIS unpack → child pipeline) report their child targets to the parent outcome.
2. **Journal.** Append-only `<output>/.morgue/journal.ndjson` (versioned events with `ts`, `seq`, `run`,
   `type`) written by every entry point, plus an incrementally and atomically published manifest. Cancelled
   or crashed runs still leave a record. Backend-owned `startedAt` drives elapsed time for every client.
3. **LLM workspace** in each output root:
   - `morgue.json` — machine manifest: targets, recipe, outcome, tool versions, timings, file counts.
   - `INDEX.md` — run index + per-target `INDEX.md` (kind, recipe, entry points, where to start reading,
     warnings).
   - `AGENTS.md` — how to navigate this output; the only file an agent must read.
   - Build on existing streaming indexes (`functions.ndjson`, name-resolution, strings analysis); no single
     huge `symbols.json`. Dedupe keeps originals.
4. **Provenance.** Input hash, tool + version, parameters, module/token/RVA/address, original → renamed
   name and the source/confidence of each recovered name. Pseudocode, IL2CPP stubs and real source are
   labeled distinctly.
5. **Quality report** per target: functions/types found vs decompiled, decompiler errors and
   `Unknown result type` comments, unresolved references, residual obfuscation, string-decryption coverage
   (absence of PUA characters is not proof of full decryption). Checks are per recipe family.
6. **Unified tool update policy** (CLI and GUI identical): freshness check before a run uses a tool, versions
   frozen for the whole run, working install kept on network errors, `tools install <name> --version X`
   for manual rollback. No automatic rollback + retry on arbitrary step failure (input may be the cause,
   dynamic steps have side effects). Record installed version/hash in a lockfile.

Out of scope for v0.8: job queue, learned ETA, full results browser, automatic resume.

## v0.9 — cheap, safe recipes (next)

- **Recipe manifest**: name, explicit priority (replaces `init()` order + `RegisterFirst`), required/optional
  tools, steps with batch group (replaces `Name()=="native"` and `StepFilter` strings), outputs, capabilities.
  Go `Execute` stays as the escape hatch for complex recipes.
- Match and output fixtures per recipe; registry test for unique priority and non-overlapping matches.
- Generated recipe docs: `morgue recipes [--json]`, README table and `/api/instructions` generated from
  manifests (today the list in `internal/instructions/instructions.go` is hand-written).
- Remove `executeRecipe` / `executeRecipeWithFilter` duplication and recipe-specific cleanup in
  `internal/engine/pipeline.go`.

## Deferred features (from bug triage 2026-10-28)

| Item | Notes |
|------|-------|
| HashLink / Haxe support (GH #1, #2) | Detection, `hlbc` tool, `hashlink` recipe, upstream defects: [docs/specs/2026-10-28-hashlink-support.md](specs/2026-10-28-hashlink-support.md) |
| Runtime CLI | `morgue tools runtimes [--update]`: list/update managed .NET/Java runtimes (no CLI today) |
| Free-disk guard | Warn/abort before AssetRipper / Ghidra steps when the output drive is low (`internal/util/disk_windows.go` is only used to pick the default output drive) |
| Lightweight native mode | Research first: rizin + rz-ghidra (Ghidra decompiler as a native plugin) for small native targets without a JVM — compare output fidelity, function coverage and name quality vs headless Ghidra before adding a recipe |
| Folder-walk exclusions | Optionally skip save/mod data dirs (`Saves`, `Mods`, `BepInEx/plugins`) under a game root |

## Removal candidates (not removed in v0.7.10)

- `internal/tui` + `run --watch` / `--force-watch` (bubbletea, only a second progress renderer) → replace with a
  plain line renderer driven by typed events.
- `morgue api run|tools|settings` → fold into normal commands (`--gui=auto` delegation) once RunSpec gives
  parity; keep aliases for one release. `api_cmd.go` `settingTypes` duplicates the config schema.
- Tool registry entries not used by any recipe: `goresym`, `nofuserex`; plus the `MethodGitBuild` install path (no registry entry uses it).
- Regex parsing of human log messages in `frontend/src/lib/pipeline.ts` (after typed events) and
  localStorage run history (after the journal).
