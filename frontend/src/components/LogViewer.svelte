<script lang="ts">
  import { afterUpdate } from 'svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let entries: Array<{ level: 'info' | 'warn' | 'error'; message: string; time?: string }> = [];
  export let autoScroll: boolean = true;

  let container: HTMLDivElement;

  afterUpdate(() => {
    if (autoScroll && container) {
      container.scrollTop = container.scrollHeight;
    }
  });
</script>

<div class="log-viewer" bind:this={container}>
  {#each entries as entry}
    <div class="log-entry log-{entry.level}">
      {#if entry.time}
        <span class="log-time">{entry.time}</span>
      {/if}
      <span class="log-level">[{entry.level.toUpperCase()}]</span>
      <span class="log-msg selectable">{entry.message}</span>
    </div>
  {/each}
  {#if entries.length === 0}
    <div class="log-empty">{t(lang, 'log.empty')}</div>
  {/if}
</div>

<style>
  .log-viewer {
    flex: 1;
    overflow-y: auto;
    padding: 8px;
    background: var(--bg-input);
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    font-family: ui-monospace, "Cascadia Code", "Fira Code", monospace;
    font-size: 11px;
    line-height: 1.6;
  }
  .log-entry {
    display: flex;
    gap: 6px;
    padding: 1px 0;
  }
  .log-time {
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .log-level {
    flex-shrink: 0;
    width: 48px;
  }
  .log-info .log-level { color: var(--text-secondary); }
  .log-warn .log-level { color: var(--warning); }
  .log-error .log-level { color: var(--error); }
  .log-msg {
    color: var(--text-primary);
    word-break: break-all;
  }
  .log-error .log-msg { color: var(--error); }
  .log-warn .log-msg { color: var(--warning); }
  .log-empty {
    color: var(--text-muted);
    text-align: center;
    padding: 24px;
    font-style: italic;
  }
</style>
