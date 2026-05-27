<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';
  import { addOperation, updateOperation } from '../lib/operations';
  import { onEvent } from '../lib/events';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  let tools: Array<{
    name: string; installed: boolean; path: string; version: string;
    latestVersion: string; updateAvailable: boolean; category: string; description: string;
    checking: boolean; runtimeDeps: string[];
  }> = [];
  let runtimes: Array<{
    kind: string; available: boolean; version: string; path: string; local: boolean; required: boolean;
  }> = [];
  let loading = true;
  let runtimesLoading = true;
  let busy = false;
  let runtimeBusy: Record<string, boolean> = {};

  let cleanups: Array<() => void> = [];
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  // Event handlers for Wails events (GUI-initiated operations)
  function handleDownloadStart(data: any) {
    const d = data.data || data;
    const toolName = d.tool || d.Tool;
    if (!toolName) return;
    const opId = `install-${toolName}`;
    addOperation({ id: opId, type: 'download', label: `${t(lang, 'status.downloading')} ${toolName}`, status: 'running', progress: 0 });
  }

  function handleDownloadProgress(data: any) {
    const d = data.data || data;
    const toolName = d.tool || d.Tool;
    const bytes = d.bytes || d.Bytes || 0;
    const total = d.total || d.Total || 1;
    const pct = Math.round((bytes / total) * 100);
    updateOperation(`install-${toolName}`, { progress: pct });
    updateOperation(`download-${toolName}`, { progress: pct });
    updateOperation(`update-${toolName}`, { progress: pct });
  }

  function handleDownloadComplete(data: any) {
    const d = data.data || data;
    const toolName = d.tool || d.Tool;
    const error = d.error || d.Error;
    if (error) {
      updateOperation(`install-${toolName}`, { status: 'failed', error: String(error) });
      updateOperation(`download-${toolName}`, { status: 'failed', error: String(error) });
      updateOperation(`update-${toolName}`, { status: 'failed', error: String(error) });
    } else {
      updateOperation(`install-${toolName}`, { status: 'success', progress: 100 });
      updateOperation(`download-${toolName}`, { status: 'success', progress: 100 });
      updateOperation(`update-${toolName}`, { status: 'success', progress: 100 });
    }
  }

  async function handleToolInstalled(data: any) {
    const toolName = typeof data === 'string' ? data : (data?.data || data?.tool || data?.Tool || '');
    if (!toolName) return;
    try {
      const st = await ToolsService.CheckAll();
      const updated = (st || []).find((s: any) => (s.Name ?? s.name) === toolName);
      if (updated) {
        tools = tools.map(t => t.name === toolName ? {
          ...t,
          installed: updated.Installed ?? updated.installed ?? false,
          version: updated.Version ?? updated.version ?? '',
          path: updated.Path ?? updated.path ?? '',
        } : t);
      }
    } catch (e) { console.error('Refresh after tool:installed failed:', e); }
  }

  // Poll tool status periodically to catch API-triggered changes.
  // SSE EventSource doesn't work inside the Wails webview (cross-origin blocked
  // by the wails:// protocol), so we poll CheckAll() as a lightweight fallback.
  // When state changes are detected, we generate operation log entries so the
  // progress bar / operation log at the bottom reflects API-triggered installs.
  function startPolling() {
    if (pollTimer) return;
    pollTimer = setInterval(async () => {
      try {
        const statuses = await ToolsService.CheckAll();
        if (!statuses) return;
        let changed = false;
        for (const s of statuses) {
          const name = s.Name ?? s.name ?? '';
          const installed = s.Installed ?? s.installed ?? false;
          const version = s.Version ?? s.version ?? '';
          const existing = tools.find(t => t.name === name);
          if (!existing) continue;
          if (existing.installed !== installed || existing.version !== version) {
            changed = true;
            // Generate operation log entries for state changes
            if (!existing.installed && installed) {
              // Tool was installed externally (via API)
              const opId = `api-install-${name}`;
              addOperation({ id: opId, type: 'download', label: `${name} ${t(lang, 'tools.installedViaApi')}`, status: 'success', progress: 100 });
            } else if (existing.installed && !installed) {
              // Tool was removed externally (via API)
              const opId = `api-remove-${name}`;
              addOperation({ id: opId, type: 'delete', label: `${name} ${t(lang, 'tools.removedViaApi')}`, status: 'success', progress: 100 });
            } else if (existing.installed && installed && existing.version !== version) {
              // Tool was updated externally (via API)
              const opId = `api-update-${name}`;
              addOperation({ id: opId, type: 'update', label: `${name} ${t(lang, 'tools.updatedViaApi')} → ${version}`, status: 'success', progress: 100 });
            }
          }
        }
        if (changed) {
          tools = tools.map(t => {
            const fresh = (statuses as any[]).find((s: any) => (s.Name ?? s.name) === t.name);
            if (!fresh) return t;
            return {
              ...t,
              installed: fresh.Installed ?? fresh.installed ?? false,
              version: fresh.Version ?? fresh.version ?? '',
              path: fresh.Path ?? fresh.path ?? '',
            };
          });
        }
      } catch { /* ignore polling errors */ }
    }, 3000);
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  onMount(async () => {
    // Wails event listeners (for GUI-initiated operations)
    cleanups.push(onEvent('tool:download:start', handleDownloadStart));
    cleanups.push(onEvent('tool:download:progress', handleDownloadProgress));
    cleanups.push(onEvent('tool:download:complete', handleDownloadComplete));
    cleanups.push(onEvent('tool:installed', handleToolInstalled));

    await Promise.all([loadTools(), loadRuntimes()]);

    // Start polling AFTER tools are loaded, so diff has baseline
    startPolling();
  });

  onDestroy(() => {
    cleanups.forEach(fn => fn());
    stopPolling();
  });

  async function loadRuntimes() {
    try {
      const statuses = await ToolsService.CheckRuntimes();
      runtimes = (statuses || []).map((s: any) => ({
        kind: s.Kind ?? s.kind ?? '',
        available: s.Available ?? s.available ?? false,
        version: s.Version ?? s.version ?? '',
        path: s.Path ?? s.path ?? '',
        local: s.Local ?? s.local ?? false,
        required: s.Required ?? s.required ?? false,
      }));
    } catch (e) { console.error('CheckRuntimes failed:', e); }
    finally { runtimesLoading = false; }
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
      // Phase 1: instant local status (no network calls)
      const statuses = await ToolsService.CheckAll();
      tools = (statuses || []).map((s: any) => ({
        name: s.Name ?? s.name ?? '', installed: s.Installed ?? s.installed ?? false,
        path: s.Path ?? s.path ?? '', version: s.Version ?? s.version ?? '',
        latestVersion: '', updateAvailable: false,
        category: s.Category ?? s.category ?? '', description: s.Description ?? s.description ?? '',
        checking: false, runtimeDeps: s.RuntimeDeps ?? s.runtimeDeps ?? [],
      }));
      loading = false;

      // Phase 2: only auto-check if enough time has passed
      const shouldCheck = await ToolsService.ShouldCheckUpdates();
      if (shouldCheck) {
        tools = tools.map(t => ({ ...t, checking: true }));
        for (const tool of tools) {
          checkLatestVersion(tool.name);
        }
        await ToolsService.MarkUpdateChecked();
      }
    } catch (e) {
      console.error('CheckAll failed:', e);
      loading = false;
    }
  }

  $: anyChecking = tools.some(t => t.checking);

  async function forceCheckUpdates() {
    tools = tools.map(t => ({ ...t, checking: true, latestVersion: '', updateAvailable: false }));
    for (const tool of tools) {
      checkLatestVersion(tool.name);
    }
    await ToolsService.MarkUpdateChecked();
  }

  async function checkLatestVersion(name: string) {
    try {
      const result = await ToolsService.CheckLatestVersion(name);
      tools = tools.map(t =>
        t.name === name
          ? { ...t, latestVersion: result.latestVersion || '', updateAvailable: result.updateAvailable || false, checking: false }
          : t
      );
    } catch {
      tools = tools.map(t => t.name === name ? { ...t, checking: false } : t);
    }
  }

  async function installTool(e: CustomEvent<{ name: string }>) {
    const name = e.detail.name;
    const opId = `install-${name}`;
    busy = true;
    addOperation({ id: opId, type: 'download', label: `${t(lang, 'status.downloading')} ${name}`, status: 'running', progress: 0 });
    try {
      await ToolsService.Install(name);
      updateOperation(opId, { status: 'success', progress: 100 });
      // Refresh only this tool's status, not the whole list
      const st = await ToolsService.CheckAll();
      const updated = (st || []).find((s: any) => (s.Name ?? s.name) === name);
      if (updated) {
        tools = tools.map(t => t.name === name ? {
          ...t,
          installed: updated.Installed ?? updated.installed ?? false,
          version: updated.Version ?? updated.version ?? '',
          path: updated.Path ?? updated.path ?? '',
          checking: true,
        } : t);
        checkLatestVersion(name);
      }
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
      // Refresh only this tool's row
      tools = tools.map(t => t.name === name ? {
        ...t, installed: false, version: '', path: '',
      } : t);
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
        // Update just this row
        tools = tools.map(t => t.name === name ? { ...t, installed: true, checking: true } : t);
        checkLatestVersion(name);
      } catch (e: any) {
        console.error('Install failed:', name, e);
        updateOperation(opId, { status: 'failed', error: e.message || String(e) });
      }
    }
    busy = false;
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
        tools = tools.map(t => t.name === name ? { ...t, updateAvailable: false, checking: true } : t);
        checkLatestVersion(name);
      } catch (e: any) {
        console.error('Update failed:', name, e);
        updateOperation(opId, { status: 'failed', error: e.message || String(e) });
      }
    }
    busy = false;
  }

  // Reactive: recomputes when runtimes or tools change
  $: runtimeDepsMap = Object.fromEntries(tools.map(tool => [
    tool.name,
    (tool.runtimeDeps || []).map(kind => {
      const rt = runtimes.find(r => r.kind === kind);
      return rt
        ? { kind, available: rt.available, version: rt.version, local: rt.local }
        : { kind, available: false, version: '', local: false };
    })
  ]));

  $: missingCount = tools.filter(t => !t.installed).length;
  $: outdatedCount = tools.filter(t => t.updateAvailable).length;
</script>

<div class="tools-page">
  <div class="tools-header">
    <h2 class="tools-title">{t(lang, 'tools.title')}</h2>
    <div class="tools-actions">
      <button class="header-btn" on:click={forceCheckUpdates} disabled={busy || anyChecking}>{t(lang, 'tools.checkUpdates')}</button>
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
          checking={tool.checking}
          runtimeDeps={runtimeDepsMap[tool.name] || []}
          on:install={installTool} on:delete={deleteTool}
          on:install-runtime={(e) => installRuntime(e.detail.kind)} />
      {/each}
    </div>
  {/if}
</div>

<style>
  .tools-page { display: flex; flex-direction: column; height: 100%; padding: 20px; gap: 16px; }
  .tools-header { display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; }
  .tools-title { font-size: clamp(18px, 2.5vw, 28px); font-weight: 600; color: var(--text-primary); margin: 0; }
  .tools-actions { display: flex; gap: 8px; }
  .header-btn { all: unset; font-size: 12px; padding: 6px 14px; border-radius: 6px; border: 1px solid var(--accent); color: var(--accent); cursor: pointer; transition: all 0.15s; }
  .header-btn:hover:not(:disabled) { background: var(--accent-dim); }
  .header-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .header-btn-accent { background: var(--accent); color: var(--bg-page); font-weight: 600; border: none; }
  .header-btn-accent:hover:not(:disabled) { box-shadow: 0 0 12px var(--accent-dim); }
  .tools-list { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; }
  .tools-loading { color: var(--text-muted); padding: 24px; text-align: center; }

  .action-btn { all: unset; font-size: 11px; padding: 4px 10px; border-radius: 4px; cursor: pointer; transition: all 0.15s; }
  .action-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .action-download { border: 1px solid var(--accent); color: var(--accent); }
  .action-download:hover:not(:disabled) { background: var(--accent-dim); }
</style>
