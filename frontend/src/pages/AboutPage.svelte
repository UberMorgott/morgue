<script lang="ts">
  import { onMount } from 'svelte';
  import { UpdateService, ToolsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';
  import { Browser } from '@wailsio/runtime';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  let version = $state('...');
  let toolsInstalled = $state(0);
  let toolsTotal = $state(0);

  onMount(async () => {
    try {
      version = await UpdateService.GetVersion();
    } catch { /* fallback to defaults */ }

    try {
      const tools = await ToolsService.CheckAll();
      toolsTotal = tools.length;
      toolsInstalled = tools.filter((t: any) => t.Installed).length;
    } catch { /* fallback to defaults */ }
  });

  function openLink(url: string) {
    try { Browser.OpenURL(url); } catch { window.open(url, '_blank'); }
  }
</script>

<div class="about-page page-container">
  <h2 class="page-title">{t(lang, 'about.title')}</h2>

  <div class="about-grid">
    <!-- App Info -->
    <section class="about-card glass">
      <div class="app-header">
        <div class="app-icon">M</div>
        <div>
          <h3 class="app-name">Morgue</h3>
          <p class="app-subtitle">Binary Decompilation Orchestrator</p>
        </div>
      </div>
      <p class="app-description">{t(lang, 'about.description')}</p>
    </section>

    <!-- Build Info -->
    <section class="about-card glass">
      <h3 class="card-title">{t(lang, 'about.version')}</h3>
      <div class="info-rows">
        <div class="info-row">
          <span class="info-label">{t(lang, 'about.version')}</span>
          <span class="info-value tag">{version}</span>
        </div>
        <div class="info-row">
          <span class="info-label">{t(lang, 'about.platform')}</span>
          <span class="info-value mono">{navigator.platform === 'Win32' ? 'Windows x64' : navigator.platform}</span>
        </div>
        <div class="info-row">
          <span class="info-label">{t(lang, 'about.license')}</span>
          <span class="info-value">Non-Commercial Research</span>
        </div>
        <div class="info-row">
          <span class="info-label">{t(lang, 'about.tools')}</span>
          <span class="info-value">{toolsInstalled} / {toolsTotal}</span>
        </div>
      </div>
    </section>

    <!-- Author -->
    <section class="about-card glass">
      <h3 class="card-title">{t(lang, 'about.author')}</h3>
      <div class="author-row">
        <div class="author-avatar">U</div>
        <div>
          <div class="author-name">UberMorgott</div>
          <div class="author-role">Developer</div>
        </div>
      </div>
    </section>

    <!-- Disclaimer -->
    <section class="about-card glass disclaimer-card">
      <h3 class="card-title">Disclaimer</h3>
      <p class="disclaimer-text">
        {t(lang, 'about.disclaimer')}
      </p>
    </section>

    <!-- Links -->
    <section class="about-card glass">
      <h3 class="card-title">{t(lang, 'about.links')}</h3>
      <div class="link-rows">
        <button class="link-row" onclick={() => openLink('https://github.com/UberMorgott/morgue')}>
          <span class="link-icon">&#xe900;</span>
          <span>{t(lang, 'about.github')}</span>
          <span class="link-arrow">&rarr;</span>
        </button>
        <button class="link-row" onclick={() => openLink('https://github.com/UberMorgott/morgue/issues')}>
          <span class="link-icon">&#x26A0;</span>
          <span>{t(lang, 'about.issues')}</span>
          <span class="link-arrow">&rarr;</span>
        </button>
        <button class="link-row" onclick={() => openLink('https://github.com/UberMorgott/morgue/releases')}>
          <span class="link-icon">&#x2B07;</span>
          <span>{t(lang, 'about.releases')}</span>
          <span class="link-arrow">&rarr;</span>
        </button>
      </div>
    </section>
  </div>
</div>

<style>
  .about-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: clamp(12px, 2vw, 20px);
  }
  .about-card {
    padding: clamp(16px, 2vw, 24px);
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  /* App header */
  .app-header {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .app-icon {
    width: 48px;
    height: 48px;
    border-radius: 12px;
    background: linear-gradient(135deg, #ff8a20, #e05500);
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: 24px;
    font-weight: 700;
    color: #fff;
    flex-shrink: 0;
  }
  .app-name {
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: clamp(18px, 2.5vw, 24px);
    color: var(--text-primary);
    margin: 0;
  }
  .app-subtitle {
    font-size: clamp(11px, 1.3vw, 13px);
    color: var(--text-muted);
    margin: 2px 0 0;
  }
  .app-description {
    font-size: clamp(12px, 1.4vw, 14px);
    color: var(--text-secondary);
    line-height: 1.5;
    margin: 0;
  }

  /* Info rows */
  .info-rows {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .info-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 4px 0;
    border-bottom: 1px solid rgba(255, 155, 55, 0.06);
  }
  .info-row:last-child {
    border-bottom: none;
  }
  .info-label {
    font-size: clamp(11px, 1.3vw, 13px);
    color: var(--text-muted);
  }
  .info-value {
    font-size: clamp(11px, 1.3vw, 13px);
    color: var(--text-primary);
  }
  .info-value.tag {
    background: rgba(255, 140, 40, 0.15);
    color: var(--accent);
    padding: 2px 8px;
    border-radius: 4px;
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: clamp(10px, 1.2vw, 12px);
  }
  .info-value.mono {
    font-family: monospace;
    font-size: clamp(10px, 1.2vw, 12px);
    color: var(--text-secondary);
  }

  /* Author */
  .author-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .author-avatar {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: linear-gradient(135deg, #6a3de8, #9b59b6);
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: 'Orbitron', 'Play', sans-serif;
    font-weight: 700;
    font-size: 18px;
    color: #fff;
    flex-shrink: 0;
  }
  .author-name {
    font-size: clamp(13px, 1.5vw, 16px);
    color: var(--text-primary);
    font-weight: 600;
  }
  .author-role {
    font-size: clamp(11px, 1.2vw, 12px);
    color: var(--text-muted);
  }

  /* Links */
  .link-rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .link-row {
    all: unset;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    border-radius: 8px;
    cursor: pointer;
    font-size: clamp(12px, 1.4vw, 14px);
    color: var(--text-secondary);
    transition: all 0.15s;
  }
  .link-row:hover {
    background: rgba(255, 140, 40, 0.08);
    color: var(--text-primary);
  }
  .link-row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .link-icon {
    width: 20px;
    text-align: center;
    font-size: 14px;
  }
  .link-arrow {
    margin-left: auto;
    color: var(--text-muted);
    font-size: 12px;
  }

  /* Disclaimer */
  .disclaimer-card {
    border-color: rgba(255, 200, 50, 0.15);
  }
  .disclaimer-text {
    font-size: clamp(11px, 1.3vw, 13px);
    color: var(--text-muted);
    line-height: 1.6;
    margin: 0;
  }
</style>
