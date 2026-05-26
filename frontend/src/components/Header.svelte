<script lang="ts">
  import { onMount } from 'svelte';
  import { UpdateService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

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
</style>
