// Recent runs: workspace path references persisted by the backend in
// BaseDir()/state/recent-runs.json. Status always comes from disk via
// WorkspaceService; nothing here is a source of truth for outcomes.

import { WorkspaceService } from '../../bindings/github.com/UberMorgott/morgue/internal/services/index.js';

export type RecentStatus = 'running' | 'finished' | 'interrupted' | 'corrupt' | 'legacy' | 'unavailable';

export interface RecentRun {
  path: string; // workspace (output) root
  input: string; // run input path, '' when unknown
  runId: string;
  status: RecentStatus;
  outcome: string; // success | error | cancelled | ''
  startedAt: string | null;
  targets: number;
  failed: number;
  error: string;
}

export interface HistoryBackend {
  recent(): Promise<RecentRun[]>;
  importLegacy(paths: string[]): Promise<number>;
}

export const LEGACY_HISTORY_KEY = 'morgue-history';
export const LEGACY_IMPORTED_KEY = 'morgue-history-imported';
export const MAX_RECENT = 20;

/** Default backend: the generated Wails WorkspaceService bindings (no SSE inside the webview). */
export const wailsHistoryBackend: HistoryBackend = {
  async recent() {
    return normalizeRecent(await WorkspaceService.Recent());
  },
  async importLegacy(paths) {
    const n = await WorkspaceService.ImportLegacy(paths);
    return typeof n === 'number' ? n : 0;
  },
};

const STATUSES: RecentStatus[] = ['running', 'finished', 'interrupted', 'corrupt', 'legacy', 'unavailable'];

/** Validates backend rows; unknown/missing fields degrade to "unavailable". */
export function normalizeRecent(raw: unknown): RecentRun[] {
  if (!Array.isArray(raw)) return [];
  const out: RecentRun[] = [];
  for (const r of raw) {
    if (!r || typeof r !== 'object' || typeof (r as any).path !== 'string' || !(r as any).path) continue;
    const o = r as Record<string, unknown>;
    const status = STATUSES.includes(o.status as RecentStatus) ? (o.status as RecentStatus) : 'unavailable';
    out.push({
      path: o.path as string,
      input: typeof o.input === 'string' ? o.input : '',
      runId: typeof o.runId === 'string' ? o.runId : '',
      status,
      outcome: typeof o.outcome === 'string' ? o.outcome : '',
      startedAt: typeof o.startedAt === 'string' ? o.startedAt : null,
      targets: typeof o.targets === 'number' ? o.targets : 0,
      failed: typeof o.failed === 'number' ? o.failed : 0,
      error: typeof o.error === 'string' ? o.error : '',
    });
    if (out.length === MAX_RECENT) break;
  }
  return out;
}

/** Output paths from the v0.7 localStorage history; outcomes are ignored. */
export function legacyOutputPaths(raw: string | null): string[] {
  if (!raw) return [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const e of parsed) {
    const p = e && typeof e === 'object' ? (e as any).output : undefined;
    if (typeof p !== 'string' || !p.trim()) continue;
    const key = p.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(p);
    if (out.length === MAX_RECENT) break;
  }
  return out;
}

interface KV {
  getItem(k: string): string | null;
  setItem(k: string, v: string): void;
}

function safeStorage(): KV | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage;
  } catch {
    return null;
  }
}

/**
 * Imports legacy localStorage paths once (the old key is kept in v0.8), then
 * returns the disk-resolved recent list. Backend failure yields [].
 */
export async function loadRecent(backend: HistoryBackend = wailsHistoryBackend, storage: KV | null = safeStorage()): Promise<RecentRun[]> {
  try {
    if (storage && storage.getItem(LEGACY_IMPORTED_KEY) !== '1') {
      const paths = legacyOutputPaths(storage.getItem(LEGACY_HISTORY_KEY));
      if (paths.length > 0) await backend.importLegacy(paths);
      storage.setItem(LEGACY_IMPORTED_KEY, '1');
    }
  } catch {
    // import retried next launch; the backend also guards against a second import
  }
  try {
    return normalizeRecent(await backend.recent());
  } catch {
    return [];
  }
}

export type StatusTone = 'success' | 'error' | 'warning' | 'muted' | 'running';

/** Visual tone; a finished run with failures never shows as success. */
export function statusTone(r: RecentRun): StatusTone {
  switch (r.status) {
    case 'running':
      return 'running';
    case 'finished':
      return r.outcome === 'success' && r.failed === 0 ? 'success' : r.outcome === 'cancelled' ? 'warning' : 'error';
    case 'interrupted':
    case 'corrupt':
      return 'warning';
    default:
      return 'muted';
  }
}

/** i18n key for the status tag, or '' when the dot alone says it (clean success). */
export function statusKey(r: RecentRun): string {
  if (r.status === 'finished') {
    if (r.outcome === 'success' && r.failed === 0) return '';
    return r.outcome === 'cancelled' ? 'history.status.cancelled' : 'history.status.failed';
  }
  return `history.status.${r.status}`;
}

export function basename(p: string): string {
  return p.split(/[\\/]/).filter(Boolean).pop() || p;
}
