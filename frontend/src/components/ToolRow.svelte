<script lang="ts">
  import { createEventDispatcher } from 'svelte';

  export let name: string;
  export let installed: boolean = false;
  export let path: string = '';
  export let installing: boolean = false;

  const dispatch = createEventDispatcher();

  function handleInstall() {
    dispatch('install', { name });
  }
</script>

<div class="tool-row" class:installed>
  <div class="tool-status">
    {#if installed}
      <span class="status-dot installed-dot"></span>
    {:else}
      <span class="status-dot missing-dot"></span>
    {/if}
  </div>

  <div class="tool-info">
    <span class="tool-name">{name}</span>
    {#if installed && path}
      <span class="tool-path selectable">{path}</span>
    {/if}
  </div>

  <div class="tool-actions">
    {#if !installed}
      <button class="install-btn" on:click={handleInstall} disabled={installing}>
        {installing ? 'Installing...' : 'Install'}
      </button>
    {:else}
      <span class="installed-label">Installed</span>
    {/if}
  </div>
</div>

<style>
  .tool-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    background: var(--bg-card);
    transition: all 0.15s;
  }
  .tool-row:hover {
    border-color: var(--border);
  }
  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .installed-dot {
    background: var(--accent);
    box-shadow: 0 0 6px var(--accent-dim);
  }
  .missing-dot {
    background: var(--error);
    box-shadow: 0 0 6px rgba(255, 51, 102, 0.2);
  }
  .tool-info {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow: hidden;
  }
  .tool-name {
    font-size: 13px;
    color: var(--text-primary);
    font-weight: 500;
  }
  .tool-path {
    font-size: 10px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tool-actions {
    flex-shrink: 0;
  }
  .install-btn {
    all: unset;
    font-size: 11px;
    padding: 4px 12px;
    border-radius: 4px;
    border: 1px solid var(--accent);
    color: var(--accent);
    cursor: pointer;
    transition: all 0.15s;
  }
  .install-btn:hover:not(:disabled) {
    background: var(--accent-dim);
  }
  .install-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .installed-label {
    font-size: 11px;
    color: var(--text-muted);
  }
</style>
