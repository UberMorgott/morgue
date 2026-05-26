<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { onMount, onDestroy } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import { t, type Lang } from '../lib/i18n';
  import { ReconService, PipelineService, ToolsService } from '../lib/api';
  import { onEvent } from '../lib/events';
  import { addOperation, updateOperation } from '../lib/operations';

  export let lang: Lang = 'en';
  export let inputPath: string = '';
  export let startupBusy: boolean = false;

  const dispatch = createEventDispatcher();

  type PipelinePhase = 'idle' | 'analyzing' | 'tools' | 'decompiling' | 'done' | 'error';

  let phase: PipelinePhase = 'idle';
  let busy = false;
  let outputPath = '';
  let errorMessage = '';
  let reconInfo = '';
  let pipelineProgress = 0;
  let pipelineStepLabel = '';
  let cleanups: Array<() => void> = [];
  let pipelineOpAdded = false;
  let lastProcessedPath = '';

  $: if (inputPath && inputPath !== lastProcessedPath && !busy && !startupBusy) {
    lastProcessedPath = inputPath;
    startPipeline();
  }

  function handleSelect(e: CustomEvent<{ path: string }>) {
    dispatch('select', { path: e.detail.path });
  }

  function handleBrowse() {
    dispatch('browse');
  }

  function handleClear() {
    phase = 'idle';
    busy = false;
    outputPath = '';
    errorMessage = '';
    reconInfo = '';
    pipelineProgress = 0;
    pipelineStepLabel = '';
    pipelineOpAdded = false;
    lastProcessedPath = '';
    dispatch('clear');
  }

  onMount(async () => {
    // If component re-mounts while pipeline is already running on backend,
    // sync the phase so we don't re-trigger startPipeline
    if (inputPath) {
      try {
        const status = await PipelineService.GetStatus();
        if (status && (status.Running || status.running)) {
          phase = 'decompiling';
          busy = true;
          lastProcessedPath = inputPath;
          pipelineOpAdded = false;
        }
      } catch {}
    }

    cleanups.push(
      onEvent('pipeline:progress', (data: any) => {
        const d = data?.data?.[0] || data?.data || data;
        if (d.Progress || d.progress) {
          const p = d.Progress || d.progress;
          const total = p.Total || p.total || 1;
          const step = p.Step || p.step || 0;
          pipelineProgress = ((step + 1) / total) * 100;
          pipelineStepLabel = `${t(lang, 'pipeline.step')} ${step + 1}/${total}: ${p.Name || p.name || ''}`;
          if (!pipelineOpAdded) {
            addOperation({ id: 'pipeline', type: 'pipeline', label: pipelineStepLabel, status: 'running', progress: 0 });
            pipelineOpAdded = true;
          }
          updateOperation('pipeline', { progress: pipelineProgress, label: pipelineStepLabel });
        }
        if (d.Done || d.done) {
          phase = 'done';
          busy = false;
          pipelineProgress = 100;
          outputPath = d.Output || d.output || inputPath;
          updateOperation('pipeline', { status: 'success', progress: 100, label: t(lang, 'pipeline.done') });
        }
        if (d.Error || d.error) {
          const err = d.Error || d.error;
          errorMessage = typeof err === 'string' ? err : err.message || JSON.stringify(err);
          phase = 'error';
          busy = false;
          updateOperation('pipeline', { status: 'failed', error: errorMessage });
        }
      }),
      onEvent('pipeline:log', (_data: any) => {
        // Logs handled by OperationsFooter
      }),
    );
  });

  onDestroy(() => {
    cleanups.forEach(fn => fn());
  });

  async function startPipeline() {
    if (busy) return;
    busy = true;
    phase = 'analyzing';
    errorMessage = '';
    reconInfo = '';
    outputPath = '';
    pipelineProgress = 0;
    pipelineOpAdded = false;

    addOperation({ id: 'pipeline', type: 'pipeline', label: t(lang, 'home.analyzing'), status: 'running', progress: 0 });
    pipelineOpAdded = true;

    try {
      // Step 1: Classify the input file
      try {
        const result = await ReconService.ClassifyFile(inputPath);
        if (result) {
          const kind = result.Kind || result.kind || '';
          const obf = result.Obfuscator || result.obfuscator || '';
          reconInfo = kind + (obf ? ` / ${obf}` : '');
        }
      } catch {
        // Classification is best-effort — pipeline will handle unknown files
      }

      // Step 2: Check tools and install missing
      phase = 'tools';
      updateOperation('pipeline', { label: t(lang, 'home.preparingTools') });
      try {
        const statuses = await ToolsService.CheckAll();
        const missing = (statuses || []).filter((s: any) => !(s.Installed || s.installed));
        if (missing.length > 0) {
          await ToolsService.InstallAll();
        }
      } catch {
        // If tools check fails, still try to run — pipeline will report missing tools
      }

      // Step 3: Execute pipeline
      phase = 'decompiling';
      updateOperation('pipeline', { label: t(lang, 'home.decompiling'), progress: 0 });
      await PipelineService.Run(inputPath, '');
    } catch (e: any) {
      if (phase !== 'done') {
        errorMessage = e.message || String(e);
        phase = 'error';
        updateOperation('pipeline', { status: 'failed', error: errorMessage });
      }
    } finally {
      busy = false;
    }
  }
</script>

<div class="home-page">
  {#if !inputPath}
    <div class="home-hero">
      <h1 class="hero-title">{t(lang, 'home.title')}</h1>
      <p class="hero-subtitle">{t(lang, 'home.subtitle')}</p>
    </div>
    <DropZone {lang} disabled={busy || startupBusy} on:select={handleSelect} on:browse={handleBrowse} />
  {:else}
    <div class="pipeline-status">
      <div class="file-path-display">
        <span class="file-icon">📄</span>
        <span class="file-path">{inputPath}</span>
      </div>

      {#if reconInfo}
        <div class="recon-badge">{reconInfo}</div>
      {/if}

      <div class="status-indicator" class:analyzing={phase === 'analyzing'} class:tools={phase === 'tools'} class:decompiling={phase === 'decompiling'} class:done={phase === 'done'} class:error={phase === 'error'}>
        {#if phase === 'analyzing'}
          <span class="spinner"></span>
          <span class="status-text">{t(lang, 'home.analyzing')}</span>
        {:else if phase === 'tools'}
          <span class="spinner"></span>
          <span class="status-text">{t(lang, 'home.preparingTools')}</span>
        {:else if phase === 'decompiling'}
          <span class="spinner"></span>
          <span class="status-text">{t(lang, 'home.decompiling')}</span>
          {#if pipelineStepLabel}
            <span class="step-label">{pipelineStepLabel}</span>
          {/if}
          {#if pipelineProgress > 0}
            <div class="progress-bar-wrap">
              <div class="progress-bar-fill" style="width: {pipelineProgress}%"></div>
            </div>
          {/if}
        {:else if phase === 'done'}
          <span class="done-icon">✓</span>
          <span class="status-text">{t(lang, 'home.done')}</span>
        {:else if phase === 'error'}
          <span class="error-icon">!</span>
          <span class="status-text">{t(lang, 'home.error')}</span>
        {/if}
      </div>

      {#if phase === 'done' && outputPath}
        <div class="result-section">
          <span class="result-label">{t(lang, 'home.result')}:</span>
          <span class="result-path selectable">{outputPath}</span>
        </div>
      {/if}

      {#if phase === 'error' && errorMessage}
        <div class="error-msg">{errorMessage}</div>
      {/if}

      {#if phase === 'done' || phase === 'error'}
        <button class="new-file-btn" on:click={handleClear}>
          {t(lang, 'home.newFile')}
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .home-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 24px;
    height: 100%;
    padding: 32px;
  }
  .home-hero { text-align: center; }
  .hero-title {
    font-size: clamp(28px, 4vw, 56px);
    font-weight: 700;
    color: var(--text-primary);
    margin: 0 0 8px 0;
    letter-spacing: -0.5px;
  }
  .hero-subtitle {
    font-size: clamp(14px, 2vw, 28px);
    color: var(--text-muted);
    margin: 0;
  }

  .pipeline-status {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 20px;
    max-width: 560px;
    width: 100%;
  }

  .file-path-display {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 18px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    width: 100%;
    overflow: hidden;
  }
  .file-icon { font-size: 20px; flex-shrink: 0; }
  .file-path {
    font-size: 13px;
    font-family: ui-monospace, monospace;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    user-select: all;
  }

  .recon-badge {
    font-size: 11px;
    padding: 4px 10px;
    border-radius: 4px;
    background: var(--accent-dim);
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    font-weight: 600;
  }

  .status-indicator {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 24px;
    width: 100%;
    border-radius: 10px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
  }
  .status-indicator.done {
    border-color: var(--success, #22c55e);
  }
  .status-indicator.error {
    border-color: var(--error, #ef4444);
  }

  .status-text {
    font-size: 16px;
    font-weight: 600;
    color: var(--text-primary);
  }
  .step-label {
    font-size: 12px;
    color: var(--text-muted);
  }

  .spinner {
    width: 24px; height: 24px;
    border: 3px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  .done-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px; height: 32px;
    border-radius: 50%;
    background: var(--success, #22c55e);
    color: #fff;
    font-size: 16px;
    font-weight: 700;
  }

  .error-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px; height: 32px;
    border-radius: 50%;
    background: var(--error, #ef4444);
    color: #fff;
    font-size: 16px;
    font-weight: 700;
  }

  .progress-bar-wrap {
    width: 100%;
    height: 6px;
    background: var(--border);
    border-radius: 3px;
    overflow: hidden;
  }
  .progress-bar-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 3px;
    transition: width 0.3s ease;
  }

  .result-section {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-secondary);
    padding: 10px 16px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    width: 100%;
  }
  .result-label { font-weight: 600; color: var(--text-primary); flex-shrink: 0; }
  .result-path {
    font-family: ui-monospace, monospace;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    user-select: all;
  }

  .error-msg {
    font-size: 12px;
    color: var(--error, #ef4444);
    font-family: ui-monospace, monospace;
    padding: 10px 14px;
    background: rgba(255, 51, 102, 0.08);
    border-radius: 6px;
    width: 100%;
    word-break: break-word;
  }

  .new-file-btn {
    all: unset;
    font-size: 13px;
    padding: 10px 24px;
    border-radius: 8px;
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    cursor: pointer;
    transition: box-shadow 0.15s;
  }
  .new-file-btn:hover {
    box-shadow: 0 0 16px var(--accent-dim);
  }

  @keyframes spin { to { transform: rotate(360deg); } }
</style>
