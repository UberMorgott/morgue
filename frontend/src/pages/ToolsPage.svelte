<script lang="ts">
  import { onMount } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';
  import { addOperation, updateOperation } from '../lib/operations';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  let tools: Array<{
    name: string; installed: boolean; path: string; version: string;
    latestVersion: string; updateAvailable: boolean; category: string; description: string;
  }> = [];
  let runtimes: Array<{
    kind: string; available: boolean; version: string; path: string; local: boolean; required: boolean;
  }> = [];
  let loading = true;
  let busy = false;
  let runtimeBusy: Record<string, boolean> = {};

  onMount(async () => {
    await Promise.all([loadTools(), loadRuntimes()]);
  });

  async function loadRuntimes() {
    try {
      const statuses = await ToolsService.CheckRuntimes();
      runtimes = (statuses || []).map((s: any) => ({
        kind: s.Kind || s.kind || '',
        available: s.Available || s.available || false,
        version: s.Version || s.version || '',
        path: s.Path || s.path || '',
        local: s.Local || s.local || false,
        required: s.Required || s.required || false,
      }));
    } catch (e) { console.error('CheckRuntimes failed:', e); }
  }

  async function installRuntime(kind: string) {
    runtimeBusy = { ...runtimeBusy, [kind]: true };
    const opId = `install-runtime-${kind}`;
    const label = `${t(lang, 'runtimes.installing')} ${kind === 'dotnet' ? '.NET SDK' : 'Java JRE'}`;
    addOperation({ id: opId, type: 'download', label, status: 'running', progress: 0 });
    try {
      await ToolsService.InstallRuntime(kind);
      updateOperation(opId, { status: 'success', progress: 100 });
      await loadRuntimes();
    } catch (err: any) {
      console.error('Runtime install failed:', err);
      updateOperation(opId, { status: 'failed', error: err.message || String(err) });
    } finally {
      runtimeBusy = { ...runtimeBusy, [kind]: false };
    }
  }

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
    const name = e.detail.name;
    const opId = `install-${name}`;
    busy = true;
    addOperation({ id: opId, type: 'download', label: `${t(lang, 'status.downloading')} ${name}`, status: 'running', progress: 0 });
    try {
      await ToolsService.Install(name);
      updateOperation(opId, { status: 'success', progress: 100 });
      await loadTools();
    } catch (err: any) {
      console.error('Install failed:', err);
      updateOperation(opId, { status: 'failed', error: err.message || String(err) });
    } finally { busy = false; }
  }

  async function deleteTool(e: CustomEvent<{ name: string }>) {
    const name = e.detail.name;
    const opId = `delete-${name}`;
    busy = true;
    addOperation({ id: opId, type: 'delete', label: `${t(lang, 'tools.delete')} ${name}`, status: 'running', progress: 0 });
    try {
      await ToolsService.Delete(name);
      updateOperation(opId, { status: 'success', progress: 100 });
      await loadTools();
    } catch (err: any) {
      console.error('Delete failed:', err);
      updateOperation(opId, { status: 'failed', error: err.message || String(err) });
    } finally { busy = false; }
  }

  async function downloadAll() {
    busy = true;
    const missing = tools.filter(t => !t.installed);
    for (let i = 0; i < missing.length; i++) {
      const name = missing[i].name;
      const opId = `download-${name}`;
      const label = `${t(lang, 'status.downloading')} ${name} (${i + 1}/${missing.length})`;
      addOperation({ id: opId, type: 'download', label, status: 'running', progress: 0 });
      try {
        await ToolsService.Install(name);
        updateOperation(opId, { status: 'success', progress: 100 });
      } catch (e: any) {
        console.error('Install failed:', name, e);
        updateOperation(opId, { status: 'failed', error: e.message || String(e) });
      }
    }
    busy = false;
    await loadTools();
  }

  async function updateAll() {
    busy = true;
    const outdated = tools.filter(t => t.updateAvailable);
    for (let i = 0; i < outdated.length; i++) {
      const name = outdated[i].name;
      const opId = `update-${name}`;
      const label = `${t(lang, 'tools.update')} ${name} (${i + 1}/${outdated.length})`;
      addOperation({ id: opId, type: 'update', label, status: 'running', progress: 0 });
      try {
        await ToolsService.Install(name);
        updateOperation(opId, { status: 'success', progress: 100 });
      } catch (e: any) {
        console.error('Update failed:', name, e);
        updateOperation(opId, { status: 'failed', error: e.message || String(e) });
      }
    }
    busy = false;
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
  {#if runtimes.length > 0}
    <div class="runtimes-section">
      <h3 class="section-title">{t(lang, 'runtimes.title')}</h3>
      <div class="runtimes-list">
        {#each runtimes as rt}
          <div class="runtime-row" class:runtime-ok={rt.available} class:runtime-missing={!rt.available}>
            <div class="runtime-info">
              <span class="runtime-name">{rt.kind === 'dotnet' ? '.NET SDK' : 'Java JRE'}</span>
              {#if rt.required}
                <span class="runtime-badge badge-required">{t(lang, 'runtimes.required')}</span>
              {:else}
                <span class="runtime-badge badge-optional">{t(lang, 'runtimes.optional')}</span>
              {/if}
            </div>
            <div class="runtime-status">
              {#if rt.available}
                <span class="runtime-indicator indicator-ok"></span>
                <span class="runtime-ver">{rt.version || '—'}</span>
                <span class="runtime-source">{rt.local ? t(lang, 'runtimes.local') : t(lang, 'runtimes.system')}</span>
              {:else}
                <span class="runtime-indicator indicator-missing"></span>
                <span class="runtime-missing-text">{t(lang, 'runtimes.missing')}</span>
              {/if}
            </div>
            <div class="runtime-actions">
              {#if !rt.available}
                <button class="action-btn action-download" on:click={() => installRuntime(rt.kind)} disabled={runtimeBusy[rt.kind]}>
                  {runtimeBusy[rt.kind] ? t(lang, 'runtimes.installing') : t(lang, 'runtimes.install')}
                </button>
              {:else}
                <span class="runtime-ok-text">{t(lang, 'runtimes.available')}</span>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </div>
  {/if}

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

  .runtimes-section { display: flex; flex-direction: column; gap: 8px; flex-shrink: 0; }
  .section-title { font-size: 13px; font-weight: 600; color: var(--text-secondary); margin: 0; text-transform: uppercase; letter-spacing: 0.5px; }
  .runtimes-list { display: flex; flex-direction: column; gap: 6px; }
  .runtime-row {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 12px; border-radius: 6px;
    background: var(--bg-card); border: 1px solid var(--border-subtle);
    transition: all 0.15s;
  }
  .runtime-row:hover { border-color: var(--border); }
  .runtime-row.runtime-missing { opacity: 0.7; }
  .runtime-info { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; }
  .runtime-name { font-size: 13px; font-weight: 600; color: var(--text-primary); font-family: ui-monospace, monospace; }
  .runtime-badge { font-size: 9px; padding: 1px 5px; border-radius: 3px; text-transform: uppercase; letter-spacing: 0.5px; }
  .badge-required { background: var(--accent-dim); color: var(--accent); }
  .badge-optional { background: var(--border-subtle); color: var(--text-muted); }
  .runtime-status { display: flex; align-items: center; gap: 6px; min-width: 160px; flex-shrink: 0; }
  .runtime-indicator { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
  .indicator-ok { background: var(--success, #22c55e); }
  .indicator-missing { background: var(--error, #ef4444); }
  .runtime-ver { font-size: 11px; font-family: ui-monospace, monospace; color: var(--text-secondary); }
  .runtime-source { font-size: 10px; color: var(--text-muted); padding: 1px 5px; border-radius: 3px; background: var(--border-subtle); }
  .runtime-missing-text { font-size: 11px; color: var(--text-muted); font-style: italic; }
  .runtime-actions { display: flex; gap: 6px; flex-shrink: 0; }
  .runtime-ok-text { font-size: 10px; color: var(--text-muted); }
  .action-btn { all: unset; font-size: 11px; padding: 4px 10px; border-radius: 4px; cursor: pointer; transition: all 0.15s; }
  .action-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .action-download { border: 1px solid var(--accent); color: var(--accent); }
  .action-download:hover:not(:disabled) { background: var(--accent-dim); }
</style>
