<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';

  import { onEvent } from '../lib/events';
  import { t, type Lang } from '../lib/i18n';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  let tools: Array<{
    name: string; installed: boolean; path: string; version: string;
    latestVersion: string; updateAvailable: boolean; category: string; description: string;
    checking: boolean; runtimeDeps: string[];
  }> = $state([]);
  let runtimes: Array<{
    kind: string; available: boolean; version: string; path: string; local: boolean; required: boolean;
  }> = $state([]);
  let loading = $state(true);
  let runtimesLoading = $state(true);
  let busy = $state(false);
  let runtimeBusy: Record<string, boolean> = $state({});

  let cleanups: Array<() => void> = [];
  let pollTimer: ReturnType<typeof setInterval> | null = null;


  async function handleToolInstalled(data: any) {
    const toolName = typeof data === 'string' ? data : (data?.data || data?.tool || data?.Tool || '');
    if (!toolName) return;
    try {
      const st = await ToolsService.CheckAll();
      const updated = (st || []).find((s: any) => s.Name === toolName);
      if (updated) {
        tools = tools.map(t => t.name === toolName ? {
          ...t,
          installed: updated.Installed ?? false,
          version: updated.Version ?? '',
          path: updated.Path ?? '',
        } : t);
      }
    } catch (e) { console.error('Refresh after tool:installed failed:', e); }
  }

  function startPolling() {
    if (pollTimer) return;
    pollTimer = setInterval(async () => {
      try {
        const statuses = await ToolsService.CheckAll();
        if (!statuses) return;
        let changed = false;
        for (const s of statuses) {
          const name = s.Name ?? '';
          const installed = s.Installed ?? false;
          const version = s.Version ?? '';
          const existing = tools.find(t => t.name === name);
          if (!existing) continue;
          if (existing.installed !== installed || existing.version !== version) {
            changed = true;
          }
        }
        if (changed) {
          tools = tools.map(t => {
            const fresh = (statuses as any[]).find((s: any) => s.Name === t.name);
            if (!fresh) return t;
            return {
              ...t,
              installed: fresh.Installed ?? false,
              version: fresh.Version ?? '',
              path: fresh.Path ?? '',
            };
          });
        }
      } catch (e) { console.error('tools polling failed:', e); }
    }, 3000);
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  onMount(async () => {
    cleanups.push(onEvent('tool:installed', handleToolInstalled));

    await Promise.all([loadTools(), loadRuntimes()]);

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
        kind: s.Kind ?? '',
        available: s.Available ?? false,
        version: s.Version ?? '',
        path: s.Path ?? '',
        local: s.Local ?? false,
        required: s.Required ?? false,
      }));
    } catch (e) { console.error('CheckRuntimes failed:', e); }
    finally { runtimesLoading = false; }
  }

  async function installRuntime(kind: string) {
    runtimeBusy = { ...runtimeBusy, [kind]: true };
    try {
      await ToolsService.InstallRuntime(kind);
      await loadRuntimes();
    } catch (err: any) {
      console.error('Runtime install failed:', err);
    } finally {
      runtimeBusy = { ...runtimeBusy, [kind]: false };
    }
  }

  async function loadTools() {
    loading = true;
    try {
      const statuses = await ToolsService.CheckAll();
      tools = (statuses || []).map((s: any) => ({
        name: s.Name ?? '', installed: s.Installed ?? false,
        path: s.Path ?? '', version: s.Version ?? '',
        latestVersion: '', updateAvailable: false,
        category: s.Category ?? '', description: s.Description ?? '',
        checking: false, runtimeDeps: s.RuntimeDeps ?? [],
      }));
      loading = false;

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

  let anyChecking = $derived(tools.some(t => t.checking));

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
    } catch (e) {
      console.error('version check failed:', e);
      tools = tools.map(t => t.name === name ? { ...t, checking: false } : t);
    }
  }

  async function installTool(detail: { name: string }) {
    const name = detail.name;
    busy = true;
    try {
      await ToolsService.Install(name);
      const st = await ToolsService.CheckAll();
      const updated = (st || []).find((s: any) => s.Name === name);
      if (updated) {
        tools = tools.map(t => t.name === name ? {
          ...t,
          installed: updated.Installed ?? false,
          version: updated.Version ?? '',
          path: updated.Path ?? '',
          checking: true,
        } : t);
        checkLatestVersion(name);
      }
    } catch (err: any) {
      console.error('Install failed:', err);
    } finally { busy = false; }
  }

  async function deleteTool(detail: { name: string }) {
    const name = detail.name;
    busy = true;
    try {
      await ToolsService.Delete(name);
      tools = tools.map(t => t.name === name ? {
        ...t, installed: false, version: '', path: '',
      } : t);
    } catch (err: any) {
      console.error('Delete failed:', err);
    } finally { busy = false; }
  }

  async function downloadAll() {
    busy = true;
    const missing = tools.filter(t => !t.installed);
    for (let i = 0; i < missing.length; i++) {
      const name = missing[i].name;
      try {
        await ToolsService.Install(name);
        tools = tools.map(t => t.name === name ? { ...t, installed: true, checking: true } : t);
        checkLatestVersion(name);
      } catch (e: any) {
        console.error('Install failed:', name, e);
      }
    }
    busy = false;
  }

  async function updateAll() {
    busy = true;
    const outdated = tools.filter(t => t.updateAvailable);
    for (let i = 0; i < outdated.length; i++) {
      const name = outdated[i].name;
      try {
        await ToolsService.Install(name);
        tools = tools.map(t => t.name === name ? { ...t, updateAvailable: false, checking: true } : t);
        checkLatestVersion(name);
      } catch (e: any) {
        console.error('Update failed:', name, e);
      }
    }
    busy = false;
  }

  let runtimeDepsMap = $derived(Object.fromEntries(tools.map(tool => [
    tool.name,
    (tool.runtimeDeps || []).map(kind => {
      const rt = runtimes.find(r => r.kind === kind);
      return rt
        ? { kind, available: rt.available, version: rt.version, local: rt.local }
        : { kind, available: false, version: '', local: false };
    })
  ])));

  let missingCount = $derived(tools.filter(t => !t.installed).length);
  let outdatedCount = $derived(tools.filter(t => t.updateAvailable).length);
</script>

<div class="tools-page">
  <div class="tools-header">
    <h2 class="tools-title">{t(lang, 'tools.title')}</h2>
    <div class="tools-actions">
      <button class="header-btn" onclick={forceCheckUpdates} disabled={busy || anyChecking}>{t(lang, 'tools.checkUpdates')}</button>
      {#if missingCount > 0}
        <button class="header-btn" onclick={downloadAll} disabled={busy}>{t(lang, 'tools.downloadAll')} ({missingCount})</button>
      {/if}
      {#if outdatedCount > 0}
        <button class="header-btn header-btn-accent" onclick={updateAll} disabled={busy}>{t(lang, 'tools.updateAll')} ({outdatedCount})</button>
      {/if}
    </div>
  </div>
  {#if loading}
    <div class="tools-loading">{t(lang, 'tools.checking')}</div>
  {:else}
    <div class="tools-list">
      {#each tools as tool (tool.name)}
        <ToolRow {lang} name={tool.name} installed={tool.installed} version={tool.version}
          latestVersion={tool.latestVersion} updateAvailable={tool.updateAvailable}
          category={tool.category} description={tool.description} {busy}
          checking={tool.checking}
          runtimeDeps={runtimeDepsMap[tool.name] || []}
          oninstall={installTool} ondelete={deleteTool}
          oninstallruntime={(detail) => installRuntime(detail.kind)} />
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

</style>
