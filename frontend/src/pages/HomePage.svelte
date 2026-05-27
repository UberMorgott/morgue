<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import PipelineProgress from '../components/PipelineProgress.svelte';
  import PipelineHistory from '../components/PipelineHistory.svelte';
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

  let { lang = 'en' as Lang, inputPath = '', startupBusy = false, onselect, onbrowse, onclear }: {
    lang?: Lang;
    inputPath?: string;
    startupBusy?: boolean;
    onselect?: (detail: { path: string }) => void;
    onbrowse?: () => void;
    onclear?: () => void;
  } = $props();

  let lastProcessedPath = $state('');

  // Accumulating panel: once a section is shown, it stays visible for the run
  let showDetection = $state(false);
  let showTools = $state(false);
  let showExecution = $state(false);
  let showSummary = $state(false);

  function resetSections() {
    showDetection = false;
    showTools = false;
    showExecution = false;
    showSummary = false;
  }

  let phase = $derived($pipelineState.phase);
  let paused = $derived($pipelineState.paused);
  let running = $derived(phase !== 'idle' && phase !== 'done' && phase !== 'error' && phase !== 'cancelled');

  // Elapsed time ticker
  let elapsedTimer: ReturnType<typeof setInterval> | null = null;
  let elapsed = $state('');

  function updateElapsed() {
    if ($pipelineState.startedAt > 0) {
      const sec = Math.floor((Date.now() - $pipelineState.startedAt) / 1000);
      if (sec < 60) elapsed = `${sec}s`;
      else if (sec < 3600) elapsed = `${Math.floor(sec / 60)}m ${sec % 60}s`;
      else elapsed = `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`;
    }
  }

  // Section visibility effects
  $effect(() => {
    if (['scan', 'recon'].includes($pipelineState.phase)) showDetection = true;
  });
  $effect(() => {
    if ($pipelineState.phase === 'tools' || $pipelineState.toolsNeeded.length > 0) showTools = true;
  });
  $effect(() => {
    if ($pipelineState.phase === 'execute') showExecution = true;
  });
  $effect(() => {
    if ($pipelineState.phase === 'done') showSummary = true;
  });

  // Clear guard when inputPath is reset
  $effect(() => {
    if (!inputPath) lastProcessedPath = '';
  });

  // Auto-start pipeline when inputPath changes
  $effect(() => {
    if (inputPath && inputPath !== lastProcessedPath && !running && !startupBusy && phase === 'idle') {
      lastProcessedPath = inputPath;
      runPipeline();
    }
  });

  // API run signal
  let prevApiRunSeq = 0;
  $effect(() => {
    if ($apiRunSeq > prevApiRunSeq) {
      prevApiRunSeq = $apiRunSeq;
      if (inputPath) {
        resetSections();
        lastProcessedPath = inputPath;
        runPipeline();
      }
    }
  });

  // Elapsed timer management
  $effect(() => {
    if (running && !paused && $pipelineState.startedAt > 0) {
      if (!elapsedTimer) {
        elapsedTimer = setInterval(updateElapsed, 1000);
      }
    } else {
      if (elapsedTimer) {
        clearInterval(elapsedTimer);
        elapsedTimer = null;
        updateElapsed();
      }
    }
  });

  onDestroy(() => {
    if (elapsedTimer) clearInterval(elapsedTimer);
  });

  function handleSelect(detail: { path: string }) {
    onselect?.(detail);
  }

  function handleBrowse() {
    onbrowse?.();
  }

  function handleClear() {
    resetPipeline();
    resetSections();
    lastProcessedPath = '';
    if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null; }
    elapsed = '';
    onclear?.();
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

  onMount(async () => {
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
    <DropZone {lang} disabled={running || startupBusy} onselect={handleSelect} onbrowse={handleBrowse} />

    {#if $history.length > 0}
      <PipelineHistory {lang} entries={$history} onselect={(detail) => onselect?.(detail)} />
    {/if}

  {:else}
    <!-- RUNNING / DONE / ERROR STATE -->
    <PipelineProgress
      {lang}
      {inputPath}
      {phase}
      {paused}
      {running}
      {elapsed}
      {showDetection}
      {showTools}
      {showExecution}
      {showSummary}
      onpause={handlePause}
      onresume={handleResume}
      oncancel={handleCancel}
    />

    <!-- New file button (done/cancelled/error) -->
    {#if phase === 'done' || phase === 'cancelled' || phase === 'error'}
      <button class="btn btn-primary" onclick={handleClear}>
        {t(lang, 'home.newFile')}
      </button>
    {/if}
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
</style>
