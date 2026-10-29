<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';

  import { onEvent } from '../lib/events';
  import { t, type Lang } from '../lib/i18n';
  import {
    toToolView, viewChanged, toolState, isInUseError, type RawToolStatus, type ToolView,
  } from '../lib/tool-policy';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  let tools: ToolView[] = $state([]);
  let runtimes: Array<{
    kind: string; available: boolean; version: string; path: string; local: boolean; required: boolean;
  }> = $state([]);
  let loading = $state(true);
  let runtimesLoading = $state(true);
  let busy = $state(false);
  // Install/delete failures used to reach console.error only, so a failed action
  // looked exactly like a successful one.
  let opError = $state('');
  let runtimeBusy: Record<string, boolean> = $state({});

  let filterInput = $state('');
  let filterQuery = $state('');
  let filterTimer: ReturnType<typeof setTimeout> | null = null;

  function onFilterInput(e: Event) {
    filterInput = (e.target as HTMLInputElement).value;
    if (filterTimer) clearTimeout(filterTimer);
    filterTimer = setTimeout(() => {
      filterQuery = filterInput.trim().toLowerCase();
    }, 300);
  }

  let filteredTools = $derived(
    filterQuery === ''
      ? tools
      : tools.filter(t =>
          t.name.toLowerCase().includes(filterQuery) ||
          (t.description || '').toLowerCase().includes(filterQuery) ||
          (t.category || '').toLowerCase().includes(filterQuery)
        )
  );

  let cleanups: Array<() => void> = [];
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  /**
   * Merge fresh backend statuses into the list. Only the backend's own state
   * (installed/version/freshness/pin/in-use) is trusted: an update is shown as
   * done only once CheckAll reports the new activated version.
   */
  function mergeStatuses(statuses: RawToolStatus[] | null | undefined): boolean {
    if (!statuses) return false;
    let changed = false;
    const next = tools.map(prev => {
      const raw = statuses.find(s => s.Name === prev.name);
      if (!raw) return prev;
      const v = toToolView(raw, prev);
      if (viewChanged(prev, v) || v.updateAvailable !== prev.updateAvailable) { changed = true; return v; }
      return prev;
    });
    if (changed) tools = next;
    return changed;
  }

  async function refresh() {
    try {
      mergeStatuses(await ToolsService.CheckAll() as RawToolStatus[]);
    } catch (e) { console.error('tools refresh failed:', e); }
  }

  function startPolling() {
    if (pollTimer) return;
    // Polling keeps run-driven changes (auto-update, in-use leases) visible.
    pollTimer = setInterval(refresh, 3000);
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  onMount(async () => {
    cleanups.push(onEvent('tool:installed', () => { refresh(); }));

    await Promise.all([loadTools(), loadRuntimes()]);

    startPolling();
  });

  onDestroy(() => {
    cleanups.forEach(fn => fn());
    stopPolling();
    if (filterTimer) clearTimeout(filterTimer);
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

  function describeError(name: string, err: any): string {
    return isInUseError(err)
      ? `${name} ${t(lang, 'tools.inUseError')}`
      : `${name}: ${err?.message ?? err}`;
  }

  async function installRuntime(kind: string) {
    runtimeBusy = { ...runtimeBusy, [kind]: true };
    opError = '';
    try {
      await ToolsService.InstallRuntime(kind);
      await loadRuntimes();
    } catch (err: any) {
      console.error('Runtime install failed:', err);
      opError = describeError(kind, err);
    } finally {
      runtimeBusy = { ...runtimeBusy, [kind]: false };
    }
  }

  async function loadTools() {
    loading = true;
    try {
      const statuses = (await ToolsService.CheckAll() || []) as RawToolStatus[];
      tools = statuses.map(s => toToolView(s));
      loading = false;

      const shouldCheck = await ToolsService.ShouldCheckUpdates();
      if (shouldCheck) {
        await forceCheckUpdates();
      }
    } catch (e) {
      console.error('CheckAll failed:', e);
      loading = false;
    }
  }

  let anyChecking = $derived(tools.some(t => t.checking));

  async function forceCheckUpdates() {
    // Pinned and system tools are never probed for a different version.
    const probe = tools.filter(t => {
      const s = toolState(t);
      return s !== 'pinned' && s !== 'system';
    }).map(t => t.name);
    tools = tools.map(t => probe.includes(t.name) ? { ...t, checking: true } : t);
    for (const name of probe) checkLatestVersion(name);
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

  /** Unversioned install: downloads, updates, retries, or clears a pin (on success only). */
  async function installOne(name: string): Promise<boolean> {
    try {
      await ToolsService.Install(name);
      // Drop the old probe result; the backend now reports the activated version.
      tools = tools.map(t => t.name === name ? { ...t, latestVersion: '', updateAvailable: false } : t);
      return true;
    } catch (err: any) {
      console.error('Install failed:', name, err);
      opError = describeError(name, err);
      return false;
    }
  }

  async function installTool(detail: { name: string }) {
    busy = true;
    opError = '';
    try {
      if (await installOne(detail.name)) checkLatestVersion(detail.name);
    } finally {
      await refresh();
      busy = false;
    }
  }

  async function deleteTool(detail: { name: string }) {
    const name = detail.name;
    busy = true;
    opError = '';
    try {
      await ToolsService.Delete(name);
    } catch (err: any) {
      console.error('Delete failed:', err);
      opError = describeError(name, err);
    } finally {
      await refresh();
      busy = false;
    }
  }

  async function installMany(names: string[]) {
    busy = true;
    opError = '';
    try {
      for (const name of names) await installOne(name);
    } finally {
      await refresh();
      busy = false;
    }
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

  let missingNames = $derived(tools.filter(t => toolState(t) === 'missing' && !t.inUse).map(t => t.name));
  let outdatedNames = $derived(tools.filter(t => toolState(t) === 'outdated' && !t.inUse).map(t => t.name));
</script>

<div class="tools-page page-container">
  <div class="page-header">
    <h2 class="page-title">{t(lang, 'tools.title')}</h2>
    <div class="tools-actions">
      <input
        class="tools-filter"
        type="text"
        value={filterInput}
        oninput={onFilterInput}
        placeholder={t(lang, 'tools.filterPlaceholder')}
        aria-label={t(lang, 'tools.filterPlaceholder')} />
      <button class="page-action-btn" onclick={forceCheckUpdates} disabled={busy || anyChecking}>{t(lang, 'tools.checkUpdates')}</button>
      {#if missingNames.length > 0}
        <button class="page-action-btn" onclick={() => installMany(missingNames)} disabled={busy}>{t(lang, 'tools.downloadAll')} ({missingNames.length})</button>
      {/if}
      {#if outdatedNames.length > 0}
        <button class="page-action-btn accent" onclick={() => installMany(outdatedNames)} disabled={busy}>{t(lang, 'tools.updateAll')} ({outdatedNames.length})</button>
      {/if}
    </div>
  </div>
  <div class="section-label">{t(lang, 'tools.autoNote')}</div>
  {#if opError}
    <div class="alert-block alert-error">{t(lang, 'tools.actionFailed')} {opError}</div>
  {/if}
  {#if loading}
    <div class="tools-loading">{t(lang, 'tools.checking')}</div>
  {:else if tools.length === 0}
    <div class="tools-empty">{t(lang, 'tools.empty')}</div>
  {:else if filteredTools.length === 0}
    <div class="tools-empty">{t(lang, 'tools.noMatch')}</div>
  {:else}
    <div class="tools-list">
      {#each filteredTools as tool (tool.name)}
        <ToolRow {lang} {tool} {busy}
          runtimeDeps={runtimeDepsMap[tool.name] || []}
          oninstall={installTool} ondelete={deleteTool}
          oninstallruntime={(detail) => installRuntime(detail.kind)} />
      {/each}
    </div>
  {/if}
</div>

<style>
  .tools-actions { display: flex; gap: 8px; align-items: center; }
  .tools-filter { width: 200px; }
  .tools-list { flex: 1; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; }
  .tools-loading { color: var(--text-muted); padding: 24px; text-align: center; }
  .tools-empty { color: var(--text-muted); padding: 24px; text-align: center; }
</style>
