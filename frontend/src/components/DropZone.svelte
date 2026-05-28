<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  let { lang = 'en' as Lang, disabled = false, onselect, onbrowsefile, onbrowsedir }: {
    lang?: Lang;
    disabled?: boolean;
    onselect?: (detail: { path: string }) => void;
    onbrowsefile?: () => void;
    onbrowsedir?: () => void;
  } = $props();

  let dragover = $state(false);

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
      onselect?.({ path });
    }
  }
</script>

<div
  class="dropzone"
  class:dragover
  class:disabled
  role="region"
  ondragover={handleDragOver}
  ondragleave={handleDragLeave}
  ondrop={handleDrop}
>
  <div class="dropzone-icon">
    <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
      <polyline points="17 8 12 3 7 8" />
      <line x1="12" y1="3" x2="12" y2="15" />
    </svg>
  </div>
  <p class="dropzone-text">{t(lang, 'dropzone.text')}</p>
  <div class="dropzone-buttons">
    <button class="browse-btn" disabled={disabled} onclick={() => onbrowsefile?.()}>
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
        <polyline points="14 2 14 8 20 8" />
      </svg>
      {t(lang, 'dropzone.pickFile')}
    </button>
    <button class="browse-btn" disabled={disabled} onclick={() => onbrowsedir?.()}>
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
      </svg>
      {t(lang, 'dropzone.pickDir')}
    </button>
  </div>
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
  }
  .dropzone-text {
    font-size: 15px;
    color: var(--text-primary);
    margin: 0;
  }
  .dropzone-buttons {
    display: flex;
    gap: 12px;
    margin-top: 4px;
  }
  .browse-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 18px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-main);
    color: var(--text-primary);
    font-size: 13px;
    cursor: pointer;
    transition: all 0.15s;
  }
  .browse-btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-dim);
  }
  .browse-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
