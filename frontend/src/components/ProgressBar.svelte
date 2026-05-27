<script lang="ts">
  let { percent = 0, label = '', elapsed = '' }: {
    percent?: number;
    label?: string;
    elapsed?: string;
  } = $props();
</script>

<div class="progress-wrapper">
  <div class="progress-info">
    {#if label}
      <span class="progress-label">{label}</span>
    {/if}
    <span class="progress-pct">{Math.round(percent)}%</span>
    {#if elapsed}
      <span class="progress-elapsed">{elapsed}</span>
    {/if}
  </div>
  <div class="progress-track">
    <div class="progress-fill" style="width: {Math.min(100, Math.max(0, percent))}%;"></div>
  </div>
</div>

<style>
  .progress-wrapper {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .progress-info {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 11px;
    font-family: ui-monospace, monospace;
  }
  .progress-label {
    color: var(--text-secondary);
  }
  .progress-pct {
    color: var(--accent);
    margin-left: auto;
  }
  .progress-elapsed {
    color: var(--text-muted);
    margin-left: 8px;
  }
  .progress-track {
    width: 100%;
    height: 6px;
    border-radius: 9999px;
    background: var(--bg-input);
    overflow: hidden;
    border: 1px solid var(--border-subtle);
  }
  .progress-fill {
    height: 100%;
    border-radius: 9999px;
    transition: width 0.2s;
    background: linear-gradient(90deg, var(--accent), var(--accent-purple));
    box-shadow: 0 0 8px var(--accent-dim);
  }
</style>
