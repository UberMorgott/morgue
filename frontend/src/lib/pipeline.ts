import { writable, derived, get } from 'svelte/store';


export type PipelinePhase = 'idle' | 'analysis' | 'tools' | 'execute' | 'done' | 'error' | 'cancelled';

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
  step: number;
  stepTotal: number;
  stepName: string;
  progress: number; // 0-100
  error: string;
  currentTarget: string;   // current file being processed
  lastMessage: string;     // last status message from engine
  startedAt: number;       // timestamp when pipeline started
  scanInfo: string;          // "Found 3 files in 1 group"
  reconResults: Array<{ file: string; kind: string; reconKind: string; compiler: string; obfuscator: string; size: number }>;  // classification results
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
  execCounters: Record<string, { count: number; unit: string }>;  // per-tool Processing/Decompiling message counts
}

const initial: PipelineState = {
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
    phase: 'analysis',
    paused: false,
    inputPath,
    outputPath: '',
    targets: [],
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
    obfuscations: [],
    downloadBytes: 0,
    downloadTotalBytes: 0,
    execCounters: {},
  }));
}

export function updateFromEvent(data: any) {
  const d = data?.data?.[0] || data?.data || data;

  pipelineState.update(s => {
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

    // Accumulate per-phase data
    if (phase === 'scan' && message) {
      next.scanInfo = message;
    }

    if (phase === 'recon' && target) {
      const fname = target.split(/[\\/]/).pop() || target;
      if (!next.reconResults.find(r => r.file === fname)) {
        next.reconResults = [...s.reconResults, { file: fname, kind: '', reconKind: d.ReconKind || '', compiler: d.Compiler || '', obfuscator: d.Obfuscator || '', size: d.FileSize || 0 }];
      }
      // Capture enriched recon data
      if (d.ReconKind) next.reconKind = d.ReconKind;
      if (d.Compiler) next.compiler = d.Compiler;
      if (d.Obfuscator) next.obfuscator = d.Obfuscator;
      if (d.FileSize) next.fileSize = d.FileSize;
    }

    if (phase === 'match' && target && message) {
      const fname = target.split(/[\\/]/).pop() || target;
      const recipe = message.replace('Recipe: ', '').replace('No matching recipe found', 'Unknown');
      next.reconResults = (next.reconResults.length ? next.reconResults : [...s.reconResults]).map(r =>
        r.file === fname ? { ...r, kind: recipe } : r
      );
      // Capture recipe details
      if (d.RecipeName) next.recipeName = d.RecipeName;
      if (d.RecipeDesc) next.recipeDesc = d.RecipeDesc;
    }

    // Accumulate obfuscation detections (grouped by obfuscator name)
    if (d.Obfuscator) {
      const fname = target ? (target.split(/[\\/]/).pop() || target) : '';
      if (fname) {
        const existing = next.obfuscations.find(o => o.name === d.Obfuscator);
        if (existing) {
          if (!existing.affectedFiles.includes(fname)) {
            next.obfuscations = next.obfuscations.map(o =>
              o.name === d.Obfuscator ? { ...o, affectedFiles: [...o.affectedFiles, fname] } : o
            );
          }
        } else {
          next.obfuscations = [...next.obfuscations, {
            name: d.Obfuscator,
            deobfuscator: d.Deobfuscator || null,
            affectedFiles: [fname],
          }];
        }
      }
    }

    if (phase === 'skip' && target && message) {
      const fname = target.split(/[\\/]/).pop() || target;
      next.reconResults = [...s.reconResults, { file: fname, kind: 'Skipped', reconKind: '', compiler: '', obfuscator: '', size: 0 }];
    }

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
        // Count Processing/Decompiling messages per tool
        if (d.Tool && (message.startsWith('Processing') || message.startsWith('Decompiling'))) {
          const tool = d.Tool;
          const prev = s.execCounters[tool] || { count: 0, unit: 'items' };
          let unit = prev.unit;
          if (/type/i.test(message)) unit = 'types';
          else if (/method|function/i.test(message)) unit = 'functions';
          else if (/class/i.test(message)) unit = 'classes';
          next.execCounters = { ...s.execCounters, [tool]: { count: prev.count + 1, unit } };
        }
      }
    }

    // Capture output stats
    if (phase === 'stats' && d.OutputStats) {
      next.outputStats = d.OutputStats;
    }

    // Step progress
    if (d.Progress) {
      const p = d.Progress;
      next.step = p.Step ?? s.step;
      next.stepTotal = p.Total ?? s.stepTotal;
      next.stepName = p.Name ?? s.stepName;
      next.progress = next.stepTotal > 0
        ? Math.round(((next.step + 1) / next.stepTotal) * 100)
        : 0;
    }

    // Done
    if (d.Done) {
      next.phase = 'done';
      next.progress = 100;
      next.outputPath = d.OutputPath || d.Output || s.outputPath;
    }

    // Capture OutputPath whenever provided (any phase)
    if (d.OutputPath) {
      next.outputPath = d.OutputPath;
    }

    // Error
    if (d.Error) {
      const err = d.Error;
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
