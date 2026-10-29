// Tool update/pin policy as seen by the UI (M3, contract docs/schemas/tools-contract.md).
// Pure functions only: ToolsPage/ToolRow/Header and the run panels derive every
// displayed tool state from here, so "what is happening to my tools" has one source.
import type { ToolProgress, LogRecord } from './events';

/** Backend ToolPolicyStatus.freshness / ToolStatus.Freshness values. */
export type Freshness = 'unchecked' | 'checking' | 'current' | 'updated' | 'pinned' | 'system' | 'warning' | 'error';
const FRESHNESS: ReadonlySet<string> = new Set(['unchecked', 'checking', 'current', 'updated', 'pinned', 'system', 'warning', 'error']);

/**
 * Typed shim for tools.ToolStatus until frontend bindings are regenerated in
 * M3 serial integration (Freshness, FreshnessWarning, OperatorVersion, InUse
 * are added by contract 13c2d09; PascalCase wire names).
 */
export interface RawToolStatus {
  Name?: string; Installed?: boolean; Path?: string; Version?: string;
  LatestVersion?: string; UpdateAvailable?: boolean; Category?: string; Description?: string;
  Optional?: boolean; RuntimeDeps?: string[] | null;
  Freshness?: string; FreshnessWarning?: string; OperatorVersion?: string; InUse?: boolean;
}

export interface ToolView {
  name: string; installed: boolean; path: string; version: string;
  latestVersion: string; updateAvailable: boolean; category: string; description: string;
  optional: boolean; runtimeDeps: string[];
  freshness: Freshness; warning: string; operatorVersion: string; inUse: boolean;
  /** UI-side: a manual latest-version probe is in flight. */
  checking: boolean;
}

export function parseFreshness(v: unknown): Freshness {
  return typeof v === 'string' && FRESHNESS.has(v) ? (v as Freshness) : 'unchecked';
}

/**
 * Normalize a backend status, keeping UI-probe data (latest version) from the
 * previous view only while the installed version is unchanged: after an update
 * the old "update available" must not linger.
 */
export function toToolView(raw: RawToolStatus, prev?: ToolView): ToolView {
  const version = raw.Version ?? '';
  const sameVersion = !!prev && prev.version === version;
  return {
    name: raw.Name ?? '',
    installed: raw.Installed ?? false,
    path: raw.Path ?? '',
    version,
    latestVersion: raw.LatestVersion || (sameVersion ? prev!.latestVersion : ''),
    updateAvailable: (raw.UpdateAvailable ?? false) || (sameVersion ? prev!.updateAvailable : false),
    category: raw.Category ?? '',
    description: raw.Description ?? '',
    optional: raw.Optional ?? false,
    runtimeDeps: raw.RuntimeDeps ?? [],
    freshness: parseFreshness(raw.Freshness),
    warning: raw.FreshnessWarning ?? '',
    operatorVersion: raw.OperatorVersion ?? '',
    inUse: raw.InUse ?? false,
    checking: prev?.checking ?? false,
  };
}

/** True when the visible fields differ (polling only re-renders on change). */
export function viewChanged(a: ToolView, b: ToolView): boolean {
  return a.installed !== b.installed || a.version !== b.version || a.path !== b.path ||
    a.freshness !== b.freshness || a.warning !== b.warning ||
    a.operatorVersion !== b.operatorVersion || a.inUse !== b.inUse;
}

/**
 * One display state per tool, in priority order:
 * failed       last install/update/verification failed (freshness=error)
 * missing      not installed (auto-downloaded on first use)
 * pinned       operator version override; automatic updates paused
 * system       PATH tool, not managed or updated by Morgue
 * checking     freshness probe in flight
 * stale        installed and working, but the update check failed (offline/rate limit)
 * outdated     newer release known; applied automatically before next use
 * updated      updated by the last freshness check
 * current      latest known version
 * unchecked    installed, not checked yet
 */
export type ToolState = 'failed' | 'missing' | 'pinned' | 'system' | 'checking' | 'stale' | 'outdated' | 'updated' | 'current' | 'unchecked';

export function toolState(v: ToolView): ToolState {
  // error: last install/verification failed; an installed payload, if any, is kept.
  if (v.freshness === 'error') return 'failed';
  if (!v.installed) return 'missing';
  if (v.operatorVersion || v.freshness === 'pinned') return 'pinned';
  if (v.freshness === 'system') return 'system';
  if (v.checking || v.freshness === 'checking') return 'checking';
  if (v.freshness === 'warning') return 'stale';
  if (v.updateAvailable) return 'outdated';
  if (v.freshness === 'updated') return 'updated';
  if (v.freshness === 'current' || v.latestVersion) return 'current';
  return 'unchecked';
}

export type ToolTone = 'muted' | 'ok' | 'accent' | 'warn' | 'error';
export const stateTone: Record<ToolState, ToolTone> = {
  failed: 'error', missing: 'muted', pinned: 'accent', system: 'muted', checking: 'muted',
  stale: 'warn', outdated: 'accent', updated: 'ok', current: 'ok', unchecked: 'muted',
};

export function stateKey(s: ToolState): string { return `tools.state.${s}`; }

/**
 * The single primary action a row offers. All of them call the unversioned
 * install, which per contract also clears an operator pin on success:
 * download (missing), repair (failed), update (outdated), unpin (pinned).
 */
export type ToolAction = 'download' | 'repair' | 'update' | 'unpin';

export function primaryAction(v: ToolView): ToolAction | null {
  switch (toolState(v)) {
    case 'missing': return 'download';
    case 'failed': return 'repair';
    case 'pinned': return 'unpin';
    case 'outdated': return 'update';
    default: return null;
  }
}

/** Mutations of a leased tool are refused by the backend; mirror that up front. */
export function canMutate(v: ToolView): boolean { return !v.inUse; }
export function canDelete(v: ToolView): boolean {
  return v.installed && !v.inUse && toolState(v) !== 'system';
}

/** Backend tools.ErrInUse ("tool is in use") or service ErrBusy. */
export function isInUseError(err: unknown): boolean {
  const msg = String((err as { message?: string })?.message ?? err ?? '');
  return /\bin use\b|\bbusy\b/i.test(msg);
}

/** Header badges: pending updates and tools needing the user's eye. */
export function headerSummary(views: ToolView[]): { updates: number; attention: number } {
  let updates = 0, attention = 0;
  for (const v of views) {
    const s = toolState(v);
    if (s === 'outdated') updates++;
    else if (s === 'failed' || s === 'stale') attention++;
  }
  return { updates, attention };
}

// ---------------------------------------------------------------------------
// Per-run tool identity (typed tool.progress / tool-scoped log records).

export type RunToolStage = 'checking' | 'downloading' | 'extracting' | 'ready' | 'failed';

export interface RunToolInfo {
  stage: RunToolStage;
  /** Exact version this run selected (tool.progress ready.version); '' until known. */
  version: string;
  /** A payload was fetched during this run (fresh install or automatic update). */
  fetched: boolean;
  error: string;
  /** Latest tool-scoped warning (e.g. freshness check failed, working install kept). */
  warning: string;
}

const stageMap: Record<string, RunToolStage> = {
  check: 'checking', download: 'downloading', extract: 'extracting', ready: 'ready', failed: 'failed',
};

/** Pure reducer; returns the same map when the event is not tool-related. */
export function reduceRunTools(
  map: Record<string, RunToolInfo>,
  ev: { type: string; data: unknown },
): Record<string, RunToolInfo> {
  if (ev.type === 'tool.progress') {
    const p = ev.data as ToolProgress;
    const stage = stageMap[p?.stage];
    if (!p?.tool || !stage) return map;
    const prev = map[p.tool] ?? { stage, version: '', fetched: false, error: '', warning: '' };
    return {
      ...map,
      [p.tool]: {
        stage,
        // A version is only trusted once the payload is ready (activated + recorded).
        version: stage === 'ready' ? (p.version || prev.version) : prev.version,
        fetched: prev.fetched || stage === 'downloading' || stage === 'extracting',
        error: stage === 'failed' ? (p.error || prev.error) : stage === 'ready' ? '' : prev.error,
        warning: prev.warning,
      },
    };
  }
  if (ev.type === 'log') {
    const d = ev.data as LogRecord;
    if (!d?.tool || d.level !== 'warn' || !d.message) return map;
    const prev = map[d.tool];
    // Only acquisition-time warnings describe the tool itself (freshness probe
    // failed, working install kept). Warnings after `ready` come from the tool's
    // execution and belong to the log, not to its identity chip.
    if (!prev || prev.stage === 'ready') return map;
    return { ...map, [d.tool]: { ...prev, warning: d.message } };
  }
  return map;
}
