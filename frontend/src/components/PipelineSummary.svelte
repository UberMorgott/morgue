<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import type { PipelineState } from '../lib/pipeline';

  let { lang = 'en' as Lang, state, elapsed = '' }: {
    lang?: Lang;
    state: PipelineState;
    elapsed?: string;
  } = $props();
</script>

<div class="acc-section acc-section-summary row-separator">
  <div class="acc-section-header">
    <svg class="acc-icon acc-icon-done" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M5 8l2 2 4-4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
    <span class="acc-section-title">{t(lang, 'home.summary')}</span>
    {#if state.startedAt > 0}
      <span class="acc-elapsed font-mono">{elapsed}</span>
    {/if}
  </div>

  <div class="summary-stats">
    <div class="stat card-sm">
      <span class="stat-num font-accent">{state.reconResults.length}</span>
      <span class="stat-label">{t(lang, 'home.summary.files')}</span>
    </div>
    <div class="stat card-sm">
      <span class="stat-num font-accent">{state.reconResults.filter(r => r.kind && r.kind !== 'Skipped' && r.kind !== 'Unknown').length}</span>
      <span class="stat-label">{t(lang, 'home.summary.decompiled')}</span>
    </div>
    <div class="stat card-sm">
      <span class="stat-num font-accent">{state.reconResults.filter(r => r.kind === 'Skipped').length}</span>
      <span class="stat-label">{t(lang, 'home.summary.skipped')}</span>
    </div>
  </div>

  {#if state.outputStats.length > 0}
    <div class="output-stats card-sm font-mono">
      {#each state.outputStats as line, i (i)}
        <div class="output-stat-line">{line}</div>
      {/each}
    </div>
  {/if}

  {#if state.outputPath}
    <div class="summary-output card-sm">
      <span class="result-label">Output:</span>
      <span class="result-path selectable font-mono">{state.outputPath}</span>
    </div>
  {/if}

</div>

<style>
  .acc-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px 20px;
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
  }
  .stat-num {
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--accent);
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
  }
  .result-label {
    font-size: 0.88rem;
    font-weight: 600;
    color: var(--text-primary);
    flex-shrink: 0;
  }
  .result-path {
    font-size: 0.84rem;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
