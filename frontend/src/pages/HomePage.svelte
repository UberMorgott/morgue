<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { onMount, onDestroy } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import { t, type Lang } from '../lib/i18n';
  import { PipelineService, ToolsService } from '../lib/api';
  import { apiRunSeq } from '../lib/stores';

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
  let logEl: HTMLDivElement | null = null;

  // Accumulating panel: once a section is shown, it stays visible for the run
  let showDetection = false;
  let showTools = false;
  let showExecution = false;
  let showSummary = false;

  $: if (['scan', 'recon'].includes($pipelineState.phase)) showDetection = true;
  $: if ($pipelineState.phase === 'tools' || $pipelineState.toolsNeeded.length > 0) showTools = true;
  $: if ($pipelineState.phase === 'execute') showExecution = true;
  $: if ($pipelineState.phase === 'done') showSummary = true;

  // Reset section visibility on pipeline reset
  function resetSections() {
    showDetection = false;
    showTools = false;
    showExecution = false;
    showSummary = false;
  }

  $: phase = $pipelineState.phase;
  $: paused = $pipelineState.paused;
  $: running = phase !== 'idle' && phase !== 'done' && phase !== 'error' && phase !== 'cancelled';

  // Clear guard when inputPath is reset (e.g. API command resets before re-run)
  $: if (!inputPath) lastProcessedPath = '';

  // Auto-start pipeline when inputPath changes (user drag-drop / history click)
  $: if (inputPath && inputPath !== lastProcessedPath && !running && !startupBusy && phase === 'idle') {
    lastProcessedPath = inputPath;
    runPipeline();
  }

  // API run signal: App.svelte bumps apiRunSeq when an API "run" command
  // arrives. This bypasses the reactive guards above so the pipeline starts
  // reliably regardless of previous state or timing.
  let prevApiRunSeq = 0;
  $: if ($apiRunSeq > prevApiRunSeq) {
    prevApiRunSeq = $apiRunSeq;
    if (inputPath) {
      resetSections();
      lastProcessedPath = inputPath;
      runPipeline();
    }
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
    resetSections();

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

  function formatFileSize(bytes: number): string {
    if (!bytes) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  }

  onMount(async () => {
    // Sync with backend if pipeline already running
    if (inputPath) {
      try {
        const status = await PipelineService.GetStatus();
        if (status && status.running) {
          lastProcessedPath = inputPath;
      
        }
      } catch {}
    }
  });

  async function runPipeline() {
    if (running) return;


    storeStartPipeline(inputPath);

    try {
      // Preflight: check runtimes (quick local check, no tool install)
      try {
        const rtStatuses = await ToolsService.CheckRuntimes();
        const missingRequired = (rtStatuses || []).filter((s: any) =>
          s.Required && !s.Available
        );
        if (missingRequired.length > 0) {
          pipelineState.update(s => ({ ...s, phase: 'error', error: t(lang, 'runtimes.missingForPipeline') }));
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
        } else {
          pipelineState.update(s => ({ ...s, phase: 'error', error: msg }));
        }
      }
    }
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

      <!-- Accumulating progress panel -->
      {#if phase !== 'idle'}
        <div class="acc-panel glass">

          <!-- Section: Detection -->
          {#if showDetection}
            <div class="acc-section">
              <div class="acc-section-header">
                <svg class="acc-icon" width="16" height="16" viewBox="0 0 16 16"><circle cx="7" cy="7" r="5.5" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M11 11l3.5 3.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>
                <span class="acc-section-title">{t(lang, 'home.section.analysis')}</span>
                {#if ['scan', 'recon'].includes(phase)}
                  <span class="spinner-sm"></span>
                {/if}
              </div>

              <!-- Enriched recon info -->
              {#if $pipelineState.reconKind}
                <!-- Recon results: file list with kind badges -->
                {#each $pipelineState.reconResults as r}
                  <div class="acc-detail-row">
                    <span class="acc-detail-mono">{r.file}</span>
                    {#if $pipelineState.reconKind}
                      <span class="tag tag-kind">{$pipelineState.reconKind}</span>
                    {/if}
                    {#if r.kind}
                      <span class="tag {r.kind === 'Skipped' ? 'tag-muted' : r.kind === 'Unknown' ? 'tag-muted' : 'tag-accent'}">{r.kind}</span>
                    {/if}
                  </div>
                {/each}
                <!-- Detection meta: only show fields with actual values -->
                {@const metaItems = [
                  $pipelineState.compiler ? `Compiler: ${$pipelineState.compiler}` : '',
                  $pipelineState.obfuscator ? `Obfuscator: ${$pipelineState.obfuscator}` : '',
                  $pipelineState.fileSize ? `Size: ${formatFileSize($pipelineState.fileSize)}` : '',
                ].filter(Boolean)}
                {#if metaItems.length > 0}
                  <div class="acc-detail-row detect-meta">
                    {#each metaItems as item, i}
                      {#if i > 0}
                        <span class="detect-meta-sep">|</span>
                      {/if}
                      <span class="detect-meta-value">{item}</span>
                    {/each}
                  </div>
                {/if}
                {#if $pipelineState.recipeName}
                  <div class="acc-detail-row">
                    <span class="acc-detail-label">Recipe</span>
                    <span class="acc-detail-mono">{$pipelineState.recipeName}</span>
                    {#if $pipelineState.recipeDesc}
                      <span class="acc-detail-value muted">&mdash; {$pipelineState.recipeDesc}</span>
                    {/if}
                  </div>
                {/if}
              {:else}
                <!-- Fallback: old-style recon results while enriched data hasn't arrived -->
                {#each $pipelineState.reconResults as r}
                  <div class="acc-detail-row">
                    <span class="acc-detail-mono">{r.file}</span>
                    {#if r.kind}
                      <span class="tag {r.kind === 'Skipped' ? 'tag-muted' : r.kind === 'Unknown' ? 'tag-muted' : 'tag-accent'}">{r.kind}</span>
                    {:else}
                      <span class="spinner-sm"></span>
                    {/if}
                  </div>
                {/each}

                {#if $pipelineState.reconResults.length === 0 && ['scan', 'recon'].includes(phase)}
                  <div class="acc-detail-row">
                    <span class="acc-detail-value muted">{t(lang, 'home.classifying')}</span>
                  </div>
                {/if}
              {/if}
            </div>
          {/if}

          <!-- Section: Tools -->
          {#if showTools}
            <div class="acc-section">
              <div class="acc-section-header">
                <svg class="acc-icon" width="16" height="16" viewBox="0 0 16 16"><path d="M9.5 2.5l4 4-7.5 7.5H2v-4L9.5 2.5z" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linejoin="round"/></svg>
                <span class="acc-section-title">{t(lang, 'home.stage.tools')}</span>
                {#if phase === 'tools'}
                  <span class="spinner-sm"></span>
                {/if}
              </div>

              {#if $pipelineState.toolsNeeded.length > 0}
                <div class="tools-list">
                  {#each $pipelineState.toolsNeeded as tool}
                    {@const isInstalled = $pipelineState.toolsInstalled.includes(tool)}
                    {@const isInstalling = !isInstalled && $pipelineState.toolsInfo.includes(tool)}
                    <div class="tool-row">
                      <span class="tool-icon" class:tool-ready={isInstalled} class:tool-installing={isInstalling}>
                        {#if isInstalled}
                          <svg width="14" height="14" viewBox="0 0 14 14"><path d="M2.5 7l3 3 6-6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
                        {:else if isInstalling}
                          <span class="spinner-sm"></span>
                        {:else}
                          <span class="tool-pending-dot"></span>
                        {/if}
                      </span>
                      <span class="tool-name">{tool}</span>
                      <span class="tool-status" class:tool-status-ready={isInstalled} class:tool-status-installing={isInstalling}>
                        {isInstalled ? 'ready' : isInstalling ? 'installing...' : 'pending'}
                      </span>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="acc-detail-row">
                  <span class="acc-detail-value">{$pipelineState.toolsInfo || t(lang, 'home.preparingTools')}</span>
                  {#if phase !== 'tools'}
                    <svg class="acc-icon-ok" width="14" height="14" viewBox="0 0 14 14"><path d="M2.5 7l3 3 6-6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
                  {/if}
                </div>
              {/if}

              {#if $pipelineState.downloadingTool}
                <div class="dl-progress-row">
                  <span class="dl-tool-name">{$pipelineState.downloadingTool}</span>
                  {#if $pipelineState.downloadProgress >= 0}
                    <div class="dl-bar">
                      <div class="dl-bar-fill" style="width: {$pipelineState.downloadProgress}%"></div>
                    </div>
                    <span class="dl-pct">{$pipelineState.downloadProgress}%</span>
                  {:else}
                    <span class="dl-extracting">extracting...</span>
                  {/if}
                </div>
              {/if}
            </div>
          {/if}

          <!-- Section: Execution -->
          {#if showExecution}
            <div class="acc-section">
              <div class="acc-section-header">
                <svg class="acc-icon acc-icon-bolt" width="16" height="16" viewBox="0 0 16 16"><path d="M8.5 1L3 9h4.5L6.5 15 13 7H8.5L9.5 1z" stroke="currentColor" stroke-width="1.2" fill="none" stroke-linejoin="round"/></svg>
                <span class="acc-section-title">{t(lang, 'home.stage.execute')}</span>
                {#if phase === 'execute'}
                  <span class="spinner-sm"></span>
                {/if}
              </div>

              <!-- Current target -->
              {#if $pipelineState.currentTarget}
                <div class="acc-detail-row">
                  <span class="acc-detail-label">Target</span>
                  <span class="acc-detail-mono">{basename($pipelineState.currentTarget)}</span>
                </div>
              {/if}

              <!-- Files counter -->
              {#if $pipelineState.filesTotal > 0}
                <div class="acc-files-row">
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
                <div class="acc-step-row">
                  <div class="step-info">
                    <span class="step-badge">{$pipelineState.step + 1}/{$pipelineState.stepTotal}</span>
                    <span class="step-name">{$pipelineState.stepName}</span>
                  </div>
                  <div class="progress-track">
                    <div class="progress-fill" style="width: {$pipelineState.progress}%; height: 6px;"></div>
                  </div>
                </div>
              {:else if phase === 'execute'}
                <div class="acc-detail-row">
                  <span class="spinner-sm"></span>
                  <span class="acc-detail-value muted">{t(lang, 'home.processing')}</span>
                </div>
              {/if}

              <!-- Mini log terminal (last 5 lines) -->
              {#if $pipelineState.logs.length > 0}
                <div class="acc-log-mini" bind:this={logEl}>
                  <pre class="acc-log-text">{$pipelineState.logs.slice(-5).join('\n')}</pre>
                </div>
              {/if}
            </div>
          {/if}

          <!-- Section: Summary -->
          {#if showSummary}
            <div class="acc-section acc-section-summary">
              <div class="acc-section-header">
                <svg class="acc-icon acc-icon-done" width="16" height="16" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6.5" stroke="currentColor" stroke-width="1.5" fill="none"/><path d="M5 8l2 2 4-4" stroke="currentColor" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
                <span class="acc-section-title">{t(lang, 'home.summary')}</span>
                {#if $pipelineState.startedAt > 0}
                  <span class="acc-elapsed">{elapsed}</span>
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

              <!-- Output stats -->
              {#if $pipelineState.outputStats.length > 0}
                <div class="output-stats">
                  {#each $pipelineState.outputStats as line}
                    <div class="output-stat-line">{line}</div>
                  {/each}
                </div>
              {/if}

              <!-- Output path -->
              {#if $pipelineState.outputPath}
                <div class="summary-output">
                  <span class="result-label">Output:</span>
                  <span class="result-path selectable">{$pipelineState.outputPath}</span>
                </div>
              {/if}

              <!-- Full log (expandable) -->
              {#if $pipelineState.logs.length > 0}
                <details class="summary-logs">
                  <summary class="logs-toggle">{t(lang, 'home.summary.log')} ({$pipelineState.logs.length})</summary>
                  <div class="logs-body">
                    <pre class="log-text">{$pipelineState.logs.join('\n')}</pre>
                  </div>
                </details>
              {/if}
            </div>
          {/if}

          <!-- Status bar: message + elapsed + controls (shown while running) -->
          {#if running}
            <div class="acc-status-bar">
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
          {/if}

        </div>
      {/if}

      <!-- Cancelled state -->
      {#if phase === 'cancelled'}
        <div class="error-card glass cancelled-card">
          <span class="cancelled-text">{t(lang, 'home.cancelled')}</span>
        </div>
      {/if}

      <!-- Error state -->
      {#if phase === 'error'}
        <div class="error-card glass">
          <span class="error-text">{$pipelineState.error}</span>
        </div>
      {/if}

      <!-- New file button (done/cancelled/error) -->
      {#if phase === 'done' || phase === 'cancelled' || phase === 'error'}
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

  /* ── Stage stepper (CSS grid) ── */
  .stepper {
    display: grid;
    grid-template-columns: auto 1fr auto 1fr auto 1fr auto 1fr auto;
    align-items: start;
    width: 100%;
    padding: 8px 16px;
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
    border-color: transparent;
    background: var(--accent-dim);
    color: var(--accent);
    box-shadow: none;
    position: relative;
  }
  .stage-active .stage-circle::before {
    content: '';
    position: absolute;
    inset: -2px;
    border-radius: 50%;
    border: 2px solid transparent;
    border-top-color: var(--accent);
    border-right-color: var(--accent);
    animation: spin-neon 1s linear infinite;
    filter: drop-shadow(0 0 6px var(--accent));
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
  @keyframes spin-neon {
    to { transform: rotate(360deg); }
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
    width: 100%;
    height: 2px;
    background: var(--border-subtle);
    margin-top: 15px; /* center with 32px circle: (32-2)/2 = 15 */
    align-self: start;
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

  /* ── Accumulating panel ── */
  .acc-panel {
    display: flex;
    flex-direction: column;
    gap: 0;
    width: 100%;
    padding: 0;
    max-height: 60vh;
    overflow-y: auto;
  }

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
  .acc-icon {
    color: var(--accent);
    flex-shrink: 0;
  }
  .acc-icon-ok {
    color: var(--success);
    flex-shrink: 0;
  }
  .acc-icon-done {
    color: var(--success);
  }
  .acc-icon-bolt {
    color: var(--accent);
  }
  .acc-section-title {
    font-size: 0.82rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--text-heading);
    flex: 1;
  }

  .acc-detail-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 2px 0 2px 24px;
  }
  .acc-detail-label {
    font-size: 0.75rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
    flex-shrink: 0;
    min-width: 48px;
  }
  .acc-detail-value {
    font-size: 0.84rem;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .acc-detail-value.muted {
    color: var(--text-muted);
  }
  .acc-detail-mono {
    font-size: 0.84rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  .tag-kind {
    background: var(--accent-dim);
    color: var(--accent);
    border-color: var(--accent);
  }

  /* Detection meta row */
  .detect-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .detect-meta-value {
    font-size: 0.82rem;
    color: var(--text-primary);
    font-weight: 500;
  }
  .detect-meta-sep {
    color: var(--border);
    font-size: 0.75rem;
  }

  /* Tools list */
  .tools-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 0 0 0 24px;
  }
  .tool-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 0;
  }
  .tool-icon {
    width: 18px;
    height: 18px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--text-muted);
  }
  .tool-icon.tool-ready {
    color: var(--success);
  }
  .tool-icon.tool-installing {
    color: var(--accent);
  }
  .tool-pending-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid var(--border);
    background: transparent;
  }
  .tool-name {
    font-size: 0.84rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-primary);
    flex: 1;
  }
  .tool-status {
    font-size: 0.72rem;
    color: var(--text-muted);
    text-transform: lowercase;
    letter-spacing: 0.3px;
  }
  .tool-status-ready {
    color: var(--success);
  }
  .tool-status-installing {
    color: var(--accent);
  }

  /* Download progress */
  .dl-progress-row { display: flex; align-items: center; gap: 10px; margin-top: 8px; padding: 0 0 0 24px; }
  .dl-tool-name { font-size: 0.8rem; color: var(--text-secondary); min-width: 80px; }
  .dl-bar { flex: 1; height: 4px; background: var(--border-subtle); border-radius: 2px; overflow: hidden; }
  .dl-bar-fill { height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.3s; box-shadow: 0 0 6px var(--accent-glow-soft); }
  .dl-pct { font-size: 0.75rem; color: var(--accent); min-width: 36px; text-align: right; }
  .dl-extracting { font-size: 0.75rem; color: var(--text-muted); font-style: italic; }

  .acc-files-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 4px 0 4px 24px;
    flex-wrap: wrap;
  }
  .files-counter {
    display: flex;
    align-items: baseline;
    gap: 2px;
  }
  .files-num {
    font-size: 1.2rem;
    font-weight: 700;
    color: var(--accent);
    font-family: Consolas, ui-monospace, monospace;
  }
  .files-sep {
    font-size: 0.9rem;
    color: var(--text-muted);
    margin: 0 2px;
  }
  .files-total {
    font-size: 0.9rem;
    color: var(--text-muted);
    font-family: 'Orbitron', monospace;
  }
  .files-label {
    font-size: 0.75rem;
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

  .acc-step-row {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 0 0 0 24px;
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

  .acc-log-mini {
    margin: 4px 0 0 24px;
    padding: 8px 12px;
    background: var(--bg-card-solid);
    border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm);
    max-height: 120px;
    overflow-y: auto;
  }
  .acc-log-text {
    font-size: 12px;
    font-family: 'Consolas', 'Courier New', monospace;
    color: #c0b0a0;
    line-height: 1.6;
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
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

  /* ── Status bar (controls) ── */
  .acc-status-bar {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 20px;
    border-top: 1px solid var(--border-subtle);
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

  .spinner-sm {
    width: 14px; height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
    flex-shrink: 0;
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

  .log-text {
    font-size: 13px;
    font-family: 'Consolas', 'Courier New', monospace;
    color: #e0d0c0;
    line-height: 2;
    margin: 0;
    white-space: pre-wrap;
    word-break: break-all;
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
