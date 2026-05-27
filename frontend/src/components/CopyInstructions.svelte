<script lang="ts">
  import { Clipboard } from '@wailsio/runtime';
  import { t, type Lang } from '../lib/i18n';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  const API_BASE = 'http://127.0.0.1:19876';

  let copied = $state(false);

  async function copyToClipboard(text: string): Promise<void> {
    // 1. Wails runtime clipboard (works when Wails backend is available)
    try {
      await Clipboard.SetText(text);
      return;
    } catch { /* fall through */ }

    // 2. Browser Clipboard API (works on localhost / secure contexts)
    if (navigator.clipboard?.writeText) {
      try {
        await navigator.clipboard.writeText(text);
        return;
      } catch { /* fall through */ }
    }

    // 3. Legacy fallback
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    document.body.appendChild(ta);
    ta.select();
    document.execCommand('copy');
    document.body.removeChild(ta);
  }

  async function copyInstructions() {
    let text: string;
    try {
      const res = await fetch(`${API_BASE}/api/instructions`);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      text = await res.text();
    } catch (e) {
      console.error('Failed to fetch instructions:', e);
      return;
    }

    await copyToClipboard(text);
    copied = true;
    setTimeout(() => (copied = false), 2000);
  }
</script>

<button class="copy-btn" onclick={copyInstructions}>
  {copied ? t(lang, 'settings.copied') : t(lang, 'settings.copyButton')}
</button>

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
  .copy-btn:hover {
    background: var(--accent-dim);
  }
</style>
