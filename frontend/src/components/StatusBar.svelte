<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let phase: string = '';
  export let target: string = '';
  export let elapsed: string = '';
</script>

<footer class="status-bar">
  <div class="status-left">
    {#if phase}
      <span class="status-phase">{phase}</span>
      {#if target}
        <span class="status-target">{target}</span>
      {/if}
    {:else}
      <span class="status-idle">{t(lang, 'status.ready')}</span>
    {/if}
  </div>
  <div class="status-right">
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
    height: 24px;
    padding: 0 12px;
    background: var(--bg-sidebar);
    border-top: 1px solid var(--border);
    font-size: 11px;
    font-family: ui-monospace, monospace;
    flex-shrink: 0;
  }
  .status-left {
    display: flex;
    align-items: center;
    gap: 8px;
    overflow: hidden;
  }
  .status-phase {
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .status-target {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 400px;
  }
  .status-idle {
    color: var(--text-muted);
  }
  .status-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .status-elapsed {
    color: var(--text-muted);
  }
</style>
