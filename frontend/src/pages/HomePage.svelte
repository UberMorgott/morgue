<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  const dispatch = createEventDispatcher();

  function handleSelect(e: CustomEvent<{ path: string }>) {
    dispatch('navigate', { page: 'pipeline', path: e.detail.path });
  }

  function handleBrowseFolder() {
    dispatch('browse-folder');
  }

  function handleBrowseFile() {
    dispatch('browse-file');
  }
</script>

<div class="home-page">
  <div class="home-hero">
    <h1 class="hero-title">{t(lang, 'home.title')}</h1>
    <p class="hero-subtitle">{t(lang, 'home.subtitle')}</p>
  </div>

  <DropZone {lang} on:select={handleSelect} on:browse={handleBrowseFolder} />

  <div class="home-actions">
    <button class="open-btn" on:click={handleBrowseFolder}>
      <span class="open-icon">📁</span>
      {t(lang, 'home.openFolder')}
    </button>
    <button class="open-btn open-btn-secondary" on:click={handleBrowseFile}>
      <span class="open-icon">📄</span>
      {t(lang, 'home.openFile')}
    </button>
  </div>
</div>

<style>
  .home-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 24px;
    height: 100%;
    padding: 32px;
  }
  .home-hero { text-align: center; }
  .hero-title {
    font-size: 28px;
    font-weight: 700;
    color: var(--text-primary);
    margin: 0 0 8px 0;
    letter-spacing: -0.5px;
  }
  .hero-subtitle {
    font-size: 14px;
    color: var(--text-muted);
    margin: 0;
  }
  .home-actions {
    display: flex;
    gap: 12px;
  }
  .open-btn {
    all: unset;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    border-radius: 8px;
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    font-size: 13px;
    cursor: pointer;
    transition: all 0.15s;
  }
  .open-btn:hover { box-shadow: 0 0 16px var(--accent-dim); }
  .open-btn-secondary {
    background: transparent;
    border: 1px solid var(--accent);
    color: var(--accent);
  }
  .open-btn-secondary:hover {
    background: var(--accent-dim);
  }
  .open-icon { font-size: 16px; }
</style>
