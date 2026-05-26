<script lang="ts">
  import { createEventDispatcher } from 'svelte';

  export let type: 'success' | 'error' | 'info' = 'info';
  export let message: string = '';
  export let dismissible: boolean = true;

  const dispatch = createEventDispatcher();

  function handleDismiss() {
    dispatch('dismiss');
  }
</script>

<div class="toast toast-{type}">
  <span class="toast-message">{message}</span>
  {#if dismissible}
    <button class="toast-dismiss" on:click={handleDismiss}>&times;</button>
  {/if}
</div>

<style>
  .toast {
    font-size: 12px;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-input);
    display: flex;
    align-items: center;
    gap: 8px;
    animation: slideIn 0.2s ease;
  }
  .toast-success {
    border-color: var(--accent);
    color: var(--accent);
  }
  .toast-error {
    border-color: var(--error);
    color: var(--error);
  }
  .toast-info {
    border-color: var(--info);
    color: var(--info);
  }
  .toast-message {
    flex: 1;
  }
  .toast-dismiss {
    all: unset;
    cursor: pointer;
    font-size: 16px;
    line-height: 1;
    color: var(--text-muted);
    padding: 0 4px;
    transition: color 0.15s;
  }
  .toast-dismiss:hover {
    color: var(--text-primary);
  }
  @keyframes slideIn {
    from { transform: translateY(-8px); opacity: 0; }
    to { transform: translateY(0); opacity: 1; }
  }
</style>
