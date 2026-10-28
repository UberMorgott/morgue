<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import PipelineProgress from '../components/PipelineProgress.svelte';
  import PipelineHistory from '../components/PipelineHistory.svelte';
  import { t, type Lang } from '../lib/i18n';
  import { PipelineService, ToolsService } from '../lib/api';
  import { RunRequest } from '../../bindings/github.com/UberMorgott/morgue/internal/runmodel/models.js';
  import { lastRunPath } from '../lib/stores';

  import {
    pipelineState,
    history,
    startPipeline as storeStartPipeline,
    resetPipeline,
    addHistoryEntry,
    elapsedMs,
    formatElapsed,
    targetTree,
    type PipelinePhase,
    type PipelineTarget,
  } from '../lib/pipeline';

  let { lang = 'en' as Lang, inputPath = '', outputPath = '', startupBusy = false, onselect, onbrowsefile, onbrowsedir, onclear }: {
    lang?: Lang;
    inputPath?: string;
    outputPath?: string;
    startupBusy?: boolean;
    onselect?: (detail: { path: string }) => void;
    onbrowsefile?: () => void;
    onbrowsedir?: () => void;
    onclear?: () => void;
  } = $props();

  // Accumulating panel: once a section is shown, it stays visible for the run
  // Restore visibility from current phase (survives tab switches)
  const phaseOrder: PipelinePhase[] = ['analysis', 'tools', 'execute', 'done'];

  function sectionsFromPhase(ph: PipelinePhase): [boolean, boolean, boolean, boolean] {
    if (ph === 'idle') return [false, false, false, false];
    const idx = phaseOrder.indexOf(ph);
    const past = (i: number) => idx >= 0 ? idx >= i : false;
    // error/cancelled: infer from accumulated state
    if (ph === 'error' || ph === 'cancelled') {
      const st = $pipelineState;
      return [
        st.reconResults.length > 0 || st.reconKind !== '',
        st.toolsNeeded.length > 0,
        st.step > 0 || st.logs.length > 0,
        false,
      ];
    }
    return [
      past(0) || $pipelineState.reconResults.length > 0,
      past(1) || $pipelineState.toolsNeeded.length > 0,
      past(2) || $pipelineState.logs.length > 0,
      ph === 'done',
    ];
  }

  let [initA, initT, initE, initS] = sectionsFromPhase($pipelineState.phase);
  let showAnalysis = $state(initA);
  let showTools = $state(initT);
  let showExecution = $state(initE);
  let showSummary = $state(initS);

  function resetSections() {
    showAnalysis = false;
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

  // Restore elapsed on mount if pipeline is running
  if ($pipelineState.startedAt > 0) updateElapsed();

  // Backend-owned start while running, frozen backend duration after
  // run.finished (see elapsedMs) — never the browser arrival time.
  function updateElapsed() {
    if ($pipelineState.startedAt > 0 || $pipelineState.outcome) {
      elapsed = formatElapsed(elapsedMs($pipelineState));
    }
  }

  // Re-render when run.started replaces the optimistic local start, and as
  // soon as the terminal event freezes the duration.
  $effect(() => {
    void $pipelineState.startedAt;
    void $pipelineState.outcome;
    updateElapsed();
  });

  // Nested target tree (events v2): shown once the run has more than one target.
  let targetRows = $derived(targetTree($pipelineState.targets.filter(t => t.id)));
  const outcomeKey: Record<PipelineTarget['status'], string> = {
    pending: 'targets.pending', running: 'targets.running', done: 'targets.success',
    error: 'targets.error', skipped: 'targets.skipped', cancelled: 'targets.cancelled',
  };

  // Section visibility effects
  $effect(() => {
    if ($pipelineState.phase === 'analysis') showAnalysis = true;
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
    if (!inputPath) $lastRunPath = '';
  });

  // Auto-start pipeline when inputPath changes. Guard persists in a store so
  // navigating away from Home and back does NOT re-trigger the run.
  $effect(() => {
    if (inputPath && inputPath !== $lastRunPath && !running && !startupBusy && phase === 'idle') {
      $lastRunPath = inputPath;
      runPipeline();
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

  function handleBrowseFile() {
    onbrowsefile?.();
  }

  function handleBrowseDir() {
    onbrowsedir?.();
  }

  function handleClear() {
    resetPipeline();
    resetSections();
    $lastRunPath = '';
    if (elapsedTimer) { clearInterval(elapsedTimer); elapsedTimer = null; }
    elapsed = '';
    onclear?.();
  }

  async function handleCancel() {
    try {
      await PipelineService.Stop();
    } catch (e) { console.error('pipeline stop failed:', e); }
    pipelineState.update(s => ({ ...s, phase: 'cancelled', paused: false }));
  }

  async function handlePause() {
    try {
      await PipelineService.Pause();
    } catch (e) { console.error('pipeline pause failed:', e); }
    pipelineState.update(s => ({ ...s, paused: true }));
  }

  async function handleResume() {
    try {
      await PipelineService.Resume();
    } catch (e) { console.error('pipeline resume failed:', e); }
    pipelineState.update(s => ({ ...s, paused: false }));
  }

  onMount(async () => {
    try {
      const status = await PipelineService.GetStatus();
      if (status && status.running) {
        if (inputPath) $lastRunPath = inputPath;
        // Reconnect: adopt the backend run identity and its backend-owned start
        // so elapsed is measured from the true run start, not the arrival time
        // of the first event we happen to observe after reattaching.
        pipelineState.update(s => {
          const backendStart = status.startedAt ? Date.parse(status.startedAt) : NaN;
          if (status.runId && status.runId !== s.runId) {
            return {
              ...s,
              runId: status.runId,
              lastSeq: status.lastSeq ?? 0,
              startedAt: Number.isFinite(backendStart) ? backendStart : s.startedAt,
              phase: s.phase === 'idle' ? 'analysis' : s.phase,
            };
          }
          // Same run already adopted via events: still correct a fallback start.
          if (Number.isFinite(backendStart) && backendStart !== s.startedAt) {
            return { ...s, startedAt: backendStart };
          }
          return s;
        });
      }
    } catch (e) { console.error('pipeline status check failed:', e); }
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

      // StartSpec admits the run (shared RunRequest admission), reserves the
      // slot and executes in the background, returning the backend handle. The
      // typed v2 event stream drives the rest of the UI; execution errors arrive
      // as run.finished, not as a rejected promise.
      const handle = await PipelineService.StartSpec(new RunRequest({ path: inputPath, output: outputPath }));
      // Adopt the backend run identity and clock immediately so elapsed is
      // backend-owned even before the first event is projected.
      pipelineState.update(s => {
        const backendStart = handle?.startedAt ? Date.parse(handle.startedAt) : NaN;
        return {
          ...s,
          runId: handle?.runId || s.runId,
          startedAt: Number.isFinite(backendStart) ? backendStart : s.startedAt,
        };
      });

    } catch (e: any) {
      const currentPhase = $pipelineState.phase;
      const msg = e?.message || String(e);
      // A concurrent run already holds the slot: the GUI auto-start is a no-op,
      // the active run keeps driving the UI.
      if (msg.toLowerCase().includes('busy')) return;
      // run.finished already set the authoritative outcome.
      if ($pipelineState.outcome || currentPhase === 'done' || currentPhase === 'cancelled') {
        // Already handled by events
      } else {
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
    <DropZone {lang} disabled={running || startupBusy} onselect={handleSelect} onbrowsefile={handleBrowseFile} onbrowsedir={handleBrowseDir} />

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
      {showAnalysis}
      {showTools}
      {showExecution}
      {showSummary}
      onpause={handlePause}
      onresume={handleResume}
      oncancel={handleCancel}
    />

    {#if targetRows.length > 1}
      <section class="glass pipeline-panel target-tree animate-in">
        <h3 class="panel-title">{t(lang, 'targets.title')}</h3>
        <ul class="target-list">
          {#each targetRows as row (row.target.id)}
            <li class="target-row row-separator text-sm" style:padding-left="{row.depth * 18}px">
              <span class="target-name font-mono" title={row.target.path}>{row.target.relativeInput || row.target.path}</span>
              {#if row.target.recipe}<span class="target-recipe text-xs">{row.target.recipe}</span>{/if}
              <span class="target-outcome text-xs status-{row.target.status}">{t(lang, outcomeKey[row.target.status])}</span>
              {#if row.target.error}<span class="target-error text-xs font-mono">{row.target.error}</span>{/if}
              {#if row.target.skipReason}<span class="target-skip text-xs">{row.target.skipReason}</span>{/if}
            </li>
          {/each}
        </ul>
      </section>
    {/if}

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

  .target-tree { width: 100%; }
  .target-list { list-style: none; margin: 0; padding: 0; }
  .target-row { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 10px; padding-block: 6px; }
  .target-name { color: var(--text-primary); overflow-wrap: anywhere; }
  .target-recipe, .target-skip { color: var(--text-muted); }
  .target-outcome { margin-left: auto; }
  .target-error { flex-basis: 100%; color: var(--error); overflow-wrap: anywhere; }
  .status-done { color: var(--success); }
  .status-error { color: var(--error); }
  .status-cancelled, .status-skipped { color: var(--warning); }
  .status-running { color: var(--accent); }
  .status-pending { color: var(--text-muted); }

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
