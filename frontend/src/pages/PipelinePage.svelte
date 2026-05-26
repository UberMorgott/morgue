<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { ReconService, PipelineService, ToolsService } from '../lib/api';
  import { onEvent } from '../lib/events';
  import ProgressBar from '../components/ProgressBar.svelte';
  import LogViewer from '../components/LogViewer.svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let inputPath: string = '';

  type PipelineStep = 'scan' | 'recon' | 'tools' | 'execute' | 'done' | 'error';

  let currentStep: PipelineStep = 'scan';
  let scanResults: Array<{ path: string; group: string }> = [];
  let reconResults: Array<{ path: string; group: string; kind: string; obfuscator: string; recipe: string }> = [];
  let toolsNeeded: Array<{ name: string; installed: boolean; category: string }> = [];
  let missingTools: string[] = [];
  let installingMissing = false;

  let pipelineProgress = 0;
  let pipelineStepLabel = '';
  let pipelineTotal = 0;
  let pipelineCurrent = 0;
  let logEntries: Array<{ level: 'info' | 'warn' | 'error'; message: string }> = [];

  let outputPath = '';
  let totalTime = '';
  let filesCount = 0;
  let errorMessage = '';

  let cleanups: Array<() => void> = [];

  export let statusPhase = '';
  export let statusProgress = 0;
  export let statusLabel = '';

  onMount(async () => {
    cleanups.push(
      onEvent('pipeline:progress', (data: any) => {
        const d = data?.data?.[0] || data?.data || data;
        if (d.Progress || d.progress) {
          const p = d.Progress || d.progress;
          pipelineTotal = p.Total || p.total || 1;
          pipelineCurrent = p.Step || p.step || 0;
          pipelineProgress = ((pipelineCurrent + 1) / pipelineTotal) * 100;
          pipelineStepLabel = `${t(lang, 'pipeline.step')} ${pipelineCurrent + 1}/${pipelineTotal}: ${p.Name || p.name || ''}`;
          statusPhase = t(lang, 'pipeline.executing');
          statusProgress = pipelineProgress;
          statusLabel = pipelineStepLabel;
        }
        if (d.Done || d.done) {
          currentStep = 'done';
          pipelineProgress = 100;
          outputPath = d.Output || d.output || inputPath;
          filesCount = d.Files || d.files || 0;
          totalTime = d.Duration || d.duration || '';
          statusPhase = '';
          statusProgress = 0;
          statusLabel = '';
        }
        if (d.Error || d.error) {
          const err = d.Error || d.error;
          errorMessage = typeof err === 'string' ? err : err.message || JSON.stringify(err);
          logEntries = [...logEntries, { level: 'error', message: errorMessage }];
        }
      }),
      onEvent('pipeline:log', (data: any) => {
        const msg = typeof data === 'string' ? data : data?.data?.[0] || data?.message || '';
        if (msg) logEntries = [...logEntries, { level: 'info', message: msg }];
      }),
    );

    await runPipeline();
  });

  onDestroy(() => {
    cleanups.forEach(fn => fn());
  });

  async function runPipeline() {
    currentStep = 'scan';
    statusPhase = t(lang, 'pipeline.scanning');
    statusProgress = 0;
    try {
      const targets = await ReconService.ScanDirectory(inputPath);
      scanResults = (targets || []).map((t: any) => ({
        path: t.path || t.Path,
        group: t.group || t.Group || '',
      }));
    } catch (e: any) {
      errorMessage = e.message || String(e);
      currentStep = 'error';
      statusPhase = '';
      return;
    }

    if (scanResults.length === 0) {
      errorMessage = 'No binaries found';
      currentStep = 'error';
      statusPhase = '';
      return;
    }

    currentStep = 'recon';
    statusPhase = t(lang, 'pipeline.recon');
    reconResults = [];
    for (const target of scanResults) {
      try {
        const result = await ReconService.ClassifyFile(target.path);
        reconResults = [...reconResults, {
          path: target.path,
          group: target.group,
          kind: result?.Kind || result?.kind || '',
          obfuscator: result?.Obfuscator || result?.obfuscator || '',
          recipe: result?.Recipe || result?.recipe || '',
        }];
      } catch (e) {
        reconResults = [...reconResults, {
          path: target.path,
          group: target.group,
          kind: '?', obfuscator: '', recipe: '',
        }];
      }
    }

    currentStep = 'tools';
    statusPhase = t(lang, 'pipeline.checkingTools');
    try {
      const statuses = await ToolsService.CheckAll();
      toolsNeeded = (statuses || []).map((s: any) => ({
        name: s.Name || s.name,
        installed: s.Installed || s.installed || false,
        category: s.Category || s.category || '',
      }));
      missingTools = toolsNeeded.filter(t => !t.installed).map(t => t.name);
    } catch (e) {
      missingTools = [];
    }

    if (missingTools.length === 0) {
      await startExecution();
    }
  }

  async function installMissingAndContinue() {
    installingMissing = true;
    statusPhase = t(lang, 'status.installing');
    try {
      await ToolsService.InstallAll();
      missingTools = [];
      installingMissing = false;
      await startExecution();
    } catch (e: any) {
      errorMessage = e.message || String(e);
      installingMissing = false;
    }
  }

  async function startExecution() {
    currentStep = 'execute';
    statusPhase = t(lang, 'pipeline.executing');
    logEntries = [];
    try {
      await PipelineService.Run(inputPath, '');
    } catch (e: any) {
      if (!errorMessage) {
        errorMessage = e.message || String(e);
        currentStep = 'error';
        statusPhase = '';
      }
    }
  }
</script>

<div class="pipeline-page">
  <!-- Step 1: Scan -->
  <section class="pipeline-step" class:collapsed={currentStep !== 'scan' && scanResults.length > 0}>
    <h3 class="step-title">
      <span class="step-num">1</span>
      {t(lang, 'scan.title')}
      {#if scanResults.length > 0}
        <span class="step-badge">{scanResults.length} {t(lang, 'pipeline.foundBinaries')}</span>
      {/if}
    </h3>
    {#if currentStep === 'scan'}
      <div class="step-loading"><span class="spinner"></span> {t(lang, 'pipeline.scanning')}</div>
    {/if}
  </section>

  <!-- Step 2: Recon -->
  {#if currentStep !== 'scan' || scanResults.length > 0}
    <section class="pipeline-step" class:collapsed={currentStep !== 'recon' && reconResults.length > 0}>
      <h3 class="step-title">
        <span class="step-num">2</span>
        {t(lang, 'pipeline.recon')}
      </h3>
      {#if currentStep === 'recon'}
        <div class="step-content">
          {#each reconResults as r}
            <div class="recon-row">
              <span class="recon-path">{r.path.split(/[/\\]/).pop()}</span>
              <span class="recon-tag">{r.kind}</span>
              {#if r.obfuscator}<span class="recon-tag tag-obf">{r.obfuscator}</span>{/if}
            </div>
          {/each}
          {#if reconResults.length < scanResults.length}
            <div class="step-loading"><span class="spinner"></span> {t(lang, 'pipeline.recon')}</div>
          {/if}
        </div>
      {:else if reconResults.length > 0}
        <div class="step-summary">
          {#each reconResults as r}
            <span class="recon-tag-sm">{r.kind}{r.obfuscator ? ` / ${r.obfuscator}` : ''}</span>
          {/each}
        </div>
      {/if}
    </section>
  {/if}

  <!-- Step 3: Tools -->
  {#if currentStep === 'tools' || currentStep === 'execute' || currentStep === 'done'}
    <section class="pipeline-step" class:collapsed={currentStep !== 'tools'}>
      <h3 class="step-title">
        <span class="step-num">3</span>
        {t(lang, 'pipeline.checkingTools')}
        {#if missingTools.length === 0 && currentStep !== 'tools'}
          <span class="step-badge ok">✓ {t(lang, 'pipeline.allToolsReady')}</span>
        {:else if missingTools.length > 0}
          <span class="step-badge warn">⚠ {missingTools.length} {t(lang, 'pipeline.missingTools')}</span>
        {/if}
      </h3>
      {#if currentStep === 'tools' && missingTools.length > 0}
        <div class="step-content">
          <div class="tools-list-mini">
            {#each toolsNeeded as tool}
              <div class="tool-mini" class:tool-ok={tool.installed} class:tool-missing={!tool.installed}>
                <span>{tool.installed ? '✅' : '❌'}</span>
                <span>{tool.name}</span>
              </div>
            {/each}
          </div>
          <button class="install-btn" on:click={installMissingAndContinue} disabled={installingMissing}>
            {installingMissing ? t(lang, 'tools.installing') : t(lang, 'pipeline.installMissing')}
          </button>
        </div>
      {/if}
    </section>
  {/if}

  <!-- Step 4: Execute -->
  {#if currentStep === 'execute' || currentStep === 'done'}
    <section class="pipeline-step" class:collapsed={currentStep === 'done'}>
      <h3 class="step-title">
        <span class="step-num">4</span>
        {t(lang, 'pipeline.executing')}
      </h3>
      {#if currentStep === 'execute'}
        <div class="step-content">
          <ProgressBar percent={pipelineProgress} label={pipelineStepLabel} />
          <div class="pipeline-log">
            <LogViewer {lang} entries={logEntries} />
          </div>
        </div>
      {/if}
    </section>
  {/if}

  <!-- Step 5: Done -->
  {#if currentStep === 'done'}
    <section class="pipeline-step done-step">
      <h3 class="step-title">
        <span class="step-num">✓</span>
        {t(lang, 'pipeline.done')}
      </h3>
      <div class="step-content done-content">
        <div class="done-stat"><strong>{t(lang, 'pipeline.outputPath')}:</strong> <span class="selectable">{outputPath}</span></div>
        {#if filesCount > 0}
          <div class="done-stat">{filesCount} {t(lang, 'pipeline.filesDecompiled')}</div>
        {/if}
        {#if totalTime}
          <div class="done-stat">{t(lang, 'pipeline.totalTime')}: {totalTime}</div>
        {/if}
      </div>
    </section>
  {/if}

  <!-- Error -->
  {#if currentStep === 'error'}
    <section class="pipeline-step error-step">
      <h3 class="step-title error-title">
        <span class="step-num">!</span>
        {t(lang, 'pipeline.error')}
      </h3>
      <div class="step-content">
        <div class="error-msg">{errorMessage}</div>
      </div>
    </section>
  {/if}
</div>

<style>
  .pipeline-page {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    padding: 20px;
    overflow-y: auto;
  }
  .pipeline-step {
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    padding: 14px 16px;
    transition: all 0.3s ease;
  }
  .pipeline-step.collapsed {
    padding: 8px 16px;
  }
  .pipeline-step.collapsed .step-content { display: none; }
  .step-title {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }
  .step-num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--accent-dim);
    color: var(--accent);
    font-size: 11px;
    font-weight: 700;
    flex-shrink: 0;
  }
  .step-badge {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    margin-left: auto;
  }
  .step-badge.ok { color: var(--success, #22c55e); }
  .step-badge.warn { color: var(--warning, #eab308); }
  .step-content { margin-top: 12px; }
  .step-loading {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    font-size: 12px;
    padding: 8px 0;
  }
  .spinner {
    width: 14px; height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .step-summary {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 6px;
  }
  .recon-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 0;
    font-size: 12px;
  }
  .recon-path {
    color: var(--text-secondary);
    font-family: ui-monospace, monospace;
  }
  .recon-tag {
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--accent-dim);
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .recon-tag-sm {
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    background: var(--accent-dim);
    color: var(--accent);
  }
  .tag-obf { background: rgba(191, 95, 255, 0.15); color: #bf5fff; }
  .tools-list-mini {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 12px;
  }
  .tool-mini {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    padding: 4px 8px;
    border-radius: 4px;
  }
  .tool-ok { color: var(--text-secondary); }
  .tool-missing { color: var(--error); background: rgba(255, 51, 102, 0.08); }
  .install-btn {
    all: unset;
    font-size: 12px;
    padding: 8px 16px;
    border-radius: 6px;
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    cursor: pointer;
  }
  .install-btn:hover { box-shadow: 0 0 12px var(--accent-dim); }
  .install-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .pipeline-log { height: 180px; margin-top: 8px; }
  .done-step { border-color: var(--success, #22c55e); }
  .done-content {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .error-step { border-color: var(--error); }
  .error-title { color: var(--error); }
  .error-msg {
    font-size: 12px;
    color: var(--error);
    font-family: ui-monospace, monospace;
    padding: 8px;
    background: rgba(255, 51, 102, 0.08);
    border-radius: 4px;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
