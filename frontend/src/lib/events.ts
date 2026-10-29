// Wails v3 event helpers + the events v2 wire contract
// (internal/runmodel/events.go, docs/schemas/events-v2.schema.json).
import { Events } from '@wailsio/runtime';

type EventCallback = (data: any) => void;

// Events.On returns an unsubscribe function that removes only this listener,
// so multiple components can safely listen to the same event name.
export function onEvent(name: string, callback: EventCallback): () => void {
  return Events.On(name, callback);
}

/** Wails event name carrying one events-v2 envelope per emit. */
export const EVENT_V2 = 'pipeline:event:v2';
export const EVENT_VERSION = 2;

export type EventType =
  | 'run.started'
  | 'scan.completed'
  | 'target.discovered'
  | 'target.finished'
  | 'step.progress'
  | 'tool.progress'
  | 'artifact.written'
  | 'quality.updated'
  | 'log'
  | 'run.finished';

export type RunOutcome = 'success' | 'error' | 'cancelled';
export type TargetOutcome = RunOutcome | 'skipped';
export type StepStatus = 'pending' | 'running' | 'success' | 'failed' | 'skipped' | 'warn';
export type ToolStage = 'check' | 'download' | 'extract' | 'ready' | 'failed';

export interface RunHandle { runId: string; startedAt: string; output: string }

export interface RunSpec {
  version: number;
  path: string;
  output: string;
  recipe: string;
  noSkip: boolean;
  exclude: string[] | null;
  referencePaths: string[] | null;
  allowDynamic: boolean;
  cflow: boolean;
  codeOnly: boolean;
  extractAssets: boolean;
  gameDataOut: string;
  toolsMirror: string;
}

export interface Counts {
  total: number; pending: number; running: number;
  success: number; error: number; cancelled: number; skipped: number;
}

export interface RunStarted { handle: RunHandle; spec: RunSpec }
export interface ScanCompleted { filesTotal: number; groupsTotal: number }
export interface TargetDiscovered {
  inputPath: string; relativeInput: string; kind: string; recipe: string; output: string; sizeBytes: number;
  // optional typed UI data (runmodel.TargetDiscovered omitempty fields)
  compiler?: string; obfuscator?: string; deobfuscator?: string; recipeDesc?: string; toolsNeeded?: string[];
}
export interface TargetFinished {
  outcome: TargetOutcome; recipe: string; output: string; error: string; skipReason: string; durationMs: number;
}
export interface StepProgress {
  stepId: string; name: string; tool: string; unit: string;
  stepIndex: number; stepTotal: number; count: number; countTotal: number; durationMs: number;
  status: StepStatus; error: string;
}
export interface ToolProgress {
  tool: string; stage: ToolStage; bytes: number; bytesTotal: number; version: string; error: string;
}
/** phase: optional legacy-display context, never a progress counter. */
export interface LogRecord { level: 'info' | 'warn' | 'error'; tool: string; message: string; phase?: string }
/** runmodel.Metric: value is null unless status is measured|estimated. */
export interface QualityMetric { value: number | null; status: string; basis: string }
export interface QualityCoverage { found: QualityMetric; emitted: QualityMetric; ratio: number | null; comparable: boolean }
/** Subset of runmodel.QualityReport the UI reads. */
export interface QualityReport {
  schemaVersion: number; family: string; status: 'pass' | 'degraded' | 'unknown' | 'not-applicable' | string;
  functions: QualityCoverage; types: QualityCoverage;
  names: { total: QualityMetric; autoNamed: QualityMetric; autoRatio: number | null; inferred: QualityMetric };
  decompilerErrors: QualityMetric;
}
export interface QualityUpdated { quality: QualityReport }
export interface ArtifactWritten { artifact: { id: string; targetId: string | null; path: string; kind: string } }
export interface RunFinished {
  outcome: RunOutcome; finishedAt: string; durationMs: number; error: string; targets: Counts; leafTargets: Counts;
}

export interface EventV2<T = unknown> {
  v: number;
  ts: string;
  seq: number;
  run: string;
  type: EventType | string;
  targetId?: string;
  parentTargetId?: string;
  data: T;
}

/**
 * Unwrap a Wails v3 event (data.data[0] → data.data → data) and return the
 * envelope when it is a structurally valid events-v2 record, else null.
 * Unknown event types are kept (callers skip them); other major versions are not.
 */
export function parseEventV2(raw: any): EventV2 | null {
  // The envelope itself has a `data` field, so only unwrap when `v` is absent.
  let e = raw;
  if (e && typeof e === 'object' && !('v' in e) && 'data' in e) {
    e = Array.isArray(e.data) ? e.data[0] : e.data;
  }
  if (typeof e === 'string') {
    try { return parseEventV2(JSON.parse(e)); } catch { return null; }
  }
  if (!e || typeof e !== 'object') return null;
  if (e.v !== EVENT_VERSION) return null;
  if (typeof e.seq !== 'number' || e.seq < 1 || !Number.isInteger(e.seq)) return null;
  if (typeof e.run !== 'string' || !/^[0-9a-f]{32}$/.test(e.run)) return null;
  if (typeof e.type !== 'string' || typeof e.ts !== 'string') return null;
  if (!e.data || typeof e.data !== 'object') return null;
  return e as EventV2;
}
