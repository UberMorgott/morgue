<script lang="ts">
  import { onMount } from 'svelte';
  import { UpdateService, ToolsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';
  import { currentLang } from '../lib/stores';

  let { lang = 'en' as Lang, onappupdate, onnavigate }: {
    lang?: Lang;
    onappupdate?: () => void;
    onnavigate?: (detail: { page: string }) => void;
  } = $props();

  function switchLang(l: Lang) {
    currentLang.set(l);
  }

  let version = $state('dev');
  let newVersion = $state('');
  let updateRaw = $state('');
  let toolUpdatesCount = $state(0);

  onMount(async () => {
    try {
      const v = await UpdateService.GetVersion();
      version = typeof v === 'string' && v.length < 30 && !v.includes('<') ? v : 'dev';
    } catch (e) { console.error('GetVersion failed:', e); }

    try {
      const result = await UpdateService.Check();
      if (result.available) {
        updateRaw = 'available';
        newVersion = result.version;
      } else {
        updateRaw = result.status;
      }
    } catch (e) { console.error('Check failed:', e); }

    try {
      const statuses = await ToolsService.CheckAllWithUpdates();
      toolUpdatesCount = (statuses || []).filter(
        (s: any) => (s.UpdateAvailable || s.updateAvailable) && (s.Installed || s.installed)
      ).length;
    } catch (e) { console.error('ToolsService.CheckAllWithUpdates failed:', e); }
  });
</script>

<header class="header">
  <div class="header-left">
    <span class="logo">MORGUE</span>
    <span class="version">v{version}</span>
  </div>
  <div class="header-right">
    {#if updateRaw === 'available' && newVersion}
      <button class="badge badge-update" onclick={onappupdate}>
        🔄 {t(lang, 'header.updateApp')} v{newVersion}
      </button>
    {/if}
    {#if toolUpdatesCount > 0}
      <button class="badge badge-tools" onclick={() => onnavigate?.({ page: 'tools' })}>
        🔧 {toolUpdatesCount} {t(lang, 'header.toolUpdates')}
      </button>
    {/if}
    <div class="lang-switcher">
      <button class="lang-btn" class:lang-active={lang === 'en'} onclick={() => switchLang('en')} title="English">🇺🇸</button>
      <button class="lang-btn" class:lang-active={lang === 'ru'} onclick={() => switchLang('ru')} title="Русский">🇷🇺</button>
    </div>
  </div>
</header>

<style>
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: clamp(44px, 6vw, 88px);
    padding: 0 clamp(16px, 2vw, 32px);
    background: rgba(18, 14, 22, 0.92);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--glass-border);
    flex-shrink: 0;
    -webkit-app-region: drag;
  }
  .header-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .logo {
    font-family: 'Orbitron', sans-serif;
    font-size: clamp(14px, 2vw, 28px);
    font-weight: 700;
    letter-spacing: 3px;
    color: var(--accent);
    text-shadow: var(--accent-neon);
  }
  .version {
    font-size: clamp(11px, 1.5vw, 22px);
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
  }
  .header-right {
    display: flex;
    align-items: center;
    gap: clamp(8px, 1.2vw, 16px);
    -webkit-app-region: no-drag;
  }

  /* Shared badge base */
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: clamp(3px, 0.5vw, 6px) clamp(8px, 1.2vw, 14px);
    border-radius: 999px;
    font-size: clamp(11px, 1.3vw, 18px);
    font-weight: 600;
    cursor: pointer;
    border: none;
    white-space: nowrap;
    transition: transform 0.15s, box-shadow 0.15s, filter 0.15s;
    font-family: inherit;
  }
  .badge:hover {
    transform: scale(1.04);
    filter: brightness(1.15);
  }
  .badge:active {
    transform: scale(0.97);
  }

  /* App update — gradient fill + pulse */
  .badge-update {
    background: linear-gradient(135deg, var(--accent-hot) 0%, var(--accent) 50%, var(--accent-warm) 100%);
    color: #0e0a14;
    box-shadow: 0 0 10px var(--accent-glow-soft), 0 0 20px var(--accent-glow-soft);
    animation: badge-pulse 2.5s ease-in-out infinite;
  }
  .badge-update:hover {
    box-shadow: 0 0 16px var(--accent-glow), 0 0 32px var(--accent-glow-soft);
  }

  @keyframes badge-pulse {
    0%, 100% { box-shadow: 0 0 10px var(--accent-glow-soft), 0 0 20px var(--accent-glow-soft); }
    50%      { box-shadow: 0 0 16px var(--accent-glow), 0 0 30px var(--accent-glow-soft); }
  }

  /* Tool updates — outline variant */
  .badge-tools {
    background: transparent;
    color: var(--accent-warm);
    border: 1px solid var(--glass-border);
    box-shadow: none;
  }
  .badge-tools:hover {
    border-color: var(--accent);
    box-shadow: 0 0 8px var(--accent-glow-soft);
    color: var(--accent);
  }

  .lang-switcher {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .lang-btn {
    background: none;
    border: none;
    cursor: pointer;
    font-size: clamp(14px, 2vw, 28px);
    padding: 2px 4px;
    border-radius: 3px;
    opacity: 0.4;
    transition: opacity 0.15s;
  }
  .lang-btn:hover {
    opacity: 0.8;
  }
  .lang-active {
    opacity: 1;
    background: var(--bg-hover, rgba(255,255,255,0.06));
  }
</style>
