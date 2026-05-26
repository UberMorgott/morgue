<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import FileTree from '../components/FileTree.svelte';
  import { ReconService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  const dispatch = createEventDispatcher();

  export let inputPath: string = '';

  let items: Array<{
    path: string;
    group: string;
    kind?: string;
    obfuscator?: string;
    recipe?: string;
    selected: boolean;
    skipped?: boolean;
    skipReason?: string;
  }> = [];
  let scanning = false;
  let classifying = false;
  let error = '';

  onMount(async () => {
    if (inputPath) {
      await scanDirectory(inputPath);
    }
  });

  async function scanDirectory(dir: string) {
    scanning = true;
    error = '';
    try {
      const targets = await ReconService.ScanDirectory(dir);
      items = (targets || []).map((t: any) => ({
        path: t.path || t.Path,
        group: t.group || t.Group || '',
        selected: true,
        skipped: false,
      }));

      // Classify each file for recon details
      classifying = true;
      for (let i = 0; i < items.length; i++) {
        try {
          const result = await ReconService.ClassifyFile(items[i].path);
          if (result) {
            items[i].kind = result.kind || result.Kind || '';
            items[i].obfuscator = result.obfuscator || result.Obfuscator || '';
          }
        } catch (e) {
          console.error('ClassifyFile failed:', items[i].path, e);
        }
      }
      items = items; // trigger reactivity
      classifying = false;
    } catch (e: any) {
      error = e.message || String(e);
      console.error('ScanDirectory failed:', e);
    } finally {
      scanning = false;
    }
  }

  function startPipeline() {
    const selected = items.filter(i => i.selected).map(i => i.path);
    dispatch('start-pipeline', { paths: selected, inputPath });
  }

  $: selectedCount = items.filter(i => i.selected).length;
</script>

<div class="scan-page">
  <div class="scan-header">
    <h2 class="scan-title">{t(lang, 'scan.title')}</h2>
    {#if inputPath}
      <span class="scan-path selectable">{inputPath}</span>
    {/if}
  </div>

  {#if scanning}
    <div class="scan-loading">
      <span class="loading-spinner"></span>
      <span>{t(lang, 'scan.scanning')}</span>
    </div>
  {:else if error}
    <div class="scan-error">{error}</div>
  {:else}
    <div class="scan-content">
      <FileTree {lang} bind:items />
      {#if classifying}
        <div class="classifying-hint">{t(lang, 'scan.classifying')}</div>
      {/if}
    </div>

    <div class="scan-footer">
      <span class="scan-summary">{selectedCount} {t(lang, 'scan.targetsSelected')}</span>
      <button class="start-btn" on:click={startPipeline} disabled={selectedCount === 0}>
        {t(lang, 'scan.startPipeline')}
      </button>
    </div>
  {/if}
</div>

<style>
  .scan-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 20px;
    gap: 16px;
  }
  .scan-header {
    display: flex;
    align-items: baseline;
    gap: 12px;
    flex-shrink: 0;
  }
  .scan-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }
  .scan-path {
    font-size: 11px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
  }
  .scan-content {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .scan-loading {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    padding: 24px;
  }
  .loading-spinner {
    width: 16px;
    height: 16px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .scan-error {
    color: var(--error);
    padding: 12px;
    background: rgba(255, 51, 102, 0.1);
    border: 1px solid rgba(255, 51, 102, 0.2);
    border-radius: 6px;
    font-size: 12px;
  }
  .classifying-hint {
    font-size: 11px;
    color: var(--text-muted);
    font-style: italic;
  }
  .scan-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-top: 12px;
    border-top: 1px solid var(--border-subtle);
    flex-shrink: 0;
  }
  .scan-summary {
    font-size: 12px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
  }
  .start-btn {
    all: unset;
    font-size: 13px;
    padding: 8px 20px;
    border-radius: 6px;
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    cursor: pointer;
    transition: all 0.15s;
  }
  .start-btn:hover:not(:disabled) {
    box-shadow: 0 0 16px var(--accent-dim);
  }
  .start-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
