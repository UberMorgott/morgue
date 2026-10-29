<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import type { HistoryEntry } from '../lib/pipeline';
  import { basename, statusKey, statusTone } from '../lib/history';

  let { lang = 'en' as Lang, entries = [] as HistoryEntry[], onselect }: {
    lang?: Lang;
    entries?: HistoryEntry[];
    onselect?: (detail: { path: string }) => void;
  } = $props();

  const dotClass: Record<string, string> = {
    success: 'dot-success',
    error: 'dot-error',
    warning: 'dot-warning',
    running: 'dot-active',
    muted: '',
  };

  function relativeTime(iso: string | null): string {
    if (!iso) return '';
    const ts = Date.parse(iso);
    if (Number.isNaN(ts)) return '';
    const diff = Math.max(0, Math.floor((Date.now() - ts) / 1000));
    if (diff < 60) return `<1${t(lang, 'home.ago.minutes')}`;
    if (diff < 3600) return `${Math.floor(diff / 60)}${t(lang, 'home.ago.minutes')}`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}${t(lang, 'home.ago.hours')}`;
    return `${Math.floor(diff / 86400)}${t(lang, 'home.ago.days')}`;
  }
</script>

<div class="history-section animate-in">
  <h3 class="section-label history-heading">{t(lang, 'home.recent')}</h3>
  <div class="history-list glass">
    {#each entries.slice(0, 5) as entry (entry.path)}
      {@const key = statusKey(entry)}
      {@const tone = statusTone(entry)}
      <button
        class="history-item"
        title={entry.path}
        disabled={!entry.input}
        onclick={() => entry.input && onselect?.({ path: entry.input })}
      >
        <span class="dot {dotClass[tone]}"></span>
        <span class="history-name font-mono text-sm">{basename(entry.input || entry.path)}</span>
        {#if key}
          <span class="tag {tone === 'error' ? 'tag-error' : tone === 'muted' ? 'tag-muted' : 'tag-accent'}">{t(lang, key)}</span>
        {/if}
        {#if entry.targets > 0}
          <span class="history-time text-xs">{entry.targets} {t(lang, 'history.targets')}</span>
        {/if}
        <span class="history-time text-xs">{relativeTime(entry.startedAt)}</span>
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
    margin: 0 0 8px 4px;
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
  .history-item:hover:not(:disabled) {
    background: var(--bg-card-hover);
  }
  .history-item:disabled {
    cursor: default;
    opacity: 0.6;
  }
  .history-name {
    color: var(--text-primary);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .history-time {
    color: var(--text-muted);
    flex-shrink: 0;
  }
</style>
