<script lang="ts">
  import { onMount } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  let tools: Array<{ name: string; installed: boolean; path: string }> = [];
  let loading = true;
  let installingAll = false;
  let installingName = '';

  onMount(async () => {
    await loadTools();
  });

  async function loadTools() {
    loading = true;
    try {
      const statuses = await ToolsService.CheckAll();
      tools = (statuses || []).map((s: any) => ({
        name: s.Name || s.name,
        installed: s.Installed || s.installed || false,
        path: s.Path || s.path || '',
      }));
    } catch (e) {
      console.error('CheckAll failed:', e);
    } finally {
      loading = false;
    }
  }

  async function installTool(e: CustomEvent<{ name: string }>) {
    const name = e.detail.name;
    installingName = name;
    try {
      await ToolsService.Install(name);
      await loadTools();
    } catch (e) {
      console.error('Install failed:', name, e);
    } finally {
      installingName = '';
    }
  }

  async function installAllMissing() {
    installingAll = true;
    try {
      await ToolsService.InstallAll();
      await loadTools();
    } catch (e) {
      console.error('InstallAll failed:', e);
    } finally {
      installingAll = false;
    }
  }

  $: missingCount = tools.filter(t => !t.installed).length;
</script>

<div class="tools-page">
  <div class="tools-header">
    <h2 class="tools-title">{t(lang, 'tools.title')}</h2>
    {#if missingCount > 0}
      <button class="install-all-btn" on:click={installAllMissing} disabled={installingAll}>
        {installingAll ? t(lang, 'tools.installing') : `${t(lang, 'tools.installAllMissing')} (${missingCount})`}
      </button>
    {:else if !loading}
      <span class="all-installed">{t(lang, 'tools.allInstalled')}</span>
    {/if}
  </div>

  {#if loading}
    <div class="tools-loading">{t(lang, 'tools.loading')}</div>
  {:else}
    <div class="tools-list">
      {#each tools as tool}
        <ToolRow
          {lang}
          name={tool.name}
          installed={tool.installed}
          path={tool.path}
          installing={installingName === tool.name || installingAll}
          on:install={installTool}
        />
      {/each}
      {#if tools.length === 0}
        <div class="tools-empty">{t(lang, 'tools.empty')}</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tools-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 20px;
    gap: 16px;
  }
  .tools-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-shrink: 0;
  }
  .tools-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }
  .install-all-btn {
    all: unset;
    font-size: 12px;
    padding: 6px 14px;
    border-radius: 6px;
    border: 1px solid var(--accent);
    color: var(--accent);
    cursor: pointer;
    transition: all 0.15s;
  }
  .install-all-btn:hover:not(:disabled) {
    background: var(--accent-dim);
  }
  .install-all-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .all-installed {
    font-size: 12px;
    color: var(--accent);
  }
  .tools-list {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .tools-loading, .tools-empty {
    color: var(--text-muted);
    padding: 24px;
    text-align: center;
  }
</style>
