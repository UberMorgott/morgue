<script lang="ts">
  import { Clipboard } from '@wailsio/runtime';

  let copied = false;

  async function copyInstructions() {
    let text: string;
    try {
      const res = await fetch('http://127.0.0.1:19876/api/instructions');
      text = await res.text();
    } catch (e) {
      console.error('Failed to fetch instructions:', e);
      return;
    }

    try {
      await Clipboard.SetText(text);
    } catch {
      // Fallback: textarea + execCommand for restricted contexts
      const ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }

    copied = true;
    setTimeout(() => (copied = false), 2000);
  }
</script>

<button class="copy-btn" on:click={copyInstructions}>
  {copied ? 'Copied!' : 'Copy AI Instructions'}
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
