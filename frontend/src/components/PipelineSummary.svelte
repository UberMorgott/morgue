<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import type { PipelineState } from '../lib/pipeline';

  let { lang = 'en' as Lang, state, elapsed = '' }: {
    lang?: Lang;
    state: PipelineState;
    elapsed?: string;
  } = $props();
</script>

<div class="acc-section acc-section-summary">
  <div class="acc-section-header">
    <svg class="acc-icon acc-icon-done" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M5 8l2 2 4-4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
    <span class="acc-section-title">{t(lang, 'home.summary')}</span>
    {#if state.startedAt > 0}
      <span class="acc-elapsed">{elapsed}</span>
    {/if}
  </div>

  <div class="summary-stats">
    <div class="stat">
      <span class="stat-num">{state.filesProcessed || state.reconResults.length}</span>
      <span class="stat-label">{t(lang, 'home.summary.files')}</span>
    </div>
    <div class="stat">
      <span class="stat-num">{state.reconResults.filter(r => r.kind && r.kind !== 'Skipped' && r.kind !== 'Unknown').length}</span>
      <span class="stat-label">{t(lang, 'home.summary.decompiled')}</span>
    </div>
    <div class="stat">
      <span class="stat-num">{state.reconResults.filter(r => r.kind === 'Skipped').length}</span>
      <span class="stat-label">{t(lang, 'home.summary.skipped')}</span>
    </div>
  </div>

  {#if state.outputStats.length > 0}
    <div class="output-stats">
      {#each state.outputStats as line, i (i)}
        <div class="output-stat-line">{line}</div>
      {/each}
    </div>
  {/if}

  {#if state.outputPath}
    <div class="summary-output">
      <span class="result-label">Output:</span>
      <span class="result-path selectable">{state.outputPath}</span>
    </div>
  {/if}

  {#if state.logs.length > 0}
    <details class="summary-logs">
      <summary class="logs-toggle">{t(lang, 'home.summary.log')} ({state.logs.length})</summary>
      <div class="logs-body">
        <pre class="log-text">{state.logs.join('\n')}</pre>
      </div>
    </details>
  {/if}
</div>

<style>
  .acc-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px 20px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .acc-section:last-of-type {
    border-bottom: none;
  }
  .acc-section-header {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .acc-icon-done {
    color: var(--success);
    flex-shrink: 0;
  }
  .acc-section-title {
    font-size: 0.82rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-heading);
    flex: 1;
  }
  .acc-elapsed {
    font-size: 0.78rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-muted);
    margin-left: auto;
  }
  .acc-section-summary .summary-stats {
    margin-top: 4px;
  }
  .summary-stats {
    display: flex;
    gap: 24px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    flex: 1;
    padding: 12px;
    background: var(--bg-card);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-subtle);
  }
  .stat-num {
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--accent);
    font-family: 'Orbitron', monospace;
  }
  .stat-label {
    font-size: 0.72rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .output-stats {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 14px;
    background: var(--bg-card);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-subtle);
    font-family: 'Consolas', 'Courier New', monospace;
    font-size: 0.82rem;
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .output-stat-line:first-child {
    color: var(--text-primary);
    font-weight: 600;
    margin-bottom: 2px;
  }
  .summary-output {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 16px;
    background: var(--bg-card);
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-subtle);
  }
  .result-label {
    font-size: 0.88rem;
    font-weight: 600;
    color: var(--text-primary);
    flex-shrink: 0;
  }
  .result-path {
    font-size: 0.84rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .summary-logs {
    border-top: 1px solid var(--border-subtle);
    padding-top: 12px;
  }
  .logs-toggle {
    font-size: 0.82rem;
    color: var(--text-muted);
    cursor: pointer;
    padding: 4px 0;
  }
  .logs-toggle:hover {
    color: var(--text-secondary);
  }
  .logs-body {
    max-height: 200px;
    overflow-y: auto;
    margin-top: 8px;
    -webkit-backdrop-filter: none;
    backdrop-filter: none;
  }
  .log-text {
    font-size: 13px;
    font-family: 'Consolas', 'Courier New', monospace;
    color: #e0d0c0;
    line-height: 2;
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
