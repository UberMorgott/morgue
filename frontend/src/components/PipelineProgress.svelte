<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import { pipelineState, type PipelinePhase, type PipelineState } from '../lib/pipeline';
  import PipelineSummary from './PipelineSummary.svelte';

  let { lang = 'en' as Lang, inputPath = '', phase, paused, running, elapsed = '',
    showDetection, showTools, showExecution, showSummary,
    onpause, onresume, oncancel }: {
    lang?: Lang;
    inputPath?: string;
    phase: PipelinePhase;
    paused: boolean;
    running: boolean;
    elapsed?: string;
    showDetection: boolean;
    showTools: boolean;
    showExecution: boolean;
    showSummary: boolean;
    onpause?: () => void;
    onresume?: () => void;
    oncancel?: () => void;
  } = $props();

  type StageId = 'scan' | 'recon' | 'tools' | 'execute' | 'done';
  type StageStatus = 'pending' | 'active' | 'done' | 'error';

  const stageIds: StageId[] = ['scan', 'recon', 'tools', 'execute', 'done'];

  let logEl: HTMLDivElement | null = $state(null);

  let stages = $derived(computeStages(phase));

  // Auto-scroll log
  $effect(() => {
    if ($pipelineState.logs.length && logEl) {
      setTimeout(() => { if (logEl) logEl.scrollTop = logEl.scrollHeight; }, 0);
    }
  });

  function computeStages(ph: PipelinePhase): Record<StageId, StageStatus> {
    const order: StageId[] = ['scan', 'recon', 'tools', 'execute', 'done'];
    const result: Record<string, StageStatus> = {};

    if (ph === 'idle') {
      for (const s of order) result[s] = 'pending';
      return result as Record<StageId, StageStatus>;
    }

    if (ph === 'cancelled' || ph === 'error') {
      let errorIdx = order.length - 2;
      if ($pipelineState.step === 0 && $pipelineState.stepTotal === 0) {
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

    const activeIdx = order.indexOf(ph as StageId);
    for (let i = 0; i < order.length; i++) {
      if (i < activeIdx) result[order[i]] = 'done';
      else if (i === activeIdx) result[order[i]] = 'active';
      else result[order[i]] = 'pending';
    }
    return result as Record<StageId, StageStatus>;
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
</script>

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

          {#if $pipelineState.reconKind}
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

          {#if $pipelineState.currentTarget}
            <div class="acc-detail-row">
              <span class="acc-detail-label">Target</span>
              <span class="acc-detail-mono">{basename($pipelineState.currentTarget)}</span>
            </div>
          {/if}

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

          {#if $pipelineState.logs.length > 0}
            <div class="acc-log-mini" bind:this={logEl}>
              <pre class="acc-log-text">{$pipelineState.logs.slice(-5).join('\n')}</pre>
            </div>
          {/if}
        </div>
      {/if}

      <!-- Section: Summary -->
      {#if showSummary}
        <PipelineSummary {lang} state={$pipelineState} {elapsed} />
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

  /* -- Stage stepper (CSS grid) -- */
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

  .stage-done .stage-circle {
    border-color: var(--success);
    background: var(--success-dim);
    color: var(--success);
    box-shadow: 0 0 8px rgba(85, 238, 160, 0.3);
  }
  .stage-done .stage-label { color: var(--success); }

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
  .stage-active .stage-label { color: var(--accent); font-weight: 600; }
  @keyframes spin-neon { to { transform: rotate(360deg); } }

  .stage-error .stage-circle {
    border-color: var(--error);
    background: var(--error-dim);
    color: var(--error);
    box-shadow: 0 0 8px rgba(255, 68, 102, 0.3);
  }
  .stage-error .stage-label { color: var(--error); }

  .stage-line {
    width: 100%;
    height: 2px;
    background: var(--border-subtle);
    margin-top: 15px;
    align-self: start;
    transition: background 0.3s;
  }
  .line-done { background: var(--success); box-shadow: 0 0 4px rgba(85, 238, 160, 0.3); }
  .line-active { background: var(--accent); box-shadow: 0 0 6px var(--accent-glow); animation: pulse-neon 2s ease-in-out infinite; }
  .line-error { background: var(--error); }

  /* -- Accumulating panel -- */
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
  .acc-section:last-of-type { border-bottom: none; }

  .acc-section-header { display: flex; align-items: center; gap: 8px; }
  .acc-icon { color: var(--accent); flex-shrink: 0; }
  .acc-icon-ok { color: var(--success); flex-shrink: 0; }
  .acc-icon-bolt { color: var(--accent); }
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

  /* Tools list */
  .tools-list { display: flex; flex-direction: column; gap: 4px; padding: 0 0 0 24px; }
  .tool-row { display: flex; align-items: center; gap: 10px; padding: 4px 0; }
  .tool-icon { width: 18px; height: 18px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; color: var(--text-muted); }
  .tool-icon.tool-ready { color: var(--success); }
  .tool-icon.tool-installing { color: var(--accent); }
  .tool-pending-dot { width: 8px; height: 8px; border-radius: 50%; border: 1.5px solid var(--border); background: transparent; }
  .tool-name { font-size: 0.84rem; font-family: 'Consolas', 'Courier New', monospace; color: var(--text-primary); flex: 1; }
  .tool-status { font-size: 0.72rem; color: var(--text-muted); text-transform: lowercase; letter-spacing: 0.3px; }
  .tool-status-ready { color: var(--success); }
  .tool-status-installing { color: var(--accent); }

  /* Download progress */
  .dl-progress-row { display: flex; align-items: center; gap: 10px; margin-top: 8px; padding: 0 0 0 24px; }
  .dl-tool-name { font-size: 0.8rem; color: var(--text-secondary); min-width: 80px; }
  .dl-bar { flex: 1; height: 4px; background: var(--border-subtle); border-radius: 2px; overflow: hidden; }
  .dl-bar-fill { height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.3s; box-shadow: 0 0 6px var(--accent-glow-soft); }
  .dl-pct { font-size: 0.75rem; color: var(--accent); min-width: 36px; text-align: right; }
  .dl-extracting { font-size: 0.75rem; color: var(--text-muted); font-style: italic; }

  .acc-files-row { display: flex; align-items: center; gap: 12px; padding: 4px 0 4px 24px; flex-wrap: wrap; }
  .files-counter { display: flex; align-items: baseline; gap: 2px; }
  .files-num { font-size: 1.2rem; font-weight: 700; color: var(--accent); font-family: Consolas, ui-monospace, monospace; }
  .files-sep { font-size: 0.9rem; color: var(--text-muted); margin: 0 2px; }
  .files-total { font-size: 0.9rem; color: var(--text-muted); font-family: 'Orbitron', monospace; }
  .files-label { font-size: 0.75rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.3px; }
  .files-progress-track { flex: 1; min-width: 60px; height: 4px; background: rgba(255, 140, 40, 0.08); border-radius: 2px; overflow: hidden; }
  .files-progress-fill { height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.4s ease; }

  .acc-step-row { display: flex; flex-direction: column; gap: 8px; padding: 0 0 0 24px; }
  .step-info { display: flex; align-items: center; gap: 8px; }
  .step-badge {
    font-size: 0.75rem; padding: 2px 8px; border-radius: 4px;
    background: var(--accent-dim); color: var(--accent); font-weight: 600;
    font-family: 'Consolas', 'Courier New', monospace; flex-shrink: 0;
  }
  .step-name { font-size: 0.85rem; color: var(--text-primary); font-weight: 500; }

  .acc-log-mini {
    margin: 4px 0 0 24px; padding: 8px 12px;
    background: var(--bg-card-solid); border: 1px solid var(--border-subtle);
    border-radius: var(--radius-sm); max-height: 120px; overflow-y: auto;
  }
  .acc-log-text {
    font-size: 12px; font-family: 'Consolas', 'Courier New', monospace;
    color: #c0b0a0; line-height: 1.6; margin: 0; white-space: pre-wrap; word-break: break-all;
  }

  /* -- Status bar -- */
  .acc-status-bar { display: flex; flex-direction: column; gap: 10px; padding: 14px 20px; border-top: 1px solid var(--border-subtle); }
  .status-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 20px; }
  .status-msg { font-size: 0.78rem; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
  .elapsed { font-size: 0.78rem; color: var(--text-muted); font-family: 'Consolas', 'Courier New', monospace; flex-shrink: 0; }

  .spinner-sm {
    width: 14px; height: 14px; border: 2px solid var(--border); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.6s linear infinite; flex-shrink: 0;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* -- Error card -- */
  .error-card { padding: 14px 18px; width: 100%; border-color: rgba(255, 68, 102, 0.25) !important; }
  .error-text { font-size: 0.82rem; color: var(--error); font-family: 'Consolas', 'Courier New', monospace; word-break: break-word; }

  /* Shimmer on active progress bars */
  :global(.progress-fill) { position: relative; overflow: hidden; }
  :global(.progress-fill)::after {
    content: ''; position: absolute; top: 0; left: -100%; width: 100%; height: 100%;
    background: linear-gradient(90deg, transparent, rgba(255,255,255,0.2), transparent);
    animation: shimmer 1.5s ease-in-out infinite;
  }
  @keyframes shimmer { 0% { left: -100%; } 100% { left: 200%; } }

  .pipeline-controls { display: flex; gap: 8px; justify-content: flex-end; }
  .btn-sm { font-size: 0.78rem; padding: 6px 16px; border-radius: var(--radius-sm); font-weight: 600; cursor: pointer; border: 1px solid transparent; transition: all 0.15s; }
  .btn-accent { background: var(--accent-dim); color: var(--accent); border-color: var(--accent); }
  .btn-accent:hover { background: var(--accent); color: var(--bg-card-solid); }
  .btn-muted { background: var(--bg-card); color: var(--text-secondary); border-color: var(--border); }
  .btn-muted:hover { background: var(--bg-card-hover); color: var(--text-primary); }
  .btn-danger { background: var(--error-dim, rgba(255, 68, 102, 0.1)); color: var(--error); border-color: rgba(255, 68, 102, 0.3); }
  .btn-danger:hover { background: var(--error); color: #fff; }

  .paused-label { color: var(--accent) !important; font-weight: 600; }
  .cancelled-card { border-color: rgba(255, 180, 40, 0.25) !important; }
  .cancelled-text { font-size: 0.88rem; color: var(--text-secondary); font-weight: 500; }
</style>
