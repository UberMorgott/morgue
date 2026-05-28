<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import type { PipelinePhase } from '../lib/pipeline';

  let { lang, reconResults, phase, reconKind, compiler, obfuscator, fileSize, recipeName, recipeDesc }: {
    lang: Lang;
    reconResults: Array<{ file: string; kind: string; reconKind: string; compiler: string; obfuscator: string; size: number }>;
    phase: PipelinePhase;
    reconKind: string;
    compiler: string;
    obfuscator: string;
    fileSize: number;
    recipeName: string;
    recipeDesc: string;
  } = $props();

  function formatFileSize(bytes: number): string {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  }
</script>

<div class="acc-section">
  <div class="acc-section-header">
    <svg class="acc-icon" width="16" height="16" viewBox="0 0 16 16"><circle cx="7" cy="7" r="5.5" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M11 11l3.5 3.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
    <span class="acc-section-title">{t(lang, 'home.section.analysis')}</span>
    {#if ['scan', 'recon'].includes(phase)}
      <span class="spinner-sm"></span>
    {/if}
  </div>

  {#if reconKind}
    {#each reconResults as r, i (i)}
      <div class="acc-detail-row">
        <span class="acc-detail-mono">{r.file}</span>
        {#if reconKind}
          <span class="tag tag-kind">{reconKind}</span>
        {/if}
        {#if r.kind}
          <span class="tag {r.kind === 'Skipped' ? 'tag-muted' : r.kind === 'Unknown' ? 'tag-muted' : 'tag-accent'}">{r.kind}</span>
        {/if}
      </div>
    {/each}
    {@const metaItems = [
      compiler ? `${t(lang, 'pipeline.compiler')} ${compiler}` : '',
      obfuscator ? `${t(lang, 'pipeline.obfuscator')} ${obfuscator}` : '',
      fileSize ? `${t(lang, 'pipeline.size')} ${formatFileSize(fileSize)}` : '',
    ].filter(Boolean)}
    {#if metaItems.length > 0}
      <div class="acc-detail-row detect-meta">
        {#each metaItems as item, i (i)}
          {#if i > 0}
            <span class="detect-meta-sep">|</span>
          {/if}
          <span class="detect-meta-value">{item}</span>
        {/each}
      </div>
    {/if}
    {#if recipeName}
      <div class="acc-detail-row">
        <span class="acc-detail-label">{t(lang, 'pipeline.recipe')}</span>
        <span class="acc-detail-mono">{recipeName}</span>
        {#if recipeDesc}
          <span class="acc-detail-value muted">&mdash; {recipeDesc}</span>
        {/if}
      </div>
    {/if}
  {:else}
    {#each reconResults as r, i (i)}
      <div class="acc-detail-row">
        <span class="acc-detail-mono">{r.file}</span>
        {#if r.kind}
          <span class="tag {r.kind === 'Skipped' ? 'tag-muted' : r.kind === 'Unknown' ? 'tag-muted' : 'tag-accent'}">{r.kind}</span>
        {:else}
          <span class="spinner-sm"></span>
        {/if}
      </div>
    {/each}

    {#if reconResults.length === 0 && ['scan', 'recon'].includes(phase)}
      <div class="acc-detail-row">
        <span class="acc-detail-value muted">{t(lang, 'home.classifying')}</span>
      </div>
    {/if}
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
  .acc-section:last-of-type { border-bottom: none; }

  .acc-section-header { display: flex; align-items: center; gap: 8px; }
  .acc-icon { color: var(--accent); flex-shrink: 0; }
  .acc-section-title {
    font-size: 0.82rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-heading);
    flex: 1;
  }

  .acc-detail-row { display: flex; align-items: center; gap: 10px; padding: 2px 0 2px 24px; }
  .acc-detail-label {
    font-size: 0.75rem; color: var(--text-muted); text-transform: uppercase;
    letter-spacing: 0.3px; flex-shrink: 0; min-width: 48px;
  }
  .acc-detail-value { font-size: 0.84rem; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .acc-detail-value.muted { color: var(--text-muted); }
  .acc-detail-mono {
    font-size: 0.84rem; font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1;
  }

  .tag-kind { background: var(--accent-dim); color: var(--accent); border-color: var(--accent); }

  .detect-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .detect-meta-value { font-size: 0.82rem; color: var(--text-primary); font-weight: 500; }
  .detect-meta-sep { color: var(--border); font-size: 0.75rem; }

  .spinner-sm {
    width: 14px; height: 14px; border: 2px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.6s linear infinite; flex-shrink: 0;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
