<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import ProgressRing from './ProgressRing.svelte';

  let {
    lang,
    currentTarget = '',
    currentTool = '',
    step = 0,
    stepTotal = 0,
    stepName = '',
    progress = 0,
    logs = [] as string[],
    toolsNeeded = [] as string[],
    execCounters = {} as Record<string, { count: number; unit: string }>,
  }: {
    lang: Lang;
    currentTarget?: string;
    currentTool?: string;
    step?: number;
    stepTotal?: number;
    stepName?: string;
    progress?: number;
    logs?: string[];
    toolsNeeded?: string[];
    execCounters?: Record<string, { count: number; unit: string }>;
  } = $props();

  let logEl: HTMLDivElement | null = $state(null);

  // Auto-scroll log
  $effect(() => {
    if (logs.length && logEl) {
      setTimeout(() => { if (logEl) logEl.scrollTop = logEl.scrollHeight; }, 0);
    }
  });

  function isActive(tool: string): boolean {
    return currentTool === tool;
  }

  function isDone(tool: string): boolean {
    return !isActive(tool) && tool in execCounters;
  }

  function isWaiting(tool: string): boolean {
    return !isActive(tool) && !(tool in execCounters);
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

<div class="glass neon-border pipeline-panel animate-in" style="animation-delay: 0.8s;">
  <h3 class="panel-title">&#x25B6; {t(lang, 'execution.title')}</h3>

  <!-- Per-tool rows -->
  <div class="tool-grid">
    {#each toolsNeeded as tool (tool)}
      {@const active = isActive(tool)}
      {@const done = isDone(tool)}
      {@const waiting = isWaiting(tool)}
      {@const counter = execCounters[tool]}
      <div class="tool-row row-separator" class:waiting>
        <span class="tool-name font-accent">{tool}</span>
        <span class="tool-file-info">
          {#if active}
            <span class="file-current font-mono">{currentTarget ? basename(currentTarget) : ''}</span>
            <span class="file-step">{t(lang, 'execution.step')} {step + 1} / {stepTotal}</span>
          {:else if done}
            <span class="file-done">{t(lang, 'execution.done')}</span>
          {:else}
            <span class="file-waiting">{t(lang, 'execution.waiting')}</span>
          {/if}
        </span>
        <span class="tool-ring">
          {#if active}
            <ProgressRing value={progress} size={32} variant="accent" label="{Math.round(progress)}%" />
          {:else if done}
            <ProgressRing value={100} size={32} variant="success" label="&#x2713;" />
          {:else}
            <span class="ring-dash">&mdash;</span>
          {/if}
        </span>
        <span class="tool-counter">
          {#if counter}
            <span class="counter-value font-accent">{counter.count}</span>
            <span class="counter-unit">{counter.unit}</span>
          {/if}
        </span>
      </div>
    {/each}
  </div>

  <!-- Mini log -->
  {#if logs.length > 0}
    <div class="log-area font-mono mini-log" bind:this={logEl}>
      {#each logs.slice(-5) as line}
        {@const parsed = parseLogTool(line)}
        <div class="log-line">
          {#if parsed}
            <span class="log-tool">[{parsed.tool}]</span> {parsed.text}
          {:else}
            {line}
          {/if}
        </div>
      {/each}
    </div>
  {/if}
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
    grid-template-columns: 100px 1fr auto auto;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    transition: opacity 0.3s;
  }
  .tool-row.waiting {
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

  .tool-file-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow: hidden;
    min-width: 0;
  }

  .file-current {
    font-size: 0.82rem;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-step {
    font-size: 0.72rem;
    color: var(--text-muted);
    letter-spacing: 0.3px;
  }

  .file-done {
    font-size: 0.82rem;
    color: var(--success);
    font-weight: 500;
  }

  .file-waiting {
    font-size: 0.82rem;
    color: var(--text-muted);
    font-style: italic;
  }

  .tool-ring {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    flex-shrink: 0;
  }

  .ring-dash {
    font-size: 1.1rem;
    color: var(--text-muted);
    opacity: 0.5;
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
