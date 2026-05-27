<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { onMount, onDestroy } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import { t, type Lang } from '../lib/i18n';
  import { PipelineService, ToolsService } from '../lib/api';
  import { addOperation, updateOperation } from '../lib/operations';
  import {
    pipelineState,
    history,
    startPipeline as storeStartPipeline,
    resetPipeline,
    addHistoryEntry,
    type PipelinePhase,
  } from '../lib/pipeline';

  export let lang: Lang = 'en';
  export let inputPath: string = '';
  export let startupBusy: boolean = false;

  const dispatch = createEventDispatcher();

  type StageId = 'scan' | 'recon' | 'tools' | 'execute' | 'done';
  type StageStatus = 'pending' | 'active' | 'done' | 'error';

  const stageIds: StageId[] = ['scan', 'recon', 'tools', 'execute', 'done'];

  let lastProcessedPath = '';
  let pipelineOpAdded = false;
  let logEl: HTMLDivElement | null = null;

  $: phase = $pipelineState.phase;
  $: paused = $pipelineState.paused;
  $: running = phase !== 'idle' && phase !== 'done' && phase !== 'error' && phase !== 'cancelled';

  // Auto-start pipeline when inputPath changes
  $: if (inputPath && inputPath !== lastProcessedPath && !running && !startupBusy && phase === 'idle') {
    lastProcessedPath = inputPath;
    runPipeline();
  }

  // Compute stage statuses from pipeline phase
  $: stages = computeStages(phase);

  // Elapsed time ticker
  let elapsedTimer: ReturnType<typeof setInterval> | null = null;
  let elapsed = '';

  $: if (running && !paused && $pipelineState.startedAt > 0) {
    if (!elapsedTimer) {
      elapsedTimer = setInterval(() => {
        const sec = Math.floor((Date.now() - $pipelineState.startedAt) / 1000);
        if (sec < 60) elapsed = `${sec}s`;
        else if (sec < 3600) elapsed = `${Math.floor(sec / 60)}m ${sec % 60}s`;
        else elapsed = `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
      }, 1000);
    }
  } else {
    if (elapsedTimer) {
      clearInterval(elapsedTimer);
      elapsedTimer = null;
      // Compute final elapsed so summary shows correct duration
      if ($pipelineState.startedAt > 0) {
        const sec = Math.floor((Date.now() - $pipelineState.startedAt) / 1000);
        if (sec < 60) elapsed = `${sec}s`;
        else if (sec < 3600) elapsed = `${Math.floor(sec / 60)}m ${sec % 60}s`;
        else elapsed = `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
      }
    }
  }

  $: if ($pipelineState.logs.length && logEl) {
    setTimeout(() => { if (logEl) logEl.scrollTop = logEl.scrollHeight; }, 0);
  }

  onDestroy(() => {
    if (elapsedTimer) clearInterval(elapsedTimer);
  });

  function computeStages(ph: PipelinePhase): Record<StageId, StageStatus> {
    const order: StageId[] = ['scan', 'recon', 'tools', 'execute', 'done'];
    const result: Record<string, StageStatus> = {};

    if (ph === 'idle') {
      for (const s of order) result[s] = 'pending';
      return result as Record<StageId, StageStatus>;
    }

    if (ph === 'cancelled' || ph === 'error') {
      // Find where we were and mark that as error, previous as done, rest pending
      const phaseToStage: Record<string, StageId> = {
        scan: 'scan', recon: 'recon', tools: 'tools', execute: 'execute', done: 'done',
      };
      // Use stored phase from error - we check pipelineState directly
      // Since phase is already 'error', we look at what stage was last active
      // We'll track this via a derived approach: mark based on progress
      let errorIdx = order.length - 2; // default to execute
      if ($pipelineState.step === 0 && $pipelineState.stepTotal === 0) {
        // Likely failed during early stage
        errorIdx = 0;
      }
      for (let i = 0; i < order.length; i++) {
        if (i < errorIdx) result[order[i]] = 'done';
        else if (i === errorIdx) result[order[i]] = 'error';
        else result[order[i]] = 'pending';
      }
      return result as Record<StageId, StageStatus>;
    }

    if (ph === 'done') {
      for (const s of order) result[s] = 'done';
      return result as Record<StageId, StageStatus>;
    }

    // Active phase
    const activeIdx = order.indexOf(ph as StageId);
    for (let i = 0; i < order.length; i++) {
      if (i < activeIdx) result[order[i]] = 'done';
      else if (i === activeIdx) result[order[i]] = 'active';
      else result[order[i]] = 'pending';
    }
    return result as Record<StageId, StageStatus>;
  }

  function handleSelect(e: CustomEvent<{ path: string }>) {
    dispatch('select', { path: e.detail.path });
  }

  function handleBrowse() {
    dispatch('browse');
  }

  function handleClear() {
    resetPipeline();
    pipelineOpAdded = false;
    lastProcessedPath = '';
    if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null; }
    elapsed = '';
    dispatch('clear');
  }

  async function handleCancel() {
    try {
      await PipelineService.Stop();
    } catch {}
    pipelineState.update(s => ({ ...s, phase: 'cancelled', paused: false }));
    updateOperation('pipeline', { status: 'failed', error: t(lang, 'home.cancelled') });
  }

  async function handlePause() {
    try {
      await PipelineService.Pause();
    } catch {}
    pipelineState.update(s => ({ ...s, paused: true }));
  }

  async function handleResume() {
    try {
      await PipelineService.Resume();
    } catch {}
    pipelineState.update(s => ({ ...s, paused: false }));
  }

  function handleHistoryClick(path: string) {
    dispatch('select', { path });
  }

  function relativeTime(ts: number): string {
    const diff = Math.floor((Date.now() - ts) / 1000);
    if (diff < 60) return `<1${t(lang, 'home.ago.minutes')}`;
    if (diff < 3600) return `${Math.floor(diff / 60)}${t(lang, 'home.ago.minutes')}`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}${t(lang, 'home.ago.hours')}`;
    return `${Math.floor(diff / 86400)}${t(lang, 'home.ago.days')}`;
  }

  function basename(p: string): string {
    return p.split(/[\\/]/).pop() || p;
  }

  onMount(async () => {
    // Sync with backend if pipeline already running
    if (inputPath) {
      try {
        const status = await PipelineService.GetStatus();
        if (status && (status.Running || status.running)) {
          lastProcessedPath = inputPath;
          pipelineOpAdded = false;
        }
      } catch {}
    }
  });

  async function runPipeline() {
    if (running) return;
    pipelineOpAdded = false;

    storeStartPipeline(inputPath);

    addOperation({ id: 'pipeline', type: 'pipeline', label: t(lang, 'home.decompiling'), status: 'running', progress: 0 });
    pipelineOpAdded = true;

    try {
      // Preflight: check runtimes (quick local check, no tool install)
      try {
        const rtStatuses = await ToolsService.CheckRuntimes();
        const missingRequired = (rtStatuses || []).filter((s: any) =>
          (s.Required || s.required) && !(s.Available || s.available)
        );
        if (missingRequired.length > 0) {
          pipelineState.update(s => ({ ...s, phase: 'error', error: t(lang, 'runtimes.missingForPipeline') }));
          updateOperation('pipeline', { status: 'failed', error: t(lang, 'runtimes.missingForPipeline') });
          return;
        }
      } catch {
        // Continue — pipeline will handle errors
      }

      // Engine handles scan→recon→match→install→execute with proper events
      await PipelineService.Run(inputPath, '');

    } catch (e: any) {
      const currentPhase = $pipelineState.phase;
      if (currentPhase === 'done' || currentPhase === 'cancelled') {
        // Already handled by events
      } else {
        const msg = e.message || String(e);
        const isCancelled = msg.includes('context canceled') || msg.includes('context cancelled');
        if (isCancelled) {
          pipelineState.update(s => ({ ...s, phase: 'cancelled', paused: false }));
          updateOperation('pipeline', { status: 'failed', error: t(lang, 'home.cancelled') });
        } else {
          pipelineState.update(s => ({ ...s, phase: 'error', error: msg }));
          updateOperation('pipeline', { status: 'failed', error: msg });
        }
      }
    }
  }

  // Keep operations footer in sync with pipeline store progress
  $: if (pipelineOpAdded && $pipelineState.step > 0) {
    const label = `${t(lang, 'home.step')} ${$pipelineState.step + 1}/${$pipelineState.stepTotal}: ${$pipelineState.stepName}`;
    updateOperation('pipeline', { progress: $pipelineState.progress, label });
  }
</script>

<div class="home-page">
  {#if !inputPath}
    <!-- IDLE STATE -->
    <div class="home-hero">
      <h1 class="hero-title">{t(lang, 'home.title')}</h1>
      <p class="hero-subtitle">{t(lang, 'home.subtitle')}</p>
    </div>
    <DropZone {lang} disabled={running || startupBusy} on:select={handleSelect} on:browse={handleBrowse} />

    {#if $history.length > 0}
      <div class="history-section animate-in">
        <h3 class="history-heading">{t(lang, 'home.recent')}</h3>
        <div class="history-list glass">
          {#each $history.slice(0, 5) as entry}
            <button class="history-item" on:click={() => handleHistoryClick(entry.path)}>
              <span class="dot {entry.success ? 'dot-success' : 'dot-error'}"></span>
              <span class="history-name">{basename(entry.path)}</span>
              {#if entry.kind}
                <span class="tag tag-accent">{entry.kind}</span>
              {/if}
              <span class="history-time">{relativeTime(entry.timestamp)}</span>
            </button>
          {/each}
        </div>
      </div>
    {/if}

  {:else}
    <!-- RUNNING / DONE / ERROR STATE -->
    <div class="pipeline-view animate-in">
      <!-- File info bar -->
      <div class="file-info glass">
        <span class="file-path selectable">{inputPath}</span>
        {#if $pipelineState.reconResults.length > 0 && $pipelineState.reconResults[0].kind && $pipelineState.reconResults[0].kind !== 'Unknown'}
          <span class="tag tag-accent">{$pipelineState.reconResults[0].kind}</span>
        {/if}
      </div>

      <!-- Stage stepper -->
      <div class="stepper">
        {#each stageIds as id, i}
          {@const status = stages[id]}
          <div class="stage" class:stage-done={status === 'done'} class:stage-active={status === 'active'} class:stage-error={status === 'error'} class:stage-pending={status === 'pending'}>
            <div class="stage-circle">
              {#if status === 'done'}
                <svg width="14" height="14" viewBox="0 0 14 14"><path d="M2.5 7l3 3 6-6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
              {:else if status === 'error'}
                <svg width="14" height="14" viewBox="0 0 14 14"><path d="M3.5 3.5l7 7M10.5 3.5l-7 7" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/></svg>
              {:else}
                <span class="stage-dot"></span>
              {/if}
            </div>
            <span class="stage-label">{t(lang, `home.stage.${id}`)}</span>
          </div>
          {#if i < stageIds.length - 1}
            {@const nextStatus = stages[stageIds[i + 1]]}
            <div class="stage-line"
              class:line-done={status === 'done' && (nextStatus === 'done' || nextStatus === 'active')}
              class:line-active={status === 'done' && nextStatus === 'active'}
              class:line-error={status === 'error' || nextStatus === 'error'}
            ></div>
          {/if}
        {/each}
      </div>

      <!-- Progress details (phase-specific) -->
      {#if running}
        <div class="progress-card glass">
          <!-- Phase-specific details -->
          {#if phase === 'scan'}
            <div class="phase-detail">
              <span class="spinner"></span>
              <span class="phase-text">{$pipelineState.scanInfo || t(lang, 'home.scanning')}</span>
            </div>

          {:else if phase === 'recon'}
            <div class="phase-detail-list">
              {#each $pipelineState.reconResults as r}
                <div class="recon-item">
                  <span class="recon-file">{r.file}</span>
                  {#if r.kind}
                    <span class="tag tag-accent">{r.kind}</span>
                  {:else}
                    <span class="spinner-sm"></span>
                  {/if}
                </div>
              {/each}
              {#if $pipelineState.reconResults.length === 0}
                <div class="fallback-row">
                  <span class="spinner"></span>
                  <span class="fallback-text">{t(lang, 'home.classifying')}</span>
                </div>
              {/if}
            </div>

          {:else if phase === 'tools'}
            <div class="phase-detail">
              <span class="spinner"></span>
              <span class="phase-text">{$pipelineState.toolsInfo || t(lang, 'home.preparingTools')}</span>
            </div>

          {:else if phase === 'execute'}
            <!-- Current target -->
            {#if $pipelineState.currentTarget}
              <div class="target-row">
                <span class="target-icon">&#9654;</span>
                <span class="target-name">{basename($pipelineState.currentTarget)}</span>
              </div>
            {/if}

            <!-- Files counter -->
            {#if $pipelineState.filesTotal > 0}
              <div class="files-row">
                <div class="files-counter">
                  <span class="files-num">{$pipelineState.filesProcessed}</span>
                  <span class="files-sep">/</span>
                  <span class="files-total">{$pipelineState.filesTotal}</span>
                </div>
                <span class="files-label">{t(lang, 'home.filesProcessed')}</span>
                <div class="files-progress-track">
                  <div class="files-progress-fill" style="width: {$pipelineState.filesTotal > 0 ? ($pipelineState.filesProcessed / $pipelineState.filesTotal) * 100 : 0}%"></div>
                </div>
              </div>
            {/if}

            <!-- Step progress -->
            {#if $pipelineState.stepTotal > 0}
              <div class="step-row">
                <div class="step-info">
                  <span class="step-badge">{$pipelineState.step + 1}/{$pipelineState.stepTotal}</span>
                  <span class="step-name">{$pipelineState.stepName}</span>
                </div>
                <div class="progress-track">
                  <div class="progress-fill" style="width: {$pipelineState.progress}%; height: 6px;"></div>
                </div>
              </div>
            {:else}
              <div class="fallback-row">
                <span class="spinner"></span>
                <span class="fallback-text">{t(lang, 'home.processing')}</span>
              </div>
            {/if}

          {:else}
            <div class="fallback-row">
              <span class="spinner"></span>
              <span class="fallback-text">{t(lang, 'home.processing')}</span>
            </div>
          {/if}

          <!-- Status message + elapsed (always at bottom) -->
          <div class="status-row">
            {#if paused}
              <span class="status-msg paused-label">{t(lang, 'home.paused')}</span>
            {:else if $pipelineState.lastMessage}
              <span class="status-msg">{$pipelineState.lastMessage}</span>
            {/if}
            {#if $pipelineState.startedAt > 0}
              <span class="elapsed">{elapsed}</span>
            {/if}
          </div>

          <!-- Pipeline controls -->
          <div class="pipeline-controls">
            {#if paused}
              <button class="btn btn-sm btn-accent" on:click={handleResume}>
                {t(lang, 'home.resume')}
              </button>
            {:else}
              <button class="btn btn-sm btn-muted" on:click={handlePause}>
                {t(lang, 'home.pause')}
              </button>
            {/if}
            <button class="btn btn-sm btn-danger" on:click={handleCancel}>
              {t(lang, 'home.cancel')}
            </button>
          </div>
        </div>

        <!-- Live log -->
        {#if $pipelineState.logs.length > 0}
          <div class="log-panel" bind:this={logEl}>
            <pre class="log-text">{$pipelineState.logs.join('\n')}</pre>
          </div>
        {/if}
      {/if}

      <!-- Done state: summary -->
      {#if phase === 'done'}
        <div class="summary-panel glass">
          <div class="summary-header">
            <span class="summary-title">{t(lang, 'home.summary')}</span>
            {#if $pipelineState.startedAt > 0}
              <span class="summary-duration">{elapsed}</span>
            {/if}
          </div>

          <!-- Stats row -->
          <div class="summary-stats">
            <div class="stat">
              <span class="stat-num">{$pipelineState.filesProcessed || $pipelineState.reconResults.length}</span>
              <span class="stat-label">{t(lang, 'home.summary.files')}</span>
            </div>
            <div class="stat">
              <span class="stat-num">{$pipelineState.reconResults.filter(r => r.kind && r.kind !== 'Skipped' && r.kind !== 'Unknown').length}</span>
              <span class="stat-label">{t(lang, 'home.summary.decompiled')}</span>
            </div>
            <div class="stat">
              <span class="stat-num">{$pipelineState.reconResults.filter(r => r.kind === 'Skipped').length}</span>
              <span class="stat-label">{t(lang, 'home.summary.skipped')}</span>
            </div>
          </div>

          <!-- Per-file results -->
          <div class="summary-files">
            {#each $pipelineState.reconResults as r}
              <div class="summary-file">
                <span class="dot dot-success"></span>
                <span class="summary-filename">{r.file}</span>
                <span class="tag {r.kind === 'Skipped' ? 'tag-muted' : 'tag-accent'}">{r.kind || '?'}</span>
              </div>
            {/each}
          </div>

          <!-- Output path -->
          <div class="summary-output">
            <span class="result-label">{t(lang, 'home.result')}:</span>
            <span class="result-path selectable">{$pipelineState.outputPath || inputPath}</span>
          </div>

          <!-- Log tail -->
          {#if $pipelineState.logs.length > 0}
            <details class="summary-logs">
              <summary class="logs-toggle">{t(lang, 'home.summary.log')} ({$pipelineState.logs.length})</summary>
              <div class="logs-body">
                <pre class="log-text">{$pipelineState.logs.join('\n')}</pre>
              </div>
            </details>
          {/if}
        </div>

        <button class="btn btn-primary" on:click={handleClear}>
          {t(lang, 'home.newFile')}
        </button>
      {/if}

      <!-- Cancelled state -->
      {#if phase === 'cancelled'}
        <div class="error-card glass cancelled-card">
          <span class="cancelled-text">{t(lang, 'home.cancelled')}</span>
        </div>
        <button class="btn btn-primary" on:click={handleClear}>
          {t(lang, 'home.newFile')}
        </button>
      {/if}

      <!-- Error state -->
      {#if phase === 'error'}
        <div class="error-card glass">
          <span class="error-text">{$pipelineState.error}</span>
        </div>
        <button class="btn btn-primary" on:click={handleClear}>
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
    gap: 24px;
    height: 100%;
    padding: 32px;
    overflow-y: auto;
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

  /* ── History ── */
  .history-section {
    width: 100%;
    max-width: 480px;
  }
  .history-heading {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin: 0 0 8px 4px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .history-list {
    display: flex;
    flex-direction: column;
    padding: 4px;
    gap: 2px;
  }
  .history-item {
    all: unset;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background 0.15s;
  }
  .history-item:hover {
    background: var(--bg-card-hover);
  }
  .history-name {
    font-size: 0.88rem;
    color: var(--text-primary);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: 'Consolas', 'Courier New', monospace;
  }
  .history-time {
    font-size: 0.75rem;
    color: var(--text-muted);
    flex-shrink: 0;
  }

  /* ── Pipeline view ── */
  .pipeline-view {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 20px;
    width: 100%;
  }

  .file-info {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 18px;
    width: 100%;
    overflow: hidden;
  }
  .file-path {
    font-size: 13px;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  /* ── Stage stepper ── */
  .stepper {
    display: flex;
    align-items: flex-start;
    width: 100%;
    padding: 8px 0;
  }

  .stage {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    z-index: 1;
  }

  .stage-circle {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 2px solid var(--border);
    background: var(--bg-card-solid);
    color: var(--text-muted);
    transition: all 0.3s ease;
  }

  .stage-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-muted);
    opacity: 0.4;
  }

  .stage-label {
    font-size: 0.72rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
    font-weight: 500;
    transition: color 0.3s;
    white-space: nowrap;
  }

  /* Stage states */
  .stage-done .stage-circle {
    border-color: var(--success);
    background: var(--success-dim);
    color: var(--success);
    box-shadow: 0 0 8px rgba(85, 238, 160, 0.3);
  }
  .stage-done .stage-label {
    color: var(--success);
  }

  .stage-active .stage-circle {
    border-color: var(--accent);
    background: var(--accent-dim);
    color: var(--accent);
    box-shadow: 0 0 12px var(--accent-glow);
    animation: glow-breathe 2s ease-in-out infinite;
  }
  .stage-active .stage-dot {
    background: var(--accent);
    opacity: 1;
    animation: pulse-neon 2s ease-in-out infinite;
  }
  .stage-active .stage-label {
    color: var(--accent);
    font-weight: 600;
  }

  .stage-error .stage-circle {
    border-color: var(--error);
    background: var(--error-dim);
    color: var(--error);
    box-shadow: 0 0 8px rgba(255, 68, 102, 0.3);
  }
  .stage-error .stage-label {
    color: var(--error);
  }

  /* Connecting lines */
  .stage-line {
    flex: 1;
    height: 2px;
    background: var(--border-subtle);
    margin-top: 16px; /* center with circle */
    transition: background 0.3s;
  }
  .line-done {
    background: var(--success);
    box-shadow: 0 0 4px rgba(85, 238, 160, 0.3);
  }
  .line-active {
    background: var(--accent);
    box-shadow: 0 0 6px var(--accent-glow);
    animation: pulse-neon 2s ease-in-out infinite;
  }
  .line-error {
    background: var(--error);
  }

  /* ── Progress card ── */
  .progress-card {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 20px;
    width: 100%;
  }

  .target-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .target-icon {
    color: var(--accent);
    font-size: 0.7rem;
  }
  .target-name {
    font-size: 0.9rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-primary);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .files-row {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .files-counter {
    display: flex;
    align-items: baseline;
    gap: 2px;
  }
  .files-num {
    font-size: 1.3rem;
    font-weight: 700;
    color: var(--accent);
    font-family: 'Orbitron', monospace;
  }
  .files-sep {
    font-size: 1rem;
    color: var(--text-muted);
    margin: 0 2px;
  }
  .files-total {
    font-size: 1rem;
    color: var(--text-muted);
    font-family: 'Orbitron', monospace;
  }
  .files-label {
    font-size: 0.78rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  .files-progress-track {
    flex: 1;
    min-width: 60px;
    height: 4px;
    background: rgba(255, 140, 40, 0.08);
    border-radius: 2px;
    overflow: hidden;
  }
  .files-progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    transition: width 0.4s ease;
  }

  .step-row {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .step-info {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .step-badge {
    font-size: 0.75rem;
    padding: 2px 8px;
    border-radius: 4px;
    background: var(--accent-dim);
    color: var(--accent);
    font-weight: 600;
    font-family: 'Consolas', 'Courier New', monospace;
    flex-shrink: 0;
  }
  .step-name {
    font-size: 0.85rem;
    color: var(--text-primary);
    font-weight: 500;
  }

  .status-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 20px;
  }
  .status-msg {
    font-size: 0.78rem;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }
  .elapsed {
    font-size: 0.78rem;
    color: var(--text-muted);
    font-family: 'Consolas', 'Courier New', monospace;
    flex-shrink: 0;
  }

  .phase-detail {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .phase-text {
    font-size: 0.88rem;
    color: var(--text-primary);
  }

  .phase-detail-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .recon-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 4px 0;
  }
  .recon-file {
    font-size: 0.84rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .spinner-sm {
    width: 14px; height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
    flex-shrink: 0;
  }

  .fallback-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .spinner {
    width: 20px; height: 20px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .fallback-text {
    font-size: 0.85rem;
    color: var(--text-muted);
  }
  @keyframes spin { to { transform: rotate(360deg); } }

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

  /* ── Error card ── */
  .error-card {
    padding: 14px 18px;
    width: 100%;
    border-color: rgba(255, 68, 102, 0.25) !important;
  }
  .error-text {
    font-size: 0.82rem;
    color: var(--error);
    font-family: 'Consolas', 'Courier New', monospace;
    word-break: break-word;
  }

  /* Shimmer on active progress bars */
  :global(.progress-fill) {
    position: relative;
    overflow: hidden;
  }
  :global(.progress-fill)::after {
    content: '';
    position: absolute;
    top: 0; left: -100%; width: 100%; height: 100%;
    background: linear-gradient(90deg, transparent, rgba(255,255,255,0.2), transparent);
    animation: shimmer 1.5s ease-in-out infinite;
  }
  @keyframes shimmer {
    0% { left: -100%; }
    100% { left: 200%; }
  }

  .log-panel {
    width: 100%;
    max-height: 200px;
    overflow-y: auto;
    padding: 14px 18px;
    background: var(--bg-card-solid);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-md);
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

  /* ── Summary panel ── */
  .summary-panel {
    width: 100%;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .summary-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .summary-title {
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: 1.1rem;
    font-weight: 600;
    color: var(--text-heading);
    letter-spacing: 1px;
  }
  .summary-duration {
    font-size: 0.9rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-muted);
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

  .summary-files {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .summary-file {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
  }
  .summary-filename {
    font-size: 0.88rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-primary);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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

  /* ── Pipeline controls ── */
  .pipeline-controls {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }

  .btn-sm {
    font-size: 0.78rem;
    padding: 6px 16px;
    border-radius: var(--radius-sm);
    font-weight: 600;
    cursor: pointer;
    border: 1px solid transparent;
    transition: all 0.15s;
  }
  .btn-accent {
    background: var(--accent-dim);
    color: var(--accent);
    border-color: var(--accent);
  }
  .btn-accent:hover {
    background: var(--accent);
    color: var(--bg-card-solid);
  }
  .btn-muted {
    background: var(--bg-card);
    color: var(--text-secondary);
    border-color: var(--border);
  }
  .btn-muted:hover {
    background: var(--bg-card-hover);
    color: var(--text-primary);
  }
  .btn-danger {
    background: var(--error-dim, rgba(255, 68, 102, 0.1));
    color: var(--error);
    border-color: rgba(255, 68, 102, 0.3);
  }
  .btn-danger:hover {
    background: var(--error);
    color: #fff;
  }

  .paused-label {
    color: var(--accent) !important;
    font-weight: 600;
  }

  .cancelled-card {
    border-color: rgba(255, 180, 40, 0.25) !important;
  }
  .cancelled-text {
    font-size: 0.88rem;
    color: var(--text-secondary);
    font-weight: 500;
  }
</style>
