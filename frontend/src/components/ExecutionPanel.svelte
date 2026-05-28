<script lang="ts">
  import ProgressRing from './ProgressRing.svelte';

  let {
    currentTarget = '',
    step = 0,
    stepTotal = 0,
    stepName = '',
    progress = 0,
    logs = [] as string[],
    toolsNeeded = [] as string[],
    execCounters = {} as Record<string, { count: number; unit: string }>,
  }: {
    currentTarget?: string;
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
    return stepName.toLowerCase().includes(tool.toLowerCase());
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

<div class="exec-panel glass neon-border animate-in" style="animation-delay: 0.8s;">
  <h3 class="panel-title">&#x25B6; &#x0412;&#x042B;&#x041F;&#x041E;&#x041B;&#x041D;&#x0415;&#x041D;&#x0418;&#x0415;</h3>

  <!-- Per-tool rows -->
  <div class="tool-grid">
    {#each toolsNeeded as tool (tool)}
      {@const active = isActive(tool)}
      {@const done = isDone(tool)}
      {@const waiting = isWaiting(tool)}
      {@const counter = execCounters[tool]}
      <div class="tool-row" class:waiting>
        <span class="tool-name">{tool}</span>
        <span class="tool-file-info">
          {#if active}
            <span class="file-current">{currentTarget ? basename(currentTarget) : ''}</span>
            <span class="file-step">Step {step + 1} / {stepTotal}</span>
          {:else if done}
            <span class="file-done">&#x0413;&#x043E;&#x0442;&#x043E;&#x0432;&#x043E;</span>
          {:else}
            <span class="file-waiting">&#x041E;&#x0436;&#x0438;&#x0434;&#x0430;&#x043D;&#x0438;&#x0435;...</span>
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
            <span class="counter-value">{counter.count}</span>
            <span class="counter-unit">{counter.unit}</span>
          {/if}
        </span>
      </div>
    {/each}
  </div>

  <!-- Mini log -->
  {#if logs.length > 0}
    <div class="mini-log" bind:this={logEl}>
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
  .exec-panel {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 18px 20px;
    border-radius: var(--radius);
    animation-fill-mode: backwards;
  }

  .panel-title {
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: 0.88rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1.5px;
    color: var(--text-heading);
    margin: 0;
  }

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
    border-bottom: 1px solid var(--border-subtle);
    transition: opacity 0.3s;
  }
  .tool-row:last-child {
    border-bottom: none;
  }
  .tool-row.waiting {
    opacity: 0.4;
  }

  .tool-name {
    font-family: 'Orbitron', monospace;
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
    font-family: 'Consolas', 'Courier New', monospace;
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
    font-family: 'Orbitron', monospace;
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

  /* Mini log */
  .mini-log {
    background: rgba(0, 0, 0, 0.2);
    border-radius: var(--radius-sm);
    padding: 8px 12px;
    max-height: 100px;
    overflow-y: auto;
    font-family: 'Consolas', 'Courier New', monospace;
    font-size: 0.75rem;
    line-height: 1.6;
    color: var(--text-muted);
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
