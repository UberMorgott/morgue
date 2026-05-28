<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import { pipelineState, type PipelinePhase } from '../lib/pipeline';
  import PipelineStepper from './PipelineStepper.svelte';
  import StatsStrip from './StatsStrip.svelte';
  import CompositionPanel from './CompositionPanel.svelte';
  import ToolsPanel from './ToolsPanel.svelte';
  import ExecutionPanel from './ExecutionPanel.svelte';
  import PipelineSummary from './PipelineSummary.svelte';

  let { lang = 'en' as Lang, inputPath = '', phase, paused, running, elapsed = '',
    showAnalysis, showTools, showExecution, showSummary,
    onpause, onresume, oncancel }: {
    lang?: Lang;
    inputPath?: string;
    phase: PipelinePhase;
    paused: boolean;
    running: boolean;
    elapsed?: string;
    showAnalysis: boolean;
    showTools: boolean;
    showExecution: boolean;
    showSummary: boolean;
    onpause?: () => void;
    onresume?: () => void;
    oncancel?: () => void;
  } = $props();

  type StageId = 'analysis' | 'tools' | 'execute' | 'done';
  type StageStatus = 'pending' | 'active' | 'done' | 'error';

  const stageIds: StageId[] = ['analysis', 'tools', 'execute', 'done'];

  let stages = $derived(computeStages(phase));

  function computeStages(ph: PipelinePhase): Record<StageId, StageStatus> {
    const order: StageId[] = ['analysis', 'tools', 'execute', 'done'];
    const result: Record<string, StageStatus> = {};

    if (ph === 'idle') {
      for (const s of order) result[s] = 'pending';
      return result as Record<StageId, StageStatus>;
    }

    if (ph === 'cancelled' || ph === 'error') {
      // Determine which stage the error occurred in based on accumulated state
      let errorIdx = 0; // default: analysis
      const st = $pipelineState;
      if (st.step > 0 || st.stepTotal > 0 || st.logs.length > 0) {
        errorIdx = 2; // execute
      } else if (st.toolsNeeded.length > 0) {
        errorIdx = 1; // tools
      } else if (st.reconResults.length > 0) {
        errorIdx = 1; // tools (recon done, failed at tools)
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

    const activeIdx = order.indexOf(ph as StageId);
    for (let i = 0; i < order.length; i++) {
      if (i < activeIdx) result[order[i]] = 'done';
      else if (i === activeIdx) result[order[i]] = 'active';
      else result[order[i]] = 'pending';
    }
    return result as Record<StageId, StageStatus>;
  }

  // Build composition groups from reconResults
  let compositionGroups = $derived.by(() => {
    const map = new Map<string, { kind: string; language: string; count: number; examples: string[] }>();
    for (const r of $pipelineState.reconResults) {
      const key = `${r.reconKind}|${r.compiler}`;
      const existing = map.get(key);
      if (existing) {
        existing.count++;
        existing.examples.push(r.file);
      } else {
        map.set(key, {
          kind: r.reconKind || r.kind || 'Unknown',
          language: r.compiler || '',
          count: 1,
          examples: [r.file],
        });
      }
    }
    return Array.from(map.values());
  });

  // Compute stats for StatsStrip
  let statsPlatform = $derived($pipelineState.reconKind || 'Unknown');
  let statsTechStack = $derived($pipelineState.compiler || '');
  let statsFileCount = $derived($pipelineState.reconResults.length);
  let statsTotalSize = $derived(formatFileSize(
    $pipelineState.reconResults.reduce((sum, r) => sum + (r.size || 0), 0) || $pipelineState.fileSize
  ));
  let statsObfuscationCount = $derived($pipelineState.obfuscations.length);

  function formatFileSize(bytes: number): string {
    if (!bytes) return '0 B';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  }
</script>

<div class="pipeline-view animate-in">
  <!-- Stage stepper -->
  <PipelineStepper {stages} {stageIds} {lang} />

  <!-- Stats strip -->
  {#if showAnalysis && statsFileCount > 0}
    <StatsStrip
      {lang}
      platform={statsPlatform}
      techStack={statsTechStack}
      fileCount={statsFileCount}
      totalSize={statsTotalSize}
      obfuscationCount={statsObfuscationCount}
    />
  {/if}

  <!-- Two-column row: Composition + Tools -->
  {#if showAnalysis || showTools}
    <div class="row-2col">
      {#if showAnalysis && compositionGroups.length > 0}
        <CompositionPanel
          {lang}
          groups={compositionGroups}
          obfuscations={$pipelineState.obfuscations}
        />
      {/if}
      {#if showTools && $pipelineState.toolsNeeded.length > 0}
        <ToolsPanel
          {lang}
          toolsNeeded={$pipelineState.toolsNeeded}
          toolsInstalled={$pipelineState.toolsInstalled}
          downloadingTool={$pipelineState.downloadingTool}
          downloadProgress={$pipelineState.downloadProgress}
          downloadBytes={$pipelineState.downloadBytes}
          downloadTotalBytes={$pipelineState.downloadTotalBytes}
          lastMessage={$pipelineState.lastMessage}
        />
      {/if}
    </div>
  {/if}

  <!-- Execution panel -->
  {#if showExecution}
    <ExecutionPanel
      {lang}
      currentTarget={$pipelineState.currentTarget}
      step={$pipelineState.step}
      stepTotal={$pipelineState.stepTotal}
      stepName={$pipelineState.stepName}
      progress={$pipelineState.progress}
      logs={$pipelineState.logs}
      toolsNeeded={$pipelineState.toolsNeeded}
      execCounters={$pipelineState.execCounters}
    />
  {/if}

  <!-- Summary -->
  {#if showSummary}
    <div class="summary-wrap glass neon-border animate-in">
      <PipelineSummary {lang} state={$pipelineState} {elapsed} />
    </div>
  {/if}

  <!-- Status bar: message + elapsed + controls (shown while running) -->
  {#if running}
    <div class="status-bar glass">
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
          <button class="btn btn-sm btn-accent" onclick={onresume}>
            {t(lang, 'home.resume')}
          </button>
        {:else}
          <button class="btn btn-sm btn-muted" onclick={onpause}>
            {t(lang, 'home.pause')}
          </button>
        {/if}
        <button class="btn btn-sm btn-danger" onclick={oncancel}>
          {t(lang, 'home.cancel')}
        </button>
      </div>
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
</div>

<style>
  .pipeline-view {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 16px;
    width: 100%;
  }

  .row-2col {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 16px;
    width: 100%;
    align-items: stretch;
  }

  /* When only one child is present, let it span full width */
  .row-2col > :only-child {
    grid-column: 1 / -1;
  }

  .summary-wrap {
    width: 100%;
    padding: 0;
  }

  /* -- Status bar -- */
  .status-bar {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 20px;
    width: 100%;
  }
  .status-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 20px; }
  .status-msg { font-size: 0.78rem; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
  .elapsed { font-size: 0.78rem; color: var(--text-muted); font-family: 'Consolas', 'Courier New', monospace; flex-shrink: 0; }

  .pipeline-controls { display: flex; gap: 8px; justify-content: flex-end; }
  .btn-sm { font-size: 0.78rem; padding: 6px 16px; border-radius: var(--radius-sm); font-weight: 600; cursor: pointer; border: 1px solid transparent; transition: all 0.15s; }
  .btn-accent { background: var(--accent-dim); color: var(--accent); border-color: var(--accent); }
  .btn-accent:hover { background: var(--accent); color: var(--bg-card-solid); }
  .btn-muted { background: var(--bg-card); color: var(--text-secondary); border-color: var(--border); }
  .btn-muted:hover { background: var(--bg-card-hover); color: var(--text-primary); }
  .btn-danger { background: var(--error-dim, rgba(255, 68, 102, 0.1)); color: var(--error); border-color: rgba(255, 68, 102, 0.3); }
  .btn-danger:hover { background: var(--error); color: #fff; }

  .paused-label { color: var(--accent) !important; font-weight: 600; }

  /* -- Error card -- */
  .error-card { padding: 14px 18px; width: 100%; border-color: rgba(255, 68, 102, 0.25) !important; }
  .error-text { font-size: 0.82rem; color: var(--error); font-family: 'Consolas', 'Courier New', monospace; word-break: break-word; }
  .cancelled-card { border-color: rgba(255, 180, 40, 0.25) !important; }
  .cancelled-text { font-size: 0.88rem; color: var(--text-secondary); font-weight: 500; }
</style>
