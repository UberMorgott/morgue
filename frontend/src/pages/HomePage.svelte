<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import { ToolsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  const dispatch = createEventDispatcher();

  let toolsInstalled = 0;
  let toolsTotal = 0;

  onMount(async () => {
    try {
      const statuses = await ToolsService.CheckAll();
      toolsTotal = statuses.length;
      toolsInstalled = statuses.filter((t: any) => t.Installed || t.installed).length;
    } catch (e) {
      console.error('CheckAll failed:', e);
    }
  });

  function handleSelect(e: CustomEvent<{ path: string }>) {
    dispatch('navigate', { page: 'scan', path: e.detail.path });
  }

  function handleBrowse() {
    dispatch('browse');
  }
</script>

<div class="home-page">
  <div class="home-hero">
    <h1 class="hero-title">{t(lang, 'home.title')}</h1>
    <p class="hero-subtitle">{t(lang, 'home.subtitle')}</p>
  </div>

  <DropZone {lang} on:select={handleSelect} on:browse={handleBrowse} />

  <div class="home-stats">
    <div class="stat-card">
      <span class="stat-value">{toolsInstalled}/{toolsTotal}</span>
      <span class="stat-label">{t(lang, 'home.toolsInstalled')}</span>
    </div>
    <div class="stat-card">
      <span class="stat-value">--</span>
      <span class="stat-label">{t(lang, 'home.lastRun')}</span>
    </div>
    <div class="stat-card">
      <span class="stat-value">5</span>
      <span class="stat-label">{t(lang, 'home.recipesAvailable')}</span>
    </div>
  </div>
</div>

<style>
  .home-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 32px;
    height: 100%;
    padding: 32px;
  }
  .home-hero {
    text-align: center;
  }
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
  .home-stats {
    display: flex;
    gap: 16px;
    margin-top: 16px;
  }
  .stat-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 16px 24px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    min-width: 120px;
    transition: border-color 0.15s;
  }
  .stat-card:hover {
    border-color: var(--border);
  }
  .stat-value {
    font-size: 20px;
    font-weight: 600;
    color: var(--accent);
    font-family: ui-monospace, monospace;
  }
  .stat-label {
    font-size: 11px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
</style>
