<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import type { PipelinePhase } from '../lib/pipeline';
  import ProgressRing from './ProgressRing.svelte';

  let {
    lang,
    phase = 'idle' as PipelinePhase,
    currentTarget = '',
    currentTool = '',
    step = 0,
    stepTotal = 0,
    stepName = '',
    progress = 0,
    logs = [] as string[],
    toolsNeeded = [] as string[],
    toolsInstalled = [] as string[],
    downloadingTool = '',
    downloadProgress = 0,
    downloadBytes = 0,
    downloadTotalBytes = 0,
    lastMessage = '',
    execCounters = {} as Record<string, { count: number; unit: string; countTotal: number }>,
  }: {
    lang: Lang;
    phase?: PipelinePhase;
    currentTarget?: string;
    currentTool?: string;
    step?: number;
    stepTotal?: number;
    stepName?: string;
    progress?: number;
    logs?: string[];
    toolsNeeded?: string[];
    toolsInstalled?: string[];
    downloadingTool?: string;
    downloadProgress?: number;
    downloadBytes?: number;
    downloadTotalBytes?: number;
    lastMessage?: string;
    execCounters?: Record<string, { count: number; unit: string; countTotal: number }>;
  } = $props();

  type ToolState = 'checking' | 'downloading' | 'installing' | 'ready' | 'running' | 'done';

  let logEl: HTMLDivElement | null = $state(null);

  // Auto-scroll log
  $effect(() => {
    if (logs.length && logEl) {
      setTimeout(() => { if (logEl) logEl.scrollTop = logEl.scrollHeight; }, 0);
    }
  });

  // Panel title: contextual based on phase
  let panelTitle = $derived(
    phase === 'tools' ? t(lang, 'tools.title')
    : phase === 'execute' ? t(lang, 'execution.title')
    : phase === 'done' ? t(lang, 'execution.done')
    : t(lang, 'tools.title')
  );

  let panelIcon = $derived(
    phase === 'tools' ? '⚙' : '▶'
  );

  function getToolState(tool: string): ToolState {
    // Active tool = running
    if (currentTool === tool) return 'running';
    // Tool with counter data = done (counter only goes up via accumulation)
    if (tool in execCounters) return 'done';

    // Tools install phase states
    if (tool === downloadingTool) {
      const msg = lastMessage.toLowerCase();
      if (msg.includes('extract') || msg.includes('распаков')) return 'installing';
      return 'downloading';
    }
    if (toolsInstalled.includes(tool)) return 'ready';

    // If we're past the tools phase, all tools are implicitly ready
    if (phase === 'execute' || phase === 'done') return 'ready';

    return 'checking';
  }

  function formatMB(bytes: number): string {
    return (bytes / 1048576).toFixed(0);
  }

  function basename(p: string): string {
    return p.split(/[\\/]/).pop() || p;
  }

  /** Parse "[ToolName] rest..." into { tool, text } or null */
  function parseLogTool(line: string): { tool: string; text: string } | null {
    const m = line.match(/^\[([^\]]+)\]\s*(.*)/);
    return m ? { tool: m[1], text: m[2] } : null;
  }
</script>

<div class="glass neon-border pipeline-panel animate-in" style="animation-delay: 0.15s;">
  <h3 class="panel-title">{panelIcon} {panelTitle}</h3>

  <div class="tool-grid">
    {#each toolsNeeded as tool (tool)}
      {@const state = getToolState(tool)}
      {@const counter = execCounters[tool]}
      {@const ringValue =
        state === 'done' ? 100
        : state === 'ready' ? 100
        : state === 'running' && counter && counter.countTotal > 0
          ? Math.round((counter.count / counter.countTotal) * 100)
          : state === 'downloading' ? downloadProgress
        : -1}
      {@const ringVariant =
        state === 'done' ? 'success' : state === 'ready' ? 'accent' : 'accent'}
      {@const ringLabel =
        state === 'done' ? '✓'
        : state === 'ready' ? '✓'
        : ringValue >= 0 ? `${ringValue}%`
        : ''}
      {@const dimmed = state === 'checking'}
      <div class="tool-row row-separator" class:dimmed>
        <span class="tool-name font-accent">{tool}</span>
        <span class="tool-info">
          {#if state === 'checking'}
            <span class="info-muted">{t(lang, 'tools.pending')}</span>
          {:else if state === 'downloading'}
            <span class="info-active">{formatMB(downloadBytes)} / {formatMB(downloadTotalBytes)} MB</span>
          {:else if state === 'installing'}
            <span class="info-active">{t(lang, 'pipeline.extracting')}</span>
          {:else if state === 'ready'}
            <span class="info-ready">{t(lang, 'tools.ready')}</span>
          {:else if state === 'running'}
            <span class="info-current font-mono">{currentTarget ? basename(currentTarget) : ''}</span>
            {#if step >= 0 && stepTotal > 0 && currentTool === tool}
              <span class="info-step">{t(lang, stepName) || stepName}</span>
            {/if}
          {:else if state === 'done'}
            <span class="info-done">{t(lang, 'execution.done')}</span>
          {/if}
        </span>
        <span class="tool-counter">
          {#if counter && counter.count > 0}
            <span class="counter-value font-accent">{counter.count}</span>
            <span class="counter-unit">{counter.unit}</span>
          {:else if counter && counter.unit && counter.count === 0}
            <span class="counter-unit">{counter.unit}...</span>
          {/if}
        </span>
        <span class="tool-ring">
          <ProgressRing value={ringValue} size={32} variant={ringVariant} label={ringLabel} />
        </span>
      </div>
    {/each}
  </div>

</div>

<style>
  /* Tool grid */
  .tool-grid {
    display: flex;
    flex-direction: column;
    gap: 0;
  }

  .tool-row {
    display: grid;
    grid-template-columns: 100px 1fr auto 36px;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    transition: opacity 0.3s;
  }
  .tool-row.dimmed {
    opacity: 0.4;
  }

  .tool-name {
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--accent-warm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tool-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow: hidden;
    min-width: 0;
  }

  .info-muted {
    font-size: 0.82rem;
    color: var(--text-muted);
    font-style: italic;
  }

  .info-step {
    font-size: 0.72rem;
    color: var(--text-muted);
    letter-spacing: 0.3px;
  }

  .info-active {
    font-size: 0.82rem;
    color: var(--accent);
    font-weight: 500;
  }

  .info-ready {
    font-size: 0.82rem;
    color: var(--text-muted);
    font-weight: 500;
    font-style: italic;
  }

  .info-current {
    font-size: 0.82rem;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .info-done {
    font-size: 0.82rem;
    color: var(--success);
    font-weight: 500;
  }

  .tool-ring {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    flex-shrink: 0;
  }

  .tool-counter {
    display: flex;
    flex-direction: column;
    align-items: center;
    min-width: 52px;
    flex-shrink: 0;
  }

  .counter-value {
    font-size: 1rem;
    font-weight: 700;
    color: var(--accent-bright);
    line-height: 1;
  }

  .counter-unit {
    font-size: 0.65rem;
    color: var(--text-muted);
    text-transform: lowercase;
    letter-spacing: 0.3px;
    margin-top: 2px;
  }

  /* Mini log — max-height only; base styles from global .log-area */
  .mini-log {
    max-height: 100px;
  }

  .log-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .log-tool {
    color: var(--accent-warm);
    font-weight: 600;
  }
</style>
