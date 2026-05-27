<script lang="ts">
  let copied = false;

  async function copyInstructions() {
    try {
      const res = await fetch('http://127.0.0.1:19876/api/instructions');
      const text = await res.text();
      await navigator.clipboard.writeText(text);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch (e) {
      console.error('Copy instructions failed:', e);
    }
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
