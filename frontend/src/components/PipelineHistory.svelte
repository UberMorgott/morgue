<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import type { HistoryEntry } from '../lib/pipeline';

  let { lang = 'en' as Lang, entries = [] as HistoryEntry[], onselect }: {
    lang?: Lang;
    entries?: HistoryEntry[];
    onselect?: (detail: { path: string }) => void;
  } = $props();

  function relativeTime(ts: number): string {
    const diff = Math.floor((Date.now() - ts) / 1000);
    if (diff < 60) return `<1${t(lang, 'home.ago.minutes')}`;
    if (diff < 3600) return `${Math.floor(diff / 60)}${t(lang, 'home.ago.minutes')}`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}${t(lang, 'home.ago.hours')}`;
    return `${Math.floor(diff / 86400)}${t(lang, 'home.ago.days')}`;
  }

  function basename(p: string): string {
    return p.split(/[\\/]/).pop() || p;
  }
</script>

<div class="history-section animate-in">
  <h3 class="history-heading">{t(lang, 'home.recent')}</h3>
  <div class="history-list glass">
    {#each entries.slice(0, 5) as entry (entry.timestamp)}
      <button class="history-item" onclick={() => onselect?.({ path: entry.path })}>
        <span class="dot {entry.success ? 'dot-success' : 'dot-error'}"></span>
        <span class="history-name">{basename(entry.path)}</span>
        {#if entry.kind}
          <span class="tag tag-accent">{entry.kind}</span>
        {/if}
        <span class="history-time">{relativeTime(entry.timestamp)}</span>
      </button>
    {/each}
  </div>
</div>

<style>
  .history-section {
    width: 100%;
    max-width: 480px;
  }
  .history-heading {
    font-size: 0.85rem;
    color: var(--text-muted);
    margin: 0 0 8px 4px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .history-list {
    display: flex;
    flex-direction: column;
    padding: 4px;
    gap: 2px;
  }
  .history-item {
    all: unset;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition: background 0.15s;
  }
  .history-item:hover {
    background: var(--bg-card-hover);
  }
  .history-name {
    font-size: 0.88rem;
    color: var(--text-primary);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: 'Consolas', 'Courier New', monospace;
  }
  .history-time {
    font-size: 0.75rem;
    color: var(--text-muted);
    flex-shrink: 0;
  }
</style>
