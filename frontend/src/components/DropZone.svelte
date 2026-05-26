<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let disabled: boolean = false;

  const dispatch = createEventDispatcher();
  let dragover = false;

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
    if (!disabled) dragover = true;
  }

  function handleDragLeave() {
    dragover = false;
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    dragover = false;
    if (disabled) return;
    const files = e.dataTransfer?.files;
    if (files && files.length > 0) {
      const path = (files[0] as any).path || files[0].name;
      dispatch('select', { path });
    }
  }

  function handleClick() {
    if (!disabled) dispatch('browse');
  }
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="dropzone"
  class:dragover
  class:disabled
  on:dragover={handleDragOver}
  on:dragleave={handleDragLeave}
  on:drop={handleDrop}
  on:click={handleClick}
>
  <div class="dropzone-icon">
    <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
      <polyline points="17 8 12 3 7 8" />
      <line x1="12" y1="3" x2="12" y2="15" />
    </svg>
  </div>
  <p class="dropzone-text">{t(lang, 'dropzone.text')}</p>
  <p class="dropzone-hint">{t(lang, 'dropzone.hint')}</p>
</div>

<style>
  .dropzone {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 48px 32px;
    border: 2px dashed var(--border);
    border-radius: 12px;
    background: var(--bg-card);
    transition: all 0.2s;
    max-width: min(90%, 480px);
    margin: 0 auto;
    cursor: pointer;
  }
  .dropzone:hover, .dropzone.dragover {
    border-color: var(--accent);
    background: var(--accent-dim);
    box-shadow: 0 0 24px var(--accent-dim);
  }
  .dropzone-icon {
    color: var(--text-muted);
    transition: color 0.2s;
  }
  .dropzone:hover .dropzone-icon, .dropzone.dragover .dropzone-icon {
    color: var(--accent);
  }
  .dropzone.disabled {
    opacity: 0.5;
    pointer-events: none;
    cursor: not-allowed;
  }
  .dropzone-text {
    font-size: 15px;
    color: var(--text-primary);
    margin: 0;
  }
  .dropzone-hint {
    font-size: 12px;
    color: var(--text-muted);
    margin: 0;
  }
</style>
