<script lang="ts">
  import { onMount } from 'svelte';
  import { UpdateService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';
  import { currentLang } from '../lib/stores';

  export let lang: Lang = 'en';

  function switchLang(l: Lang) {
    currentLang.set(l);
  }

  let version = 'dev';
  let updateStatus = '';
  let updateRaw = '';

  onMount(async () => {
    try {
      version = await UpdateService.GetVersion();
    } catch (e) { console.error('GetVersion failed:', e); }

    try {
      const result = await UpdateService.Check();
      if (result.available) {
        updateRaw = 'available';
        updateStatus = `${t(lang, 'header.update')}: ${result.version}`;
      } else {
        updateRaw = result.status;
        updateStatus = result.status;
      }
    } catch (e) { console.error('Check failed:', e); }
  });

  $: {
    if (updateRaw === 'available') {
      updateStatus = `${t(lang, 'header.update')}: ${updateStatus.split(': ')[1] || ''}`;
    }
  }
</script>

<header class="header">
  <div class="header-left">
    <span class="logo">MORGUE</span>
    <span class="version">v{version}</span>
  </div>
  <div class="header-right">
    {#if updateStatus}
      <span class="update-status" class:update-available={updateRaw === 'available'}>
        {updateStatus}
      </span>
    {/if}
    <div class="lang-switcher">
      <button class="lang-btn" class:lang-active={lang === 'en'} on:click={() => switchLang('en')} title="English">🇺🇸</button>
      <button class="lang-btn" class:lang-active={lang === 'ru'} on:click={() => switchLang('ru')} title="Русский">🇷🇺</button>
    </div>
  </div>
</header>

<style>
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 40px;
    padding: 0 16px;
    background: var(--bg-sidebar);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
    -webkit-app-region: drag;
  }
  .header-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .logo {
    font-size: 15px;
    font-weight: 700;
    letter-spacing: 2px;
    color: var(--accent);
    text-shadow: 0 0 12px var(--accent-dim);
  }
  .version {
    font-size: 11px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
  }
  .header-right {
    display: flex;
    align-items: center;
    gap: 12px;
    -webkit-app-region: no-drag;
  }
  .update-status {
    font-size: 11px;
    color: var(--text-muted);
  }
  .update-available {
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
    font-size: 14px;
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
