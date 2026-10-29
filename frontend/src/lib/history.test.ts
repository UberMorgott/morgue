import { describe, expect, it } from 'vitest';
import {
  LEGACY_HISTORY_KEY,
  LEGACY_IMPORTED_KEY,
  legacyOutputPaths,
  loadRecent,
  normalizeRecent,
  statusKey,
  statusTone,
  type HistoryBackend,
  type RecentRun,
} from './history';

function memStorage(init: Record<string, string> = {}) {
  const m = new Map(Object.entries(init));
  return {
    getItem: (k: string) => (m.has(k) ? (m.get(k) as string) : null),
    setItem: (k: string, v: string) => void m.set(k, v),
    map: m,
  };
}

function run(p: Partial<RecentRun>): RecentRun {
  return { path: 'D:\\out', input: '', runId: '', status: 'finished', outcome: 'success', startedAt: null, targets: 1, failed: 0, error: '', ...p };
}

describe('legacy import', () => {
  const legacy = JSON.stringify([
    { path: 'C:\\g\\a.exe', output: 'D:\\out\\a', success: true, timestamp: 1, kind: 'pe' },
    { path: 'C:\\g\\b.exe', output: '', success: false, timestamp: 2, kind: '' },
    { path: 'C:\\g\\c.exe', output: 'd:\\OUT\\A', success: true, timestamp: 3, kind: '' },
  ]);

  it('extracts only output paths, deduped, never outcomes', () => {
    expect(legacyOutputPaths(legacy)).toEqual(['D:\\out\\a']);
    expect(legacyOutputPaths('not json')).toEqual([]);
    expect(legacyOutputPaths(null)).toEqual([]);
  });

  it('imports once and keeps the old key', async () => {
    const calls: string[][] = [];
    const backend: HistoryBackend = {
      recent: async () => [run({ path: 'D:\\out\\a', status: 'interrupted' })],
      importLegacy: async (p) => (calls.push(p), p.length),
    };
    const st = memStorage({ [LEGACY_HISTORY_KEY]: legacy });
    const first = await loadRecent(backend, st);
    await loadRecent(backend, st);
    expect(calls).toEqual([['D:\\out\\a']]);
    expect(st.getItem(LEGACY_IMPORTED_KEY)).toBe('1');
    expect(st.getItem(LEGACY_HISTORY_KEY)).toBe(legacy);
    // status comes from the backend (disk), not from legacy success:true
    expect(first[0].status).toBe('interrupted');
  });

  it('backend failure yields an empty list, not stale data', async () => {
    const backend: HistoryBackend = {
      recent: async () => {
        throw new Error('no service');
      },
      importLegacy: async () => 0,
    };
    expect(await loadRecent(backend, memStorage())).toEqual([]);
  });
});

describe('status display', () => {
  it('never shows a failed or partial run as success', () => {
    expect(statusTone(run({}))).toBe('success');
    expect(statusKey(run({}))).toBe('');
    expect(statusTone(run({ failed: 2 }))).toBe('error');
    expect(statusKey(run({ outcome: 'error' }))).toBe('history.status.failed');
    expect(statusTone(run({ status: 'interrupted', outcome: '' }))).toBe('warning');
    expect(statusKey(run({ status: 'unavailable' }))).toBe('history.status.unavailable');
    expect(statusKey(run({ outcome: 'cancelled' }))).toBe('history.status.cancelled');
  });

  it('normalizes unknown rows and bounds the list', () => {
    const rows = normalizeRecent([{ path: 'x', status: 'weird' }, { nope: 1 }, ...Array.from({ length: 30 }, (_, i) => ({ path: `p${i}` }))]);
    expect(rows[0].status).toBe('unavailable');
    expect(rows.length).toBe(20);
    expect(normalizeRecent(null)).toEqual([]);
  });
});
