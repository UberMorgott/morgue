<script lang="ts">
  let { label, hint = '', active, onToggle }: { label: string; hint?: string; active: boolean; onToggle: () => void } = $props();

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onToggle();
    }
  }
</script>

<div class="setting-row">
  {#if hint}
    <div class="setting-label-with-hint">
      <span class="setting-label">{label}</span>
      <span class="setting-hint-icon" title={hint}>?</span>
    </div>
  {:else}
    <span class="setting-label">{label}</span>
  {/if}
  <div class="toggle" class:active={active} onclick={onToggle} onkeydown={handleKeydown} role="switch" tabindex="0" aria-checked={active}></div>
</div>

<style>
  .setting-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 32px;
  }

  .setting-label {
    font-size: clamp(11px, 0.9vw, 14px);
    color: var(--text-secondary);
    flex-shrink: 1;
    min-width: 0;
  }

  .setting-label-with-hint {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 1;
    min-width: 0;
  }

  .setting-hint-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: 10px;
    cursor: help;
    flex-shrink: 0;
    transition: color 0.15s, border-color 0.15s;
  }
  .setting-hint-icon:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
</style>
