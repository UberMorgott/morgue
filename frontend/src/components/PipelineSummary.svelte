<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import { primaryFidelity, targetTree, type PipelineState, type TargetQuality } from '../lib/pipeline';

  let { lang = 'en' as Lang, state, elapsed = '' }: {
    lang?: Lang;
    state: PipelineState;
    elapsed?: string;
  } = $props();

  const qualityRows = $derived(targetTree(state.targets).filter(r => r.target.quality || primaryFidelity(r.target)));

  const statusTag: Record<string, string> = { pass: 'tag-success', degraded: 'tag-accent' };
  const pct = (v: number | null) => (v === null ? '' : `${(v * 100).toFixed(1)}%`);

  function metrics(q: TargetQuality | undefined): string[] {
    if (!q) return [];
    const out: string[] = [];
    if (q.functionsRatio !== null) out.push(`${t(lang, 'home.summary.quality.functions')} ${pct(q.functionsRatio)}`);
    else if (q.typesRatio !== null) out.push(`${t(lang, 'home.summary.quality.types')} ${pct(q.typesRatio)}`);
    if (q.namesAutoRatio !== null) out.push(`${t(lang, 'home.summary.quality.autoNames')} ${pct(q.namesAutoRatio)}`);
    if (q.decompilerErrors !== null) out.push(`${t(lang, 'home.summary.quality.decompErrors')} ${q.decompilerErrors}`);
    return out.slice(0, 3);
  }

  function statusLabel(q: TargetQuality | undefined): string {
    if (!q) return t(lang, 'home.summary.quality.notCollected');
    const key = `home.summary.quality.${q.status}`;
    const s = t(lang, key);
    return s === key ? q.status : s;
  }
</script>

<div class="acc-section acc-section-summary row-separator">
  <div class="acc-section-header">
    <svg class="acc-icon acc-icon-done" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M5 8l2 2 4-4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
    <span class="acc-section-title">{t(lang, 'home.summary')}</span>
    {#if state.startedAt > 0}
      <span class="acc-elapsed font-mono">{elapsed}</span>
    {/if}
  </div>

  {#if state.filesTotal === 0 && state.reconResults.length === 0}
    <div class="alert-block alert-warning">{t(lang, 'home.summary.noTargets')}</div>
  {/if}

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

  {#if qualityRows.length > 0}
    <div class="quality-list card-sm">
      <span class="section-label">{t(lang, 'home.summary.quality')}</span>
      {#each qualityRows as { target, depth } (target.id ?? target.path)}
        {@const fid = primaryFidelity(target)}
        <div class="quality-row row-separator" style:padding-left="{depth * 14}px">
          <span class="quality-name font-mono text-sm selectable" title={target.path}>{target.relativeInput || target.path}</span>
          <span class="tag {statusTag[target.quality?.status ?? ''] ?? 'tag-muted'}">{statusLabel(target.quality)}</span>
          {#each metrics(target.quality) as m (m)}
            <span class="quality-metric text-xs font-accent">{m}</span>
          {/each}
          {#if fid}
            <span class="tag tag-muted">{t(lang, `home.summary.fidelity.${fid}`)}</span>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

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
  .quality-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .quality-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding-bottom: 6px;
  }
  .quality-name {
    flex: 1 1 200px;
    min-width: 0;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .quality-metric {
    color: var(--text-secondary);
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
