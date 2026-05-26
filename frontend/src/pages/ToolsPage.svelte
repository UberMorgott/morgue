<script lang="ts">
  import { onMount } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let statusPhase = '';
  export let statusProgress = 0;
  export let statusLabel = '';

  let tools: Array<{
    name: string; installed: boolean; path: string; version: string;
    latestVersion: string; updateAvailable: boolean; category: string; description: string;
  }> = [];
  let loading = true;
  let busy = false;

  onMount(async () => { await loadTools(); });

  async function loadTools() {
    loading = true;
    try {
      const statuses = await ToolsService.CheckAllWithUpdates();
      tools = (statuses || []).map((s: any) => ({
        name: s.Name || s.name, installed: s.Installed || s.installed || false,
        path: s.Path || s.path || '', version: s.Version || s.version || '',
        latestVersion: s.LatestVersion || s.latestVersion || '',
        updateAvailable: s.UpdateAvailable || s.updateAvailable || false,
        category: s.Category || s.category || '', description: s.Description || s.description || '',
      }));
    } catch (e) { console.error('CheckAllWithUpdates failed:', e); }
    finally { loading = false; }
  }

  async function installTool(e: CustomEvent<{ name: string }>) {
    busy = true;
    statusPhase = t(lang, 'status.downloading'); statusLabel = e.detail.name;
    try { await ToolsService.Install(e.detail.name); await loadTools(); }
    catch (e: any) { console.error('Install failed:', e); }
    finally { busy = false; statusPhase = ''; statusLabel = ''; }
  }

  async function deleteTool(e: CustomEvent<{ name: string }>) {
    busy = true;
    try { await ToolsService.Delete(e.detail.name); await loadTools(); }
    catch (e: any) { console.error('Delete failed:', e); }
    finally { busy = false; }
  }

  async function downloadAll() {
    busy = true;
    const missing = tools.filter(t => !t.installed);
    for (let i = 0; i < missing.length; i++) {
      statusPhase = t(lang, 'status.downloading');
      statusLabel = `${missing[i].name} (${i + 1}/${missing.length})`;
      statusProgress = ((i) / missing.length) * 100;
      try { await ToolsService.Install(missing[i].name); } catch (e) { console.error('Install failed:', missing[i].name, e); }
    }
    statusPhase = ''; statusProgress = 0; statusLabel = ''; busy = false;
    await loadTools();
  }

  async function updateAll() {
    busy = true;
    const outdated = tools.filter(t => t.updateAvailable);
    for (let i = 0; i < outdated.length; i++) {
      statusPhase = t(lang, 'status.downloading');
      statusLabel = `${outdated[i].name} (${i + 1}/${outdated.length})`;
      statusProgress = ((i) / outdated.length) * 100;
      try { await ToolsService.Install(outdated[i].name); } catch (e) { console.error('Update failed:', outdated[i].name, e); }
    }
    statusPhase = ''; statusProgress = 0; statusLabel = ''; busy = false;
    await loadTools();
  }

  $: missingCount = tools.filter(t => !t.installed).length;
  $: outdatedCount = tools.filter(t => t.updateAvailable).length;
</script>

<div class="tools-page">
  <div class="tools-header">
    <h2 class="tools-title">{t(lang, 'tools.title')}</h2>
    <div class="tools-actions">
      {#if missingCount > 0}
        <button class="header-btn" on:click={downloadAll} disabled={busy}>{t(lang, 'tools.downloadAll')} ({missingCount})</button>
      {/if}
      {#if outdatedCount > 0}
        <button class="header-btn header-btn-accent" on:click={updateAll} disabled={busy}>{t(lang, 'tools.updateAll')} ({outdatedCount})</button>
      {/if}
    </div>
  </div>
  {#if loading}
    <div class="tools-loading">{t(lang, 'tools.checking')}</div>
  {:else}
    <div class="tools-list">
      {#each tools as tool}
        <ToolRow {lang} name={tool.name} installed={tool.installed} version={tool.version}
          latestVersion={tool.latestVersion} updateAvailable={tool.updateAvailable}
          category={tool.category} description={tool.description} {busy}
          on:install={installTool} on:delete={deleteTool} />
      {/each}
    </div>
  {/if}
</div>

<style>
  .tools-page { display: flex; flex-direction: column; height: 100%; padding: 20px; gap: 16px; }
  .tools-header { display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; }
  .tools-title { font-size: 18px; font-weight: 600; color: var(--text-primary); margin: 0; }
  .tools-actions { display: flex; gap: 8px; }
  .header-btn { all: unset; font-size: 12px; padding: 6px 14px; border-radius: 6px; border: 1px solid var(--accent); color: var(--accent); cursor: pointer; transition: all 0.15s; }
  .header-btn:hover:not(:disabled) { background: var(--accent-dim); }
  .header-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .header-btn-accent { background: var(--accent); color: var(--bg-page); font-weight: 600; border: none; }
  .header-btn-accent:hover:not(:disabled) { box-shadow: 0 0 12px var(--accent-dim); }
  .tools-list { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; }
  .tools-loading { color: var(--text-muted); padding: 24px; text-align: center; }
</style>
