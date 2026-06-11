<script lang="ts">
  import { t, type Lang } from '../lib/i18n';
  import { updateProgress } from '../lib/stores';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  const up = updateProgress;

  // Human-readable byte counts, e.g. "3.2 / 8.0 MB".
  function fmtBytes(n: number): string {
    if (n <= 0) return '0';
    const mb = n / (1024 * 1024);
    if (mb >= 1) return mb.toFixed(1) + ' MB';
    return (n / 1024).toFixed(0) + ' KB';
  }

  // Phase → status line. "done" means the binary was replaced; we then auto-relaunch.
  let phaseLabel = $derived.by(() => {
    switch ($up.phase) {
      case 'downloading': return t(lang, 'update.downloading');
      case 'installing':  return t(lang, 'update.installing');
      case 'done':        return t(lang, 'update.restarting');
      case 'error':       return t(lang, 'update.failed');
      default:            return t(lang, 'update.downloading');
    }
  });

  // Download phase shows a determinate bar; installing/done show a full bar.
  let barPercent = $derived(
    $up.phase === 'installing' || $up.phase === 'done' ? 100 : $up.percent
  );
  let isError = $derived($up.phase === 'error');
</script>

{#if $up.active}
  <div class="update-backdrop">
    <div class="glass neon-border update-card animate-in">
      <div class="update-head">
        <span class="panel-title">{t(lang, 'update.title')}</span>
        {#if $up.version}<span class="update-version font-accent">v{$up.version}</span>{/if}
      </div>

      <div class="update-status" class:is-error={isError}>{phaseLabel}</div>

      {#if isError}
        <div class="alert-block alert-error font-mono update-error">{$up.error}</div>
      {:else}
        <div class="bar-track">
          <div
            class="bar-fill"
            class:indeterminate={$up.phase === 'installing' || $up.phase === 'done'}
            style="width: {barPercent}%;"
          ></div>
        </div>
        <div class="bar-meta font-mono text-xs">
          {#if $up.phase === 'downloading' && $up.total > 0}
            <span>{fmtBytes($up.downloaded)} / {fmtBytes($up.total)}</span>
            <span>{$up.percent}%</span>
          {:else if $up.phase === 'downloading'}
            <span>{fmtBytes($up.downloaded)}</span>
          {:else}
            <span></span>
            <span>{barPercent}%</span>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .update-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(8, 6, 12, 0.72);
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
  }
  .update-card {
    width: min(420px, 86vw);
    padding: 26px 28px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .update-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
  }
  .update-version {
    color: var(--accent);
    font-size: 14px;
  }
  .update-status {
    color: var(--text-primary);
    font-size: 14px;
  }
  .update-status.is-error {
    color: var(--error);
  }
  .update-error {
    max-height: 120px;
    overflow: auto;
    word-break: break-word;
  }
  .bar-track {
    width: 100%;
    height: 10px;
    border-radius: 999px;
    background: rgba(255, 255, 255, 0.07);
    overflow: hidden;
  }
  .bar-fill {
    height: 100%;
    border-radius: 999px;
    background: linear-gradient(90deg, var(--accent-warm, #ffb060), var(--accent) 60%, var(--accent-hot, #ff6a00));
    box-shadow: 0 0 10px var(--accent-glow-soft, rgba(255, 138, 32, 0.4));
    transition: width 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  }
  /* Installing/done: no concrete byte progress — animate a sheen on a full bar. */
  .bar-fill.indeterminate {
    background-size: 200% 100%;
    animation: bar-sheen 1.1s linear infinite;
  }
  @keyframes bar-sheen {
    0%   { background-position: 200% 0; }
    100% { background-position: -200% 0; }
  }
  .bar-meta {
    display: flex;
    justify-content: space-between;
    color: var(--text-muted);
  }
</style>
