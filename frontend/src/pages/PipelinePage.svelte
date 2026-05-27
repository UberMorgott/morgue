<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { PipelineService } from '../lib/api';
  import { onEvent } from '../lib/events';

  import ProgressBar from '../components/ProgressBar.svelte';
  import LogViewer from '../components/LogViewer.svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let inputPath: string = '';

  type PipelineStep = 'scan' | 'recon' | 'tools' | 'execute' | 'done' | 'error';

  let currentStep: PipelineStep = 'scan';

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
        }
        if (d.Done || d.done) {
          currentStep = 'done';
          pipelineProgress = 100;
          outputPath = d.Output || d.output || inputPath;
          filesCount = d.Files || d.files || 0;
          totalTime = d.Duration || d.duration || '';
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
    currentStep = 'execute';
    logEntries = [];
    try {
      // Engine handles scan→recon→match→install→execute with proper events
      await PipelineService.Run(inputPath, '');
    } catch (e: any) {
      if (!errorMessage) {
        errorMessage = e.message || String(e);
        currentStep = 'error';
      }
    }
  }
</script>

<div class="pipeline-page">
  <!-- Execute -->
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
