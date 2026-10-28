import { writable, derived } from 'svelte/store';
import type {
  EventV2, RunStarted, ScanCompleted, TargetDiscovered, TargetFinished, StepProgress,
  ToolProgress, LogRecord, RunFinished, RunOutcome, Counts,
} from './events';


export type PipelinePhase = 'idle' | 'analysis' | 'tools' | 'execute' | 'done' | 'error' | 'cancelled';

export interface PipelineTarget {
  path: string;
  kind?: string;
  recipe?: string;
  status: 'pending' | 'running' | 'done' | 'error' | 'skipped' | 'cancelled';
  error?: string;
  // events v2 identity (absent for legacy-only targets)
  id?: string;
  parentTargetId?: string;
  relativeInput?: string;
  output?: string;
  sizeBytes?: number;
  skipReason?: string;
  durationMs?: number;
}

export interface PipelineState {
  // events v2 run identity; '' until run.started of a v2 engine arrives
  runId: string;
  lastSeq: number;
  /** v2 terminal outcome; '' while running or for legacy-only runs */
  outcome: RunOutcome | '';
  /** backend-measured duration, frozen at run.finished; 0 while running */
  durationMs: number;
  leafCounts: Counts | null;
  phase: PipelinePhase;
  paused: boolean;
  inputPath: string;
  outputPath: string;
  targets: PipelineTarget[];
  step: number;
  stepTotal: number;
  stepName: string;
  progress: number; // 0-100
  error: string;
  currentTarget: string;   // current file being processed
  lastMessage: string;     // last status message from engine
  startedAt: number;       // timestamp when pipeline started
  scanInfo: string;          // "Found 3 files in 1 group"
  filesTotal: number;        // targets the scan found; 0 means nothing to decompile
  reconResults: Array<{ file: string; kind: string; reconKind: string; compiler: string; obfuscator: string; size: number; targetId?: string }>;  // classification results
  toolsInfo: string;         // "All tools ready" or "Installing ilspycmd..."
  logs: string[];            // last N log messages (keep max 20)
  outputStats: string[];     // post-execution file statistics
  // Enriched fields from backend events
  reconKind: string;         // "Managed", "Native", "UnrealEngine"
  compiler: string;          // "Delphi", "Go", ""
  obfuscator: string;        // "ConfuserEx", ""
  fileSize: number;          // bytes
  recipeName: string;        // "dotnet-generic"
  recipeDesc: string;        // "Decompile clean .NET assembly"
  toolsNeeded: string[];     // ["ilspycmd", "strings"]
  toolsInstalled: string[];  // tools that finished installing
  downloadingTool: string;   // current tool being downloaded
  downloadProgress: number;  // 0-100 or -1 for indeterminate (extracting)
  obfuscations: Array<{ name: string; deobfuscator: string | null; affectedFiles: string[] }>;
  downloadBytes: number;       // bytes downloaded so far
  downloadTotalBytes: number;  // total bytes to download
  execCounters: Record<string, { count: number; unit: string; countTotal: number; _completed?: number; _done?: boolean }>;  // per-tool progress counters
  toolStatus: Record<string, 'success' | 'skipped' | 'failed'>;  // per-tool final outcome
  toolOrder: Record<string, number>;  // per-tool first step index, for display ordering
  currentTool: string;  // tool currently executing
}

const initial: PipelineState = {
  runId: '',
  lastSeq: 0,
  outcome: '',
  durationMs: 0,
  leafCounts: null,
  phase: 'idle',
  paused: false,
  inputPath: '',
  outputPath: '',
  targets: [],
  step: 0,
  stepTotal: 0,
  stepName: '',
  progress: 0,
  error: '',
  currentTarget: '',
  lastMessage: '',
  startedAt: 0,
  scanInfo: '',
  filesTotal: 0,
  reconResults: [],
  toolsInfo: '',
  logs: [],
  outputStats: [],
  reconKind: '',
  compiler: '',
  obfuscator: '',
  fileSize: 0,
  recipeName: '',
  recipeDesc: '',
  toolsNeeded: [],
  toolsInstalled: [],
  downloadingTool: '',
  downloadProgress: 0,
  obfuscations: [],
  downloadBytes: 0,
  downloadTotalBytes: 0,
  execCounters: {},
  toolStatus: {},
  toolOrder: {},
  currentTool: '',
};

export const pipelineState = writable<PipelineState>({ ...initial });

export const isRunning = derived(pipelineState, $s =>
  $s.phase !== 'idle' && $s.phase !== 'done' && $s.phase !== 'error' && $s.phase !== 'cancelled'
);

export function resetPipeline() {
  pipelineState.set({ ...initial });
}

/**
 * Local optimistic start (GUI-started run, before the backend admits it).
 * startedAt here is provisional: run.started replaces it with the backend clock.
 */
export function startPipeline(inputPath: string) {
  pipelineState.set({ ...initial, phase: 'analysis', inputPath, startedAt: Date.now() });
}

const isTerminal = (p: PipelinePhase) => p === 'done' || p === 'error' || p === 'cancelled';

function parseTS(ts: string | undefined): number {
  const n = ts ? Date.parse(ts) : NaN;
  return Number.isFinite(n) ? n : 0;
}

/**
 * Elapsed run time in ms: frozen backend duration once finished, otherwise
 * now minus the backend-owned start (never the browser arrival time of events).
 */
export function elapsedMs(s: Pick<PipelineState, 'startedAt' | 'durationMs' | 'phase' | 'outcome'>, now: number = Date.now()): number {
  if (s.outcome) return s.durationMs;
  if (s.startedAt <= 0) return 0;
  return Math.max(0, now - s.startedAt);
}

export function formatElapsed(ms: number): string {
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`;
  return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
}

export interface TargetRow { target: PipelineTarget; depth: number }

/** Flatten targets into display order: each child directly under its parent. */
export function targetTree(targets: PipelineTarget[]): TargetRow[] {
  const ids = new Set(targets.filter(t => t.id).map(t => t.id));
  const children = new Map<string, PipelineTarget[]>();
  const roots: PipelineTarget[] = [];
  for (const t of targets) {
    const p = t.parentTargetId;
    if (p && ids.has(p)) {
      const list = children.get(p) ?? [];
      list.push(t);
      children.set(p, list);
    } else roots.push(t);
  }
  const out: TargetRow[] = [];
  const seen = new Set<PipelineTarget>();
  const walk = (t: PipelineTarget, depth: number) => {
    if (seen.has(t)) return;
    seen.add(t);
    out.push({ target: t, depth });
    for (const c of (t.id && children.get(t.id)) || []) walk(c, depth + 1);
  };
  for (const r of roots) walk(r, 0);
  return out;
}

const targetStatus: Record<string, PipelineTarget['status']> = {
  success: 'done', error: 'error', cancelled: 'cancelled', skipped: 'skipped',
};

function upsertTarget(list: PipelineTarget[], id: string, patch: Partial<PipelineTarget>): PipelineTarget[] {
  const i = list.findIndex(t => t.id === id);
  const out = [...list];
  if (i >= 0) out[i] = { ...out[i], ...patch };
  else out.push({ path: patch.path ?? id, status: 'pending', id, ...patch });
  return out;
}

export type ApplyResult = 'applied' | 'duplicate' | 'stale-run' | 'ignored';

/**
 * Reduce one events-v2 envelope into the store. Rejects duplicate/stale seq of
 * the current run; a different run ID resets state (only run.started may open
 * a new run — a stray event of an older run is ignored once a newer run is seen).
 */
export function applyEventV2(ev: EventV2): ApplyResult {
  let result: ApplyResult = 'applied';
  pipelineState.update(s => {
    const r = reduceEventV2(s, ev);
    result = r.result;
    return r.state;
  });
  return result;
}

export function reduceEventV2(s: PipelineState, ev: EventV2): { state: PipelineState; result: ApplyResult } {
  let next: PipelineState;
  if (ev.run !== s.runId) {
    if (ev.type !== 'run.started') {
      // Mid-run attach (reconnect / API-started run whose run.started we missed):
      // adopt only if we hold no v2 run, or ours is finished. Never let a stale
      // run's late event clobber a live one.
      if (s.runId && !isTerminal(s.phase)) return { state: s, result: 'stale-run' };
    }
    next = {
      ...initial,
      // keep a GUI-local optimistic start's input until run.started overrides it
      inputPath: s.runId ? '' : s.inputPath,
      startedAt: 0,
      phase: 'analysis',
      runId: ev.run,
      lastSeq: 0,
    };
  } else {
    if (ev.seq <= s.lastSeq) return { state: s, result: 'duplicate' };
    next = { ...s };
  }
  next.lastSeq = ev.seq;
  // Once terminal, only the terminal record itself (already applied) matters.
  if (isTerminal(next.phase) && next.outcome) return { state: next, result: 'ignored' };

  const tid = ev.targetId || '';
  switch (ev.type) {
    case 'run.started': {
      const d = ev.data as RunStarted;
      next.startedAt = parseTS(d.handle?.startedAt) || parseTS(ev.ts);
      next.inputPath = d.spec?.path || next.inputPath;
      next.outputPath = d.handle?.output || d.spec?.output || next.outputPath;
      next.phase = 'analysis';
      break;
    }
    case 'scan.completed': {
      const d = ev.data as ScanCompleted;
      next.filesTotal = d.filesTotal ?? 0;
      break;
    }
    case 'target.discovered': {
      const d = ev.data as TargetDiscovered;
      if (!tid) break;
      const known = next.targets.some(t => t.id === tid);
      next.targets = upsertTarget(next.targets, tid, {
        path: d.inputPath, relativeInput: d.relativeInput, kind: d.kind, recipe: d.recipe || undefined,
        output: d.output, sizeBytes: d.sizeBytes, parentTargetId: ev.parentTargetId || undefined,
      });
      if (!next.startedAt) next.startedAt = parseTS(ev.ts);
      if (!known) applyComposition(next, d, tid);
      break;
    }
    case 'target.finished': {
      const d = ev.data as TargetFinished;
      if (!tid) break;
      const prev = next.targets.find(t => t.id === tid);
      next.targets = upsertTarget(next.targets, tid, {
        status: targetStatus[d.outcome] ?? 'error',
        recipe: d.recipe || prev?.recipe, output: d.output || prev?.output,
        error: d.error || undefined, skipReason: d.skipReason || undefined, durationMs: d.durationMs,
        parentTargetId: ev.parentTargetId || prev?.parentTargetId,
      });
      const kindLabel = d.outcome === 'skipped' ? 'Skipped' : d.recipe;
      if (kindLabel) {
        next.reconResults = next.reconResults.map(r => r.targetId === tid ? { ...r, kind: kindLabel } : r);
      }
      if (d.outcome === 'error') {
        const label = prev?.relativeInput || prev?.path || tid;
        next.logs = [...next.logs.slice(-19), `ERROR ${label}: ${d.error || 'failed'}`];
      }
      break;
    }
    case 'step.progress': {
      const p = ev.data as StepProgress;
      applyStep(next, p, tid);
      break;
    }
    case 'tool.progress': {
      const p = ev.data as ToolProgress;
      if (next.phase === 'analysis') next.phase = 'tools';
      if (p.tool && !next.toolsNeeded.includes(p.tool)) next.toolsNeeded = [...next.toolsNeeded, p.tool];
      if (p.stage === 'download') {
        next.downloadingTool = p.tool;
        next.downloadBytes = p.bytes;
        next.downloadTotalBytes = p.bytesTotal;
        next.downloadProgress = p.bytesTotal > 0 ? Math.round((p.bytes / p.bytesTotal) * 100) : -1;
      } else if (p.stage === 'extract') {
        next.downloadingTool = p.tool;
        next.downloadProgress = -1;
      } else if (p.stage === 'ready') {
        if (!next.toolsInstalled.includes(p.tool)) next.toolsInstalled = [...next.toolsInstalled, p.tool];
        if (next.downloadingTool === p.tool) next.downloadingTool = '';
      } else if (p.stage === 'failed') {
        if (next.downloadingTool === p.tool) next.downloadingTool = '';
        next.logs = [...next.logs.slice(-19), `ERROR ${p.tool}: ${p.error || 'install failed'}`];
      }
      break;
    }
    case 'log': {
      const d = ev.data as LogRecord;
      if (d.message) {
        next.logs = [...next.logs.slice(-19), d.level === 'error' ? `ERROR ${d.message}` : d.message];
        next.lastMessage = d.message;
      }
      break;
    }
    case 'run.finished': {
      const d = ev.data as RunFinished;
      next.outcome = d.outcome;
      next.durationMs = d.durationMs ?? 0;
      next.leafCounts = d.leafTargets ?? null;
      // A run with any failed target is never shown as a green success, even
      // if an inconsistent producer said otherwise.
      const anyError = (d.targets?.error ?? 0) > 0 || (d.leafTargets?.error ?? 0) > 0
        || next.targets.some(t => t.status === 'error');
      if (d.outcome === 'error' || (d.outcome === 'success' && anyError)) {
        next.phase = 'error';
        next.error = d.error || next.targets.find(t => t.status === 'error')?.error || 'Pipeline failed';
      } else if (d.outcome === 'cancelled') {
        next.phase = 'cancelled';
      } else if (d.outcome === 'success') {
        next.phase = 'done';
      } else {
        next.phase = 'error';
        next.error = d.error || `unknown run outcome: ${String(d.outcome)}`;
      }
      next.paused = false;
      next.progress = 100;
      next.currentTool = '';
      const fin = { ...next.execCounters };
      for (const k of Object.keys(fin)) fin[k] = { ...fin[k], _done: true };
      next.execCounters = fin;
      break;
    }
    default:
      // artifact.written / quality.updated / unknown types: retained by the
      // journal, nothing to project in the M1 UI.
      return { state: next, result: 'ignored' };
  }
  return { state: next, result: 'applied' };
}

/** Composition/stats data from typed target.discovered fields (no message parsing). */
function applyComposition(next: PipelineState, d: TargetDiscovered, id: string) {
  const src = d.relativeInput || d.inputPath || '';
  const file = src.split(/[\\/]/).pop() || src;
  next.reconResults = [...next.reconResults, {
    file, kind: d.recipe || '', reconKind: d.kind || '', compiler: d.compiler || '',
    obfuscator: d.obfuscator || '', size: d.sizeBytes || 0, targetId: id,
  }];
  if (d.kind) next.reconKind = d.kind;
  if (d.compiler) next.compiler = d.compiler;
  if (d.obfuscator) next.obfuscator = d.obfuscator;
  if (d.sizeBytes) next.fileSize = (next.fileSize || 0) + d.sizeBytes;
  if (d.recipe) next.recipeName = d.recipe;
  if (d.recipeDesc) next.recipeDesc = d.recipeDesc;
  if (d.toolsNeeded?.length) next.toolsNeeded = [...new Set([...next.toolsNeeded, ...d.toolsNeeded])];
  if (d.obfuscator && file) {
    const existing = next.obfuscations.find(o => o.name === d.obfuscator);
    if (!existing) {
      next.obfuscations = [...next.obfuscations, { name: d.obfuscator, deobfuscator: d.deobfuscator || null, affectedFiles: [file] }];
    } else if (!existing.affectedFiles.includes(file)) {
      next.obfuscations = next.obfuscations.map(o => o === existing ? { ...o, affectedFiles: [...o.affectedFiles, file] } : o);
    }
  }
}

const stepStatusLegacy: Record<string, 'success' | 'skipped' | 'failed'> = {
  success: 'success', skipped: 'skipped', failed: 'failed', warn: 'success',
};

function applyStep(next: PipelineState, p: StepProgress, tid: string) {
  next.phase = 'execute';
  if (tid) {
    const t = next.targets.find(x => x.id === tid);
    if (t) {
      next.currentTarget = t.path;
      if (t.status === 'pending') next.targets = upsertTarget(next.targets, tid, { status: 'running' });
    }
  }
  next.step = p.stepIndex;
  next.stepTotal = p.stepTotal;
  const done = p.status === 'success' || p.status === 'skipped' || p.status === 'failed' || p.status === 'warn';
  next.stepName = done ? '' : p.name;
  next.progress = p.stepTotal > 0 ? Math.round(((p.stepIndex + (done ? 1 : 0)) / p.stepTotal) * 100) : 0;
  const tool = p.tool;
  if (!tool) return;
  if (p.status === 'running') next.currentTool = tool;
  if (next.toolOrder[tool] === undefined || p.stepIndex < next.toolOrder[tool]) {
    next.toolOrder = { ...next.toolOrder, [tool]: p.stepIndex };
  }
  const prev = next.execCounters[tool] || { count: 0, unit: p.unit, countTotal: 0, _completed: 0 };
  if (p.status === 'running' && p.unit) {
    next.execCounters = { ...next.execCounters, [tool]: {
      count: (prev._completed || 0) + p.count, unit: p.unit, countTotal: p.countTotal, _completed: prev._completed || 0,
    } };
  } else if (done) {
    const isSize = (p.unit || prev.unit) === 'KB';
    const completed = isSize ? (p.count > 0 ? p.count : prev._completed || 0) : (prev._completed || 0) + Math.max(0, p.count);
    next.execCounters = { ...next.execCounters, [tool]: {
      count: completed, unit: p.unit || prev.unit || '', countTotal: 0, _completed: completed,
    } };
    next.toolStatus = { ...next.toolStatus, [tool]: stepStatusLegacy[p.status] };
    if (p.status === 'failed' && p.error) next.logs = [...next.logs.slice(-19), `ERROR ${tool}: ${p.error}`];
    if (next.currentTool === tool) next.currentTool = '';
  }
}

/**
 * Legacy `pipeline:progress` reducer. Once a v2 run is active (runId set) the
 * v2 stream is authoritative for lifecycle, targets, progress, tools and logs;
 * legacy events then only enrich recon/obfuscation/match details that v2 M1
 * payloads do not carry.
 */
export function updateFromEvent(data: any) {
  const d = data?.data?.[0] || data?.data || data;

  pipelineState.update(s => {
    if (s.runId) return enrichFromLegacy(s, d);
    const next = { ...s };

    const phase = d.Phase || '';
    const target = d.Target || '';
    const message = d.Message || '';

    // Phase update
    if (phase) {
      // Map engine phases to our phases
      if (phase === 'scan' || phase === 'recon' || phase === 'match') next.phase = 'analysis';
      else if (phase === 'tools' || phase === 'install' || phase === 'download' || phase === 'extract') next.phase = 'tools';
      else if (phase === 'execute' || phase === 'log') next.phase = 'execute';
      else if (phase === 'done') {
        next.phase = 'done';
      }
      else if (phase === 'skip') { /* keep current phase */ }
    }

    // If Progress present but no Phase given, and still in early phase — advance to execute
    if (!phase && d.Progress && (s.phase === 'analysis' || s.phase === 'tools')) {
      next.phase = 'execute';
    }

    // Target (file being processed)
    if (target) {
      next.currentTarget = target;
    }
    // Output path (only set when explicitly provided)
    if (d.Output) {
      next.outputPath = d.Output;
    }

    // Status message
    if (message) {
      next.lastMessage = message;
    }

    enrichInto(next, d);
    if (typeof d.FilesTotal === 'number') next.filesTotal = d.FilesTotal;

    if (phase === 'download' && message) {
      const match = message.match(/Downloading (.+?)\.\.\. (\d+)%/);
      if (match) {
        next.downloadingTool = match[1];
        next.downloadProgress = parseInt(match[2]);
      }
      // Parse "X / Y MB" download size
      const bytesMatch = message.match(/([\d.]+)\s*\/\s*([\d.]+)\s*MB/);
      if (bytesMatch) {
        next.downloadBytes = Math.round(parseFloat(bytesMatch[1]) * 1024 * 1024);
        next.downloadTotalBytes = Math.round(parseFloat(bytesMatch[2]) * 1024 * 1024);
      }
    }
    if (phase === 'extract' && message) {
      const match = message.match(/Extracting (.+?)\.\.\./);
      if (match) {
        next.downloadingTool = match[1];
        next.downloadProgress = -1; // indeterminate
      }
    }

    // Capture tools needed list from either 'tools' or 'install' phase
    if ((phase === 'tools' || phase === 'install') && d.ToolsNeeded && Array.isArray(d.ToolsNeeded)) {
      const merged = new Set([...s.toolsNeeded, ...d.ToolsNeeded]);
      next.toolsNeeded = [...merged];
    }

    if (phase === 'install' && message) {
      // When a new "Installing Y..." arrives, mark the previous tool as installed
      const installMatch = message.match(/^Installing\s+(\S+)/);
      if (installMatch) {
        const newTool = installMatch[1].replace(/\.{3}$/, '');
        // Find the previously installing tool from old toolsInfo
        const prevMatch = s.toolsInfo.match(/^Installing\s+(\S+)/);
        if (prevMatch) {
          const prevTool = prevMatch[1].replace(/\.{3}$/, '');
          if (prevTool !== newTool && !s.toolsInstalled.includes(prevTool)) {
            next.toolsInstalled = [...s.toolsInstalled, prevTool];
          }
        }
      }
      // "All tools ready" or similar completion message = mark all remaining as installed
      if (message.toLowerCase().includes('ready') || message.toLowerCase().includes('done')) {
        next.toolsInstalled = [...next.toolsNeeded];
      }
      next.toolsInfo = message;
    }

    // When execute starts, mark any un-installed tools as ready (they were already present)
    if (phase === 'execute' && next.toolsNeeded.length > 0 && next.toolsInstalled.length < next.toolsNeeded.length) {
      next.toolsInstalled = [...next.toolsNeeded];
      next.downloadingTool = '';
    }

    // Only log actual tool output, not status messages from other phases
    if (phase === 'execute' || phase === 'log') {
      if (message) {
        next.logs = [...s.logs.slice(-19), message];
        // Counters are driven by StepProgress events only — no log-based counting.
      }
    }

    // Capture output stats
    if (phase === 'stats' && d.OutputStats) {
      next.outputStats = d.OutputStats;
    }

    // Track current tool from Running events
    if (d.Tool && (!d.Progress || d.Progress.Status === 'Running')) {
      next.currentTool = d.Tool;
    }

    // Step progress
    if (d.Progress) {
      const p = d.Progress;
      next.step = p.Step ?? s.step;
      next.stepTotal = p.Total ?? s.stepTotal;
      // Only update stepName from intermediate events (files, functions, phases)
      // Skip initial Running events that carry recipe step names ("Copy original", "Extract metadata")
      if (p.Name && (p.Count > 0 || p.Name.startsWith('ghidra:') || p.Name.includes('.'))) {
        next.stepName = p.Name;
      } else if (p.Status === 'Success' || p.Status === 'Failed') {
        next.stepName = ''; // clear on completion
      }
      // Only count a step as completed (+1) when its status is Success/Skipped/Failed.
      // Running means the step is still in progress — don't inflate the percentage.
      const stepDone = p.Status === 'Success' || p.Status === 'Skipped' || p.Status === 'Failed';
      next.progress = next.stepTotal > 0
        ? Math.round(((next.step + (stepDone ? 1 : 0)) / next.stepTotal) * 100)
        : 0;

      // Update execCounters from StepProgress events
      const stepTool = p.Tool || d.Tool || '';

      // Record the first step index each tool appears at, so the UI can list
      // tools in execution order instead of the recipe's RequiredTools order.
      if (stepTool && typeof p.Step === 'number') {
        const prevOrder = next.toolOrder[stepTool] ?? s.toolOrder[stepTool];
        if (prevOrder === undefined || p.Step < prevOrder) {
          next.toolOrder = { ...(next.toolOrder || s.toolOrder), [stepTool]: p.Step };
        }
      }

      // Running: show progress for active tool (accept Count=0 for phase indicators like "analyzing")
      if (stepTool && p.Status === 'Running' && p.Unit) {
        if (stepTool === next.currentTool || stepTool === s.currentTool || !s.execCounters[stepTool]) {
          const prev = s.execCounters[stepTool] || { count: 0, unit: p.Unit, countTotal: 0, _completed: 0 };
          next.execCounters = { ...s.execCounters, ...next.execCounters, [stepTool]: {
            count: (prev._completed || 0) + p.Count,
            unit: p.Unit,
            countTotal: p.CountTotal || 0,
            _completed: prev._completed || 0,
          }};
        }
      }

      // Completion: update counter (shown when tool is not active)
      if (stepTool && (p.Status === 'Success' || p.Status === 'Skipped' || p.Status === 'Failed')) {
        const prev = next.execCounters[stepTool] || s.execCounters[stepTool] || { count: 0, unit: '', countTotal: 0, _completed: 0 };
        // Sizes (KB) are absolute and replace; item counts accumulate. This keeps
        // a tool that runs across multiple steps (e.g. de4dot) from summing sizes.
        const isSize = (p.Unit || prev.unit) === 'KB';
        const completed = isSize
          ? (p.Count > 0 ? p.Count : (prev._completed || 0))
          : (prev._completed || 0) + (p.Count > 0 ? p.Count : 0);
        next.execCounters = { ...next.execCounters, [stepTool]: {
          count: completed,
          unit: p.Unit || prev.unit || '',
          countTotal: 0,
          _completed: completed,
        }};
        // Record the tool's final outcome so the UI can show skipped/failed
        // instead of a misleading success checkmark.
        const outcome = p.Status === 'Success' ? 'success' : p.Status === 'Skipped' ? 'skipped' : 'failed';
        next.toolStatus = { ...(next.toolStatus || s.toolStatus), [stepTool]: outcome };
      }
    }

    // Error text: the engine sends a wire-safe `error` string; older builds sent
    // `Error` as a Go error interface, which marshals to `{}`.
    const errText = eventErrorText(d);
    const runOutcome: string = d.RunOutcome || '';
    const targetOutcome: string = d.TargetOutcome || '';

    // One target failing must not flip the whole run to error while other
    // targets continue: record it on the target and in the log only.
    if (!d.Done && (errText || targetOutcome === 'error')) {
      const label = target ? `${target}: ${errText || 'failed'}` : errText || 'failed';
      next.logs = [...(next.logs || s.logs).slice(-19), `ERROR ${label}`];
      if (target) {
        const targets = [...(next.targets || s.targets)];
        const i = targets.findIndex(t => t.path === target);
        const entry: PipelineTarget = { ...(i >= 0 ? targets[i] : { path: target }), status: 'error', error: errText || undefined };
        if (i >= 0) targets[i] = entry; else targets.push(entry);
        next.targets = targets;
      }
    }

    // Done — mark all tools as _done
    if (d.Done) {
      // Terminal outcome of the whole run. Without RunOutcome (older engine),
      // fall back to "an error on the final event means the run failed".
      if (runOutcome === 'error' || (!runOutcome && errText)) {
        next.phase = 'error';
        next.error = errText || 'Pipeline failed';
      } else if (runOutcome === 'cancelled') {
        next.phase = 'cancelled';
      } else {
        next.phase = 'done';
      }
      next.progress = 100;
      next.currentTool = '';
      next.outputPath = d.OutputPath || d.Output || s.outputPath;
      // Mark all tools as done
      const finalCounters = { ...s.execCounters, ...next.execCounters };
      for (const tool of Object.keys(finalCounters)) {
        finalCounters[tool] = { ...finalCounters[tool], _done: true };
      }
      next.execCounters = finalCounters;
    }

    // Capture OutputPath whenever provided (any phase)
    if (d.OutputPath) {
      next.outputPath = d.OutputPath;
    }

    return next;
  });
}

/**
 * v2-active path: the legacy stream is a projection of the same v2 events, so
 * take only what events v2 M1 does not carry (post-run output statistics).
 */
function enrichFromLegacy(s: PipelineState, d: any): PipelineState {
  if (d?.Phase === 'stats' && Array.isArray(d.OutputStats)) return { ...s, outputStats: d.OutputStats };
  return s;
}

/** Legacy recon/match/obfuscation/skip details (message-based, pre-v2 engines). */
function enrichInto(next: PipelineState, d: any) {
  const phase = d.Phase || '';
  const target = d.Target || '';
  const message = d.Message || '';
  const fname = target ? (target.split(/[\\/]/).pop() || target) : '';

  if (phase === 'scan' && message) next.scanInfo = message;

  if (phase === 'recon' && fname) {
    if (!next.reconResults.find(r => r.file === fname)) {
      next.reconResults = [...next.reconResults, { file: fname, kind: '', reconKind: d.ReconKind || '', compiler: d.Compiler || '', obfuscator: d.Obfuscator || '', size: d.FileSize || 0 }];
    }
    if (d.ReconKind) next.reconKind = d.ReconKind;
    if (d.Compiler) next.compiler = d.Compiler;
    if (d.Obfuscator) next.obfuscator = d.Obfuscator;
    if (d.FileSize) next.fileSize = (next.fileSize || 0) + d.FileSize;
  }

  if (phase === 'match' && fname && message) {
    const recipe = message.replace('Recipe: ', '').replace('No matching recipe found', 'Unknown');
    next.reconResults = next.reconResults.map(r => r.file === fname ? { ...r, kind: recipe } : r);
    if (d.RecipeName) next.recipeName = d.RecipeName;
    if (d.RecipeDesc) next.recipeDesc = d.RecipeDesc;
  }

  // Accumulate obfuscation detections (grouped by obfuscator name)
  if (d.Obfuscator && fname) {
    const existing = next.obfuscations.find(o => o.name === d.Obfuscator);
    if (existing) {
      if (!existing.affectedFiles.includes(fname)) {
        next.obfuscations = next.obfuscations.map(o =>
          o.name === d.Obfuscator ? { ...o, affectedFiles: [...o.affectedFiles, fname] } : o
        );
      }
    } else {
      next.obfuscations = [...next.obfuscations, { name: d.Obfuscator, deobfuscator: d.Deobfuscator || null, affectedFiles: [fname] }];
    }
  }

  if (phase === 'skip' && fname && message) {
    next.reconResults = [...next.reconResults, { file: fname, kind: 'Skipped', reconKind: '', compiler: '', obfuscator: '', size: 0 }];
  }
}

/** Extract human-readable error text from a pipeline event (string or object). */
export function eventErrorText(d: any): string {
  for (const v of [d?.error, d?.ErrorMsg, d?.Error]) {
    if (!v) continue;
    if (typeof v === 'string') return v;
    if (typeof v === 'object') {
      const m = v.message || v.error || v.Error;
      if (typeof m === 'string' && m) return m;
      const j = JSON.stringify(v);
      if (j && j !== '{}' && j !== '[]') return j;
    }
  }
  return '';
}

// --- History ---

export interface HistoryEntry {
  path: string;
  kind: string;
  output: string;
  timestamp: number;
  success: boolean;
  error?: string;
}

const HISTORY_KEY = 'morgue-history';
const MAX_HISTORY = 20;

function loadHistory(): HistoryEntry[] {
  try {
    return JSON.parse(localStorage.getItem(HISTORY_KEY) || '[]');
  } catch {
    return [];
  }
}

export const history = writable<HistoryEntry[]>(loadHistory());

export function addHistoryEntry(entry: HistoryEntry) {
  history.update(h => {
    const next = [entry, ...h.filter(e => e.path !== entry.path)].slice(0, MAX_HISTORY);
    try { localStorage.setItem(HISTORY_KEY, JSON.stringify(next)); } catch {}
    return next;
  });
}
