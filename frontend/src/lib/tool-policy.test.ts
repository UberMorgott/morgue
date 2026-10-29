import { describe, it, expect } from 'vitest';
import {
  toToolView, toolState, primaryAction, canDelete, canMutate, headerSummary, isInUseError,
  reduceRunTools, parseFreshness, viewChanged, stateKey, type RawToolStatus, type RunToolInfo,
} from './tool-policy';
import { t } from './i18n';

const base: RawToolStatus = { Name: 'ilspycmd', Installed: true, Version: '9.1.0', Freshness: 'current' };
const view = (o: Partial<RawToolStatus> = {}) => toToolView({ ...base, ...o });

describe('tool state', () => {
  it('distinguishes missing, failed and stale-check-failed', () => {
    expect(toolState(view({ Installed: false }))).toBe('missing');
    expect(toolState(view({ Freshness: 'error' }))).toBe('failed');
    expect(toolState(view({ Installed: false, Freshness: 'error' }))).toBe('failed');
    expect(toolState(view({ Freshness: 'warning', FreshnessWarning: 'offline' }))).toBe('stale');
  });
  it('pin wins over update availability and freshness', () => {
    const v = view({ OperatorVersion: '8.2.0', UpdateAvailable: true, Freshness: 'warning' });
    expect(toolState(v)).toBe('pinned');
    expect(primaryAction(v)).toBe('unpin');
    expect(headerSummary([v])).toEqual({ updates: 0, attention: 0 });
  });
  it('system tools are neither updated nor deleted', () => {
    const v = view({ Freshness: 'system', UpdateAvailable: true });
    expect(toolState(v)).toBe('system');
    expect(primaryAction(v)).toBeNull();
    expect(canDelete(v)).toBe(false);
  });
  it('maps actions', () => {
    expect(primaryAction(view({ Installed: false }))).toBe('download');
    expect(primaryAction(view({ Freshness: 'error' }))).toBe('repair');
    expect(primaryAction(view({ UpdateAvailable: true }))).toBe('update');
    expect(primaryAction(view())).toBeNull();
  });
  it('in-use tools cannot be mutated', () => {
    const v = view({ InUse: true });
    expect(canMutate(v)).toBe(false);
    expect(canDelete(v)).toBe(false);
  });
  it('unknown freshness is unchecked, never current', () => {
    expect(parseFreshness('latest')).toBe('unchecked');
    expect(toolState(view({ Freshness: undefined }))).toBe('unchecked');
  });
  it('drops a stale update hint once the installed version changes', () => {
    const before = { ...view(), latestVersion: '9.2.0', updateAvailable: true };
    expect(toToolView({ ...base }, before).updateAvailable).toBe(true);
    const after = toToolView({ ...base, Version: '9.2.0' }, before);
    expect(after.updateAvailable).toBe(false);
    expect(after.latestVersion).toBe('');
    expect(viewChanged(before, after)).toBe(true);
  });
  it('header counts updates and attention separately', () => {
    expect(headerSummary([
      view({ UpdateAvailable: true }), view({ Freshness: 'error' }), view({ Freshness: 'warning' }),
      view({ Installed: false }), view(),
    ])).toEqual({ updates: 1, attention: 2 });
  });
  it('recognizes backend in-use/busy errors', () => {
    expect(isInUseError(new Error('delete ghidra: tool is in use'))).toBe(true);
    expect(isInUseError('install busy')).toBe(true);
    expect(isInUseError(new Error('404 not found'))).toBe(false);
  });
  it('every state has en and ru strings', () => {
    for (const s of ['failed', 'missing', 'pinned', 'system', 'checking', 'stale', 'outdated', 'updated', 'current', 'unchecked'] as const) {
      expect(t('en', stateKey(s))).not.toBe(stateKey(s));
      expect(t('ru', stateKey(s))).not.toBe(t('en', stateKey(s)));
    }
  });
});

describe('run tool reducer', () => {
  const tp = (stage: string, extra: Record<string, unknown> = {}) =>
    ({ type: 'tool.progress', data: { tool: 'ghidra', stage, bytes: 0, bytesTotal: 0, version: '', error: '', ...extra } });

  it('records the exact version only when ready, and that a payload was fetched', () => {
    let m: Record<string, RunToolInfo> = {};
    m = reduceRunTools(m, tp('check'));
    expect(m.ghidra).toMatchObject({ stage: 'checking', version: '', fetched: false });
    m = reduceRunTools(m, tp('download', { version: '11.4' }));
    expect(m.ghidra.version).toBe('');
    m = reduceRunTools(m, tp('ready', { version: '11.4' }));
    expect(m.ghidra).toMatchObject({ stage: 'ready', version: '11.4', fetched: true, error: '' });
  });
  it('keeps acquisition warnings, ignores execution warnings', () => {
    let m = reduceRunTools({}, tp('check'));
    m = reduceRunTools(m, { type: 'log', data: { level: 'warn', tool: 'ghidra', message: 'update check failed; using installed 11.3' } });
    m = reduceRunTools(m, tp('ready', { version: '11.3' }));
    expect(m.ghidra).toMatchObject({ version: '11.3', fetched: false, warning: 'update check failed; using installed 11.3' });
    const after = reduceRunTools(m, { type: 'log', data: { level: 'warn', tool: 'ghidra', message: '3 functions failed' } });
    expect(after).toBe(m);
  });
  it('records failures and ignores unrelated events', () => {
    const m = reduceRunTools({}, tp('failed', { error: 'hash mismatch' }));
    expect(m.ghidra).toMatchObject({ stage: 'failed', error: 'hash mismatch' });
    expect(reduceRunTools(m, { type: 'step.progress', data: {} })).toBe(m);
    expect(reduceRunTools(m, tp('bogus'))).toBe(m);
  });
});
