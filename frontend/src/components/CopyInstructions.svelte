<script lang="ts">
  import { Clipboard } from '@wailsio/runtime';
  import { InstructionsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  const API_BASE = 'http://127.0.0.1:19876';

  type Status = 'idle' | 'loading' | 'copied' | 'fallback' | 'error';
  let status = $state<Status>('idle');
  let fallbackText = $state('');
  let resetTimer: ReturnType<typeof setTimeout> | undefined;

  async function copyToClipboard(text: string): Promise<boolean> {
    // 1. Wails runtime clipboard
    try {
      await Clipboard.SetText(text);
      // Verify it actually wrote by reading back
      const readback = await Clipboard.Text();
      if (readback === text) return true;
    } catch { /* fall through */ }

    // 2. Browser Clipboard API (secure contexts only)
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        return true;
      } catch { /* fall through */ }
    }

    // 3. Legacy execCommand fallback
    try {
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.focus();
      ta.select();
      const ok = document.execCommand('copy');
      document.body.removeChild(ta);
      if (ok) return true;
    } catch { /* fall through */ }

    return false;
  }

  async function fetchInstructions(): Promise<string> {
    // 1. Native Wails binding (no HTTP dependency)
    try {
      const text = await InstructionsService.Get();
      if (text) return text;
    } catch (e) {
      console.warn('Instructions binding unavailable, falling back to HTTP:', e);
    }

    // 2. HTTP fallback (also serves CLI/external clients)
    const res = await fetch(`${API_BASE}/api/instructions`);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.text();
  }

  async function copyInstructions() {
    clearTimeout(resetTimer);
    status = 'loading';
    let text: string;
    try {
      text = await fetchInstructions();
    } catch (e) {
      console.error('Failed to retrieve instructions:', e);
      status = 'error';
      resetTimer = setTimeout(() => (status = 'idle'), 4000);
      return;
    }

    const ok = await copyToClipboard(text);
    if (ok) {
      status = 'copied';
      resetTimer = setTimeout(() => (status = 'idle'), 2000);
    } else {
      fallbackText = text;
      status = 'fallback';
    }
  }

  function closeFallback() {
    status = 'idle';
    fallbackText = '';
  }
</script>

<button
  class="copy-btn"
  class:copy-btn-error={status === 'error'}
  onclick={copyInstructions}
  disabled={status === 'loading'}
>
  {#if status === 'copied'}
    {t(lang, 'settings.copied')}
  {:else if status === 'loading'}
    ...
  {:else if status === 'error'}
    {t(lang, 'settings.copyError')}
  {:else}
    {t(lang, 'settings.copyButton')}
  {/if}
</button>

{#if status === 'fallback'}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="fallback-overlay" onclick={closeFallback}>
    <div class="fallback-panel glass" onclick={(e) => e.stopPropagation()}>
      <p class="fallback-hint">{t(lang, 'settings.copyFailed')}</p>
      <textarea
        class="fallback-textarea log-area font-mono selectable"
        readonly
        value={fallbackText}
        onfocus={(e) => (e.target as HTMLTextAreaElement).select()}
      ></textarea>
      <button class="page-action-btn" onclick={closeFallback}>
        {t(lang, 'settings.close')}
      </button>
    </div>
  </div>
{/if}

<style>
  .copy-btn {
    all: unset;
    font-size: clamp(10px, 0.8vw, 12px);
    padding: 6px 14px;
    border-radius: 6px;
    border: 1px solid var(--accent);
    color: var(--accent);
    cursor: pointer;
    white-space: nowrap;
    transition: all 0.15s;
  }
  .copy-btn:hover:not(:disabled) {
    background: var(--accent-dim);
  }
  .copy-btn:disabled {
    opacity: 0.6;
    cursor: wait;
  }
  .copy-btn-error {
    border-color: var(--error);
    color: var(--error);
  }

  .fallback-overlay {
    position: fixed;
    inset: 0;
    z-index: 9999;
    background: var(--bg-overlay);
    display: flex;
    align-items: center;
    justify-content: center;
    animation: fade-in 0.2s ease;
  }

  .fallback-panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 20px;
    width: min(90vw, 600px);
    max-height: 70vh;
  }

  .fallback-hint {
    margin: 0;
    font-size: 0.85rem;
    color: var(--warning);
  }

  .fallback-textarea {
    flex: 1;
    min-height: 200px;
    max-height: 50vh;
    resize: none;
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
