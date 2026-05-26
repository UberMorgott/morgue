<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let phase: string = '';
  export let target: string = '';
  export let elapsed: string = '';
  export let progress: number = 0;
  export let progressLabel: string = '';
</script>

<footer class="status-bar">
  <div class="status-left">
    {#if phase}
      <span class="status-phase">{phase}</span>
      {#if progressLabel}
        <span class="status-label">{progressLabel}</span>
      {/if}
      {#if target}
        <span class="status-target">{target}</span>
      {/if}
    {:else}
      <span class="status-idle">{t(lang, 'status.ready')}</span>
    {/if}
  </div>

  {#if progress > 0 && progress < 100}
    <div class="status-progress">
      <div class="status-progress-fill" style="width: {progress}%"></div>
    </div>
  {/if}

  <div class="status-right">
    {#if progress > 0 && progress < 100}
      <span class="status-percent">{Math.round(progress)}%</span>
    {/if}
    {#if elapsed}
      <span class="status-elapsed">{elapsed}</span>
    {/if}
  </div>
</footer>

<style>
  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 52px;
    padding: 0 24px;
    background: var(--bg-sidebar);
    border-top: 1px solid var(--border);
    font-size: 22px;
    font-family: ui-monospace, monospace;
    flex-shrink: 0;
    gap: 8px;
  }
  .status-left {
    display: flex;
    align-items: center;
    gap: 8px;
    overflow: hidden;
    flex-shrink: 0;
  }
  .status-phase {
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .status-label {
    color: var(--text-secondary);
  }
  .status-target {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 300px;
  }
  .status-idle {
    color: var(--text-muted);
  }
  .status-idle::before {
    content: '';
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--success);
    box-shadow: 0 0 8px rgba(85, 238, 160, 0.5);
    margin-right: 8px;
    vertical-align: middle;
  }
  .status-progress {
    flex: 1;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    overflow: hidden;
    min-width: 60px;
  }
  .status-progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    transition: width 0.3s ease;
  }
  .status-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .status-percent {
    color: var(--accent);
    font-weight: 600;
  }
  .status-elapsed {
    color: var(--text-muted);
  }
</style>
