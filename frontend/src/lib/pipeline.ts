import { writable, derived, get } from 'svelte/store';


export type PipelinePhase = 'idle' | 'scan' | 'recon' | 'tools' | 'execute' | 'done' | 'error' | 'cancelled';

export interface PipelineTarget {
  path: string;
  kind?: string;
  recipe?: string;
  status: 'pending' | 'running' | 'done' | 'error' | 'skipped';
  error?: string;
}

export interface PipelineState {
  phase: PipelinePhase;
  paused: boolean;
  inputPath: string;
  outputPath: string;
  targets: PipelineTarget[];
  filesTotal: number;
  filesProcessed: number;
  step: number;
  stepTotal: number;
  stepName: string;
  progress: number; // 0-100
  error: string;
  currentTarget: string;   // current file being processed
  lastMessage: string;     // last status message from engine
  startedAt: number;       // timestamp when pipeline started
  scanInfo: string;          // "Found 3 files in 1 group"
  reconResults: Array<{ file: string; kind: string }>;  // classification results
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
}

const initial: PipelineState = {
  phase: 'idle',
  paused: false,
  inputPath: '',
  outputPath: '',
  targets: [],
  filesTotal: 0,
  filesProcessed: 0,
  step: 0,
  stepTotal: 0,
  stepName: '',
  progress: 0,
  error: '',
  currentTarget: '',
  lastMessage: '',
  startedAt: 0,
  scanInfo: '',
  reconResults: [],
  toolsInfo: '',
  logs: [],
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
};

export const pipelineState = writable<PipelineState>({ ...initial });

export const isRunning = derived(pipelineState, $s =>
  $s.phase !== 'idle' && $s.phase !== 'done' && $s.phase !== 'error' && $s.phase !== 'cancelled'
);

export function resetPipeline() {
  pipelineState.set({ ...initial });
}

export function startPipeline(inputPath: string) {
  pipelineState.update(s => ({
    ...s,
    phase: 'scan',
    paused: false,
    inputPath,
    outputPath: '',
    targets: [],
    filesTotal: 0,
    filesProcessed: 0,
    step: 0,
    stepTotal: 0,
    stepName: '',
    progress: 0,
    error: '',
    currentTarget: '',
    lastMessage: '',
    startedAt: Date.now(),
    scanInfo: '',
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
  }));
}

export function updateFromEvent(data: any) {
  const d = data?.data?.[0] || data?.data || data;

  pipelineState.update(s => {
    const next = { ...s };

    const phase = d.Phase || d.phase || '';
    const target = d.Target || d.target || '';
    const message = d.Message || d.message || '';

    // Phase update
    if (phase) {
      // Map engine phases to our phases
      if (phase === 'scan') next.phase = 'scan';
      else if (phase === 'recon' || phase === 'match') next.phase = 'recon';
      else if (phase === 'tools' || phase === 'install') next.phase = 'tools';
      else if (phase === 'execute' || phase === 'log') next.phase = 'execute';
      else if (phase === 'done') {
        next.phase = 'done';
        // Ensure filesProcessed matches filesTotal on completion
        if (next.filesTotal > 0) next.filesProcessed = next.filesTotal;
      }
      else if (phase === 'skip') { /* keep current phase */ }
    }

    // Target (file being processed)
    if (target) {
      next.currentTarget = target;
    }
    // Output path (only set when explicitly provided)
    if (d.Output || d.output) {
      next.outputPath = d.Output || d.output;
    }

    // Status message
    if (message) {
      next.lastMessage = message;
    }

    // Accumulate per-phase data
    if (phase === 'scan' && message) {
      next.scanInfo = message;
    }

    if (phase === 'recon' && target) {
      const fname = target.split(/[\\/]/).pop() || target;
      if (!next.reconResults.find(r => r.file === fname)) {
        next.reconResults = [...s.reconResults, { file: fname, kind: '' }];
      }
      // Capture enriched recon data
      if (d.ReconKind) next.reconKind = d.ReconKind;
      if (d.Compiler) next.compiler = d.Compiler;
      if (d.Obfuscator) next.obfuscator = d.Obfuscator;
      if (d.FileSize) next.fileSize = d.FileSize;
    }

    if (phase === 'match' && target && message) {
      const fname = target.split(/[\\/]/).pop() || target;
      const recipe = message.replace('Matched recipe: ', '').replace('No matching recipe found', 'Unknown');
      next.reconResults = (next.reconResults.length ? next.reconResults : [...s.reconResults]).map(r =>
        r.file === fname ? { ...r, kind: recipe } : r
      );
      // Capture recipe details
      if (d.RecipeName) next.recipeName = d.RecipeName;
      if (d.RecipeDesc) next.recipeDesc = d.RecipeDesc;
    }

    if (phase === 'skip' && target && message) {
      const fname = target.split(/[\\/]/).pop() || target;
      next.reconResults = [...s.reconResults, { file: fname, kind: 'Skipped' }];
    }

    if (phase === 'download' && message) {
      const match = message.match(/Downloading (.+?)\.\.\. (\d+)%/);
      if (match) {
        next.downloadingTool = match[1];
        next.downloadProgress = parseInt(match[2]);
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
      next.toolsNeeded = d.ToolsNeeded;
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
      }
    }

    // Capture output stats
    if (phase === 'stats' && d.OutputStats) {
      next.outputStats = d.OutputStats;
    }

    // File counts
    if (d.FilesTotal || d.filesTotal) next.filesTotal = d.FilesTotal || d.filesTotal;
    if (d.FilesProcessed !== undefined || d.filesProcessed !== undefined) {
      next.filesProcessed = d.FilesProcessed ?? d.filesProcessed;
    }

    // Step progress
    if (d.Progress || d.progress) {
      const p = d.Progress || d.progress;
      next.step = p.Step ?? p.step ?? s.step;
      next.stepTotal = p.Total ?? p.total ?? s.stepTotal;
      next.stepName = p.Name ?? p.name ?? s.stepName;
      next.progress = next.stepTotal > 0
        ? Math.round(((next.step + 1) / next.stepTotal) * 100)
        : 0;
    }

    // Done
    if (d.Done || d.done) {
      next.phase = 'done';
      next.progress = 100;
      next.outputPath = d.OutputPath || d.Output || d.output || s.outputPath;
    }

    // Capture OutputPath whenever provided (any phase)
    if (d.OutputPath) {
      next.outputPath = d.OutputPath;
    }

    // Error
    if (d.Error || d.error) {
      const err = d.Error || d.error;
      next.error = typeof err === 'string' ? err : err.message || JSON.stringify(err);
      next.phase = 'error';
    }

    return next;
  });
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
