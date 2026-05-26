<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ProgressBar from '../components/ProgressBar.svelte';
  import LogViewer from '../components/LogViewer.svelte';
  import JobCard from '../components/JobCard.svelte';
  import { PipelineService } from '../lib/api';
  import { onEvent } from '../lib/events';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  let status = { running: false, phase: '', target: '' };
  let progress = 0;
  let stepLabel = '';
  let logEntries: Array<{ level: 'info' | 'warn' | 'error'; message: string; time?: string }> = [];
  let pastJobs: Array<{ target: string; status: 'completed' | 'failed'; duration: string; recipe: string }> = [];

  let elapsed = '';
  let startTime = 0;
  let timer: ReturnType<typeof setInterval> | null = null;
  let cleanups: Array<() => void> = [];

  onMount(async () => {
    try {
      status = await PipelineService.GetStatus();
      if (status.running) {
        startTimer();
      }
    } catch (e) {
      console.error('GetStatus failed:', e);
    }

    cleanups.push(
      onEvent('pipeline:progress', (data: any) => {
        if (data.Phase || data.phase) {
          status = {
            running: true,
            phase: data.Phase || data.phase,
            target: data.Target || data.target || '',
          };
        }
        if (data.Progress || data.progress) {
          const p = data.Progress || data.progress;
          const total = p.Total || p.total || 1;
          const step = p.Step || p.step || 0;
          progress = ((step + 1) / total) * 100;
          stepLabel = `${t(lang, 'jobs.step')} ${step + 1}/${total}: ${p.Name || p.name || ''}`;
        }
        if (data.Done || data.done) {
          status = { running: false, phase: '', target: '' };
          stopTimer();
          progress = 100;
        }
        if (data.Error || data.error) {
          const err = data.Error || data.error;
          const msg = typeof err === 'string' ? err : err.message || JSON.stringify(err);
          logEntries = [...logEntries, { level: 'error', message: msg }];
        }
      }),
      onEvent('pipeline:log', (data: any) => {
        const msg = typeof data === 'string' ? data : data.message || data.Message || '';
        if (msg) {
          logEntries = [...logEntries, { level: 'info', message: msg }];
        }
      }),
    );
  });

  onDestroy(() => {
    cleanups.forEach(fn => fn());
    stopTimer();
  });

  function startTimer() {
    startTime = Date.now();
    timer = setInterval(() => {
      const secs = Math.floor((Date.now() - startTime) / 1000);
      const m = Math.floor(secs / 60);
      const s = secs % 60;
      elapsed = `${m}:${s.toString().padStart(2, '0')}`;
    }, 1000);
  }

  function stopTimer() {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
  }

  async function cancelPipeline() {
    try {
      await PipelineService.Stop();
    } catch (e) {
      console.error('Stop failed:', e);
    }
  }
</script>

<div class="jobs-page">
  <h2 class="jobs-title">{t(lang, 'jobs.title')}</h2>

  {#if status.running}
    <div class="active-job">
      <div class="active-job-header">
        <span class="active-label">{t(lang, 'jobs.active')}</span>
        <span class="active-phase">{status.phase}</span>
        {#if status.target}
          <span class="active-target selectable">{status.target}</span>
        {/if}
        <button class="cancel-btn" on:click={cancelPipeline}>{t(lang, 'jobs.cancel')}</button>
      </div>

      <ProgressBar percent={progress} label={stepLabel} elapsed={elapsed} />

      <div class="active-log">
        <LogViewer {lang} entries={logEntries} />
      </div>
    </div>
  {:else}
    <div class="no-active">
      <span class="no-active-text">{t(lang, 'jobs.noActive')}</span>
    </div>
  {/if}

  {#if pastJobs.length > 0}
    <div class="past-jobs">
      <h3 class="past-title">{t(lang, 'jobs.history')}</h3>
      <div class="past-list">
        {#each pastJobs as job}
          <JobCard
            target={job.target}
            status={job.status}
            duration={job.duration}
            recipe={job.recipe}
          />
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .jobs-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 20px;
    gap: 20px;
  }
  .jobs-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
    flex-shrink: 0;
  }
  .active-job {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 16px;
    background: var(--bg-card);
    border: 1px solid var(--accent);
    border-radius: 8px;
    box-shadow: 0 0 12px var(--accent-dim);
  }
  .active-job-header {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .active-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--accent);
    background: var(--accent-dim);
    padding: 2px 8px;
    border-radius: 3px;
    font-weight: 600;
  }
  .active-phase {
    font-size: 12px;
    color: var(--text-secondary);
    text-transform: capitalize;
  }
  .active-target {
    font-size: 11px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }
  .cancel-btn {
    all: unset;
    font-size: 11px;
    padding: 4px 12px;
    border-radius: 4px;
    border: 1px solid var(--error);
    color: var(--error);
    cursor: pointer;
    transition: all 0.15s;
    flex-shrink: 0;
  }
  .cancel-btn:hover {
    background: rgba(255, 51, 102, 0.1);
  }
  .active-log {
    height: 200px;
    display: flex;
  }
  .no-active {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 32px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
  }
  .no-active-text {
    color: var(--text-muted);
    font-size: 13px;
  }
  .past-jobs {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
    overflow: hidden;
  }
  .past-title {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-secondary);
    margin: 0;
  }
  .past-list {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
</style>
