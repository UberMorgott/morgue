import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
// Golden fixture shared with the Go contract tests (internal/runmodel).
import goldenRaw from '../../../internal/runmodel/testdata/run-started.json?raw';
import { parseEventV2, type EventV2 } from './events';
import {
  pipelineState, resetPipeline, startPipeline, applyEventV2, updateFromEvent,
  elapsedMs, formatElapsed, targetTree,
} from './pipeline';

const goldenRunStarted = goldenRaw.trim();

const RUN = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const RUN2 = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
const T0 = '2026-10-29T00:00:00Z';
const at = (sec: number) => new Date(Date.parse(T0) + sec * 1000).toISOString();
const zero = { total: 0, pending: 0, running: 0, success: 0, error: 0, cancelled: 0, skipped: 0 };

function ev(seq: number, type: string, data: any, extra: Partial<EventV2> = {}, run = RUN): EventV2 {
  return { v: 2, ts: at(seq), seq, run, type, data, ...extra };
}

/** NSIS installer → extracted .NET child → ConfuserEx-extracted grandchild, plus a same-basename sibling. */
function nestedStream(outcome: 'success' | 'error' = 'error'): EventV2[] {
  const golden = JSON.parse(goldenRunStarted) as EventV2;
  return [
    golden,
    ev(2, 'scan.completed', { filesTotal: 2, groupsTotal: 1 }),
    ev(3, 'target.discovered', { inputPath: 'C:/g/setup.exe', relativeInput: 'setup.exe', kind: 'Installer', recipe: 'nsis', output: 'setup', sizeBytes: 100 }, { targetId: 't0000000000000001' }),
    ev(4, 'target.discovered', { inputPath: 'C:/g/sub/setup.exe', relativeInput: 'sub/setup.exe', kind: 'Installer', recipe: 'nsis', output: 'sub/setup', sizeBytes: 50 }, { targetId: 't0000000000000002' }),
    ev(5, 'target.discovered', { inputPath: 'C:/x/App.dll', relativeInput: 'setup.exe/App.dll', kind: 'Managed', recipe: 'dotnet-confuserex', output: 'setup/App', sizeBytes: 10, obfuscator: 'ConfuserEx', deobfuscator: 'de4dot', toolsNeeded: ['de4dot', 'ilspycmd'] }, { targetId: 't0000000000000003', parentTargetId: 't0000000000000001' }),
    ev(6, 'tool.progress', { tool: 'ilspycmd', stage: 'download', bytes: 50, bytesTotal: 200, version: '9.1', error: '' }),
    ev(7, 'tool.progress', { tool: 'ilspycmd', stage: 'ready', bytes: 200, bytesTotal: 200, version: '9.1', error: '' }),
    ev(8, 'target.discovered', { inputPath: 'C:/x/Core.dll', relativeInput: 'setup.exe/App.dll/Core.dll', kind: 'Managed', recipe: 'dotnet-generic', output: 'setup/App/Core', sizeBytes: 5 }, { targetId: 't0000000000000004', parentTargetId: 't0000000000000003' }),
    ev(9, 'step.progress', { stepId: 's1', name: 'Decompiling 3/10', tool: 'ilspycmd', unit: 'types', stepIndex: 0, stepTotal: 2, count: 3, countTotal: 10, durationMs: 0, status: 'running', error: '' }, { targetId: 't0000000000000004', parentTargetId: 't0000000000000003' }),
    ev(10, 'step.progress', { stepId: 's1', name: 'Decompile', tool: 'ilspycmd', unit: 'types', stepIndex: 0, stepTotal: 2, count: 10, countTotal: 10, durationMs: 900, status: 'success', error: '' }, { targetId: 't0000000000000004', parentTargetId: 't0000000000000003' }),
    ev(11, 'target.finished', { outcome: 'error', recipe: 'dotnet-generic', output: 'setup/App/Core', error: 'ilspy crashed', skipReason: '', durationMs: 1000 }, { targetId: 't0000000000000004', parentTargetId: 't0000000000000003' }),
    ev(12, 'target.finished', { outcome: 'error', recipe: 'dotnet-confuserex', output: 'setup/App', error: 'child failed', skipReason: '', durationMs: 2000 }, { targetId: 't0000000000000003', parentTargetId: 't0000000000000001' }),
    ev(13, 'target.finished', { outcome: 'error', recipe: 'nsis', output: 'setup', error: 'child failed', skipReason: '', durationMs: 3000 }, { targetId: 't0000000000000001' }),
    ev(14, 'target.finished', { outcome: 'skipped', recipe: '', output: '', error: '', skipReason: 'no recipe', durationMs: 0 }, { targetId: 't0000000000000002' }),
    ev(15, 'run.finished', {
      outcome, finishedAt: at(15), durationMs: 15000, error: outcome === 'error' ? '1 target failed' : '',
      targets: { ...zero, total: 4, error: 3, skipped: 1 }, leafTargets: { ...zero, total: 2, error: 1, skipped: 1 },
    }),
  ];
}

describe('parseEventV2', () => {
  it('accepts the golden run.started fixture and the Wails wrapper', () => {
    expect(parseEventV2(goldenRunStarted)?.type).toBe('run.started');
    const env = JSON.parse(goldenRunStarted);
    expect(parseEventV2(env)?.run).toBe(RUN);
    expect(parseEventV2({ name: 'pipeline:event:v2', data: env })?.seq).toBe(1);
    expect(parseEventV2({ name: 'pipeline:event:v2', data: [env] })?.seq).toBe(1);
  });

  it('rejects other major versions and malformed envelopes', () => {
    const env = JSON.parse(goldenRunStarted);
    expect(parseEventV2({ ...env, v: 1 })).toBeNull();
    expect(parseEventV2({ ...env, v: 3 })).toBeNull();
    expect(parseEventV2({ ...env, seq: 0 })).toBeNull();
    expect(parseEventV2({ ...env, run: 'ABC' })).toBeNull();
    expect(parseEventV2({ Phase: 'scan' })).toBeNull();
  });
});

describe('events v2 reducer', () => {
  beforeEach(() => resetPipeline());

  it('is idempotent: the identical golden stream fed twice yields the same state', () => {
    for (const e of nestedStream()) expect(applyEventV2(e)).not.toBe('duplicate');
    const once = get(pipelineState);
    const replay = nestedStream().map(applyEventV2);
    expect(replay.every(r => r === 'duplicate')).toBe(true);
    expect(get(pipelineState)).toEqual(once);
    expect(once.lastSeq).toBe(15);
    expect(once.targets).toHaveLength(4);
  });

  it('rejects a stale seq within the current run', () => {
    const s = nestedStream();
    applyEventV2(s[0]);
    applyEventV2(s[2]);
    expect(applyEventV2(s[1])).toBe('duplicate');
    expect(get(pipelineState).filesTotal).toBe(0);
  });

  it('a new run ID resets state; a stray event of another run cannot clobber a live run', () => {
    const s = nestedStream();
    s.slice(0, 5).forEach(applyEventV2);
    expect(get(pipelineState).targets).toHaveLength(3);
    expect(applyEventV2(ev(9, 'log', { level: 'info', tool: '', message: 'late' }, {}, RUN2))).toBe('stale-run');
    expect(get(pipelineState).runId).toBe(RUN);
    applyEventV2(ev(1, 'run.started', { handle: { runId: RUN2, startedAt: at(100), output: 'o2' }, spec: { version: 1, path: 'other' } }, { ts: at(100) }, RUN2));
    const st = get(pipelineState);
    expect(st.runId).toBe(RUN2);
    expect(st.targets).toHaveLength(0);
    expect(st.reconResults).toHaveLength(0);
    expect(st.lastSeq).toBe(1);
    expect(st.inputPath).toBe('other');
  });

  it('an API-started run adopts the backend startedAt instead of the local clock', () => {
    startPipeline('game');
    expect(get(pipelineState).startedAt).not.toBe(Date.parse(T0));
    applyEventV2(JSON.parse(goldenRunStarted));
    const st = get(pipelineState);
    expect(st.startedAt).toBe(Date.parse(T0));
    expect(st.runId).toBe(RUN);
    expect(st.outputPath).toBe('out');
    expect(elapsedMs(st, Date.parse(T0) + 42_000)).toBe(42_000);
  });

  it('freezes elapsed at the backend duration after run.finished', () => {
    nestedStream().forEach(applyEventV2);
    const st = get(pipelineState);
    expect(elapsedMs(st, Date.now())).toBe(15_000);
    expect(formatElapsed(elapsedMs(st))).toBe('15s');
  });

  it('terminal failure is never shown as a green success', () => {
    nestedStream('error').forEach(applyEventV2);
    expect(get(pipelineState).phase).toBe('error');
    expect(get(pipelineState).error).toBe('1 target failed');
    // A legacy Done(success) after the v2 terminal must not flip it.
    updateFromEvent({ data: [{ Phase: 'done', Done: true, RunOutcome: 'success' }] });
    expect(get(pipelineState).phase).toBe('error');

    resetPipeline();
    // Inconsistent producer: success outcome with failed targets → still error.
    nestedStream('success').forEach(applyEventV2);
    expect(get(pipelineState).phase).toBe('error');
    expect(get(pipelineState).outcome).toBe('success');
  });

  it('maps cancelled and clean success outcomes', () => {
    applyEventV2(JSON.parse(goldenRunStarted));
    applyEventV2(ev(2, 'run.finished', { outcome: 'cancelled', finishedAt: at(2), durationMs: 2000, error: '', targets: zero, leafTargets: zero }));
    expect(get(pipelineState).phase).toBe('cancelled');
    resetPipeline();
    applyEventV2(JSON.parse(goldenRunStarted));
    applyEventV2(ev(2, 'run.finished', { outcome: 'success', finishedAt: at(2), durationMs: 2000, error: '', targets: zero, leafTargets: zero }));
    expect(get(pipelineState).phase).toBe('done');
  });

  it('progress comes from typed fields, not human text', () => {
    const s = nestedStream();
    s.slice(0, 6).forEach(applyEventV2);
    let st = get(pipelineState);
    expect(st.phase).toBe('tools');
    expect(st.downloadingTool).toBe('ilspycmd');
    expect(st.downloadProgress).toBe(25);
    expect(st.downloadBytes).toBe(50);
    s.slice(6, 9).forEach(applyEventV2);
    st = get(pipelineState);
    expect(st.phase).toBe('execute');
    expect(st.toolsInstalled).toContain('ilspycmd');
    expect(st.execCounters['ilspycmd']).toMatchObject({ count: 3, countTotal: 10, unit: 'types' });
    // A log whose text looks like progress does not move counters.
    applyEventV2(ev(10, 'log', { level: 'info', tool: 'ilspycmd', message: 'Downloading ilspycmd... 99% 9 / 10 MB' }));
    st = get(pipelineState);
    expect(st.downloadProgress).toBe(25);
    expect(st.execCounters['ilspycmd'].count).toBe(3);
  });

  it('legacy events cannot double-count once v2 is active', () => {
    const s = nestedStream();
    s.slice(0, 9).forEach(applyEventV2);
    updateFromEvent({ data: [{ Phase: 'execute', Tool: 'ilspycmd', Progress: { Step: 0, Total: 2, Tool: 'ilspycmd', Status: 'Success', Count: 10, Unit: 'types' } }] });
    updateFromEvent({ data: [{ Phase: 'recon', Target: 'C:/x/App.dll', ReconKind: 'Managed', Obfuscator: 'ConfuserEx' }] });
    const st = get(pipelineState);
    expect(st.execCounters['ilspycmd'].count).toBe(3);
    expect(st.reconResults).toHaveLength(4);
    expect(st.obfuscations).toEqual([{ name: 'ConfuserEx', deobfuscator: 'de4dot', affectedFiles: ['App.dll'] }]);
    updateFromEvent({ data: [{ Phase: 'stats', OutputStats: ['12 files'] }] });
    expect(get(pipelineState).outputStats).toEqual(['12 files']);
  });

  it('keeps nested targets, duplicate basenames and child failures distinct', () => {
    nestedStream().forEach(applyEventV2);
    const st = get(pipelineState);
    const rows = targetTree(st.targets);
    expect(rows.map(r => [r.target.id, r.depth])).toEqual([
      ['t0000000000000001', 0], ['t0000000000000003', 1], ['t0000000000000004', 2], ['t0000000000000002', 0],
    ]);
    const byId = Object.fromEntries(st.targets.map(t => [t.id, t]));
    expect(byId['t0000000000000004']).toMatchObject({ status: 'error', error: 'ilspy crashed' });
    expect(byId['t0000000000000001'].status).toBe('error');
    expect(byId['t0000000000000002']).toMatchObject({ status: 'skipped', skipReason: 'no recipe' });
    expect(st.reconResults.filter(r => r.file === 'setup.exe')).toHaveLength(2);
    expect(st.reconResults.find(r => r.targetId === 't0000000000000002')?.kind).toBe('Skipped');
    expect(st.leafCounts?.error).toBe(1);
  });
});
