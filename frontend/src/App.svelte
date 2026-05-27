<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import OperationsFooter from './components/OperationsFooter.svelte';
  import HomePage from './pages/HomePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { ReconService, UpdateService, ToolsService } from './lib/api';
  import { currentLang, startupBusy } from './lib/stores';
  import { addOperation, updateOperation } from './lib/operations';
  import { onEvent } from './lib/events';
  import { pipelineState, updateFromEvent, addHistoryEntry, resetPipeline } from './lib/pipeline';
  import type { Lang } from './lib/i18n';

  let currentPage = 'home';
  let pipelineInputPath = '';

  let lang: Lang;
  currentLang.subscribe(v => lang = v);

  // --- API command poll: runs at App level so commands are received on any tab ---
  let apiPollTimer: ReturnType<typeof setInterval> | null = null;
  let processingApiCommand = false;

  function startApiPoll() {
    if (apiPollTimer) return;
    apiPollTimer = setInterval(async () => {
      if (processingApiCommand) return;
      try {
        const cmd = await ToolsService.PollAPICommand();
        if (!cmd || !cmd.action) return;
        processingApiCommand = true;
        try {
          switch (cmd.action) {
            case 'install': {
              const name = cmd.tool || '';
              if (!name) break;
              const opId = `api-install-${name}`;
              addOperation({ id: opId, type: 'download', label: `Installing ${name}...`, status: 'running', progress: 0 });
              try {
                await ToolsService.Install(name);
                updateOperation(opId, { status: 'success', progress: 100 });
              } catch (err: any) {
                updateOperation(opId, { status: 'failed', error: err.message || String(err) });
              }
              break;
            }
            case 'install-all': {
              const opId = 'install-all-api';
              addOperation({ id: opId, type: 'download', label: 'Installing all tools...', status: 'running', progress: 0 });
              try {
                const statuses = await ToolsService.CheckAll();
                const missing = (statuses || []).filter((s: any) => !(s.Installed ?? s.installed));
                for (const s of missing) {
                  const name = s.Name ?? s.name ?? '';
                  if (!name) continue;
                  const toolOpId = `api-install-${name}`;
                  addOperation({ id: toolOpId, type: 'download', label: `Installing ${name}...`, status: 'running', progress: 0 });
                  try {
                    await ToolsService.Install(name);
                    updateOperation(toolOpId, { status: 'success', progress: 100 });
                  } catch (e: any) {
                    updateOperation(toolOpId, { status: 'failed', error: e.message || String(e) });
                  }
                }
                updateOperation(opId, { status: 'success', progress: 100 });
              } catch (err: any) {
                updateOperation(opId, { status: 'failed', error: err.message || String(err) });
              }
              break;
            }
            case 'delete': {
              const name = cmd.tool || '';
              if (!name) break;
              const opId = `delete-${name}`;
              addOperation({ id: opId, type: 'delete', label: `Deleting ${name}...`, status: 'running', progress: 0 });
              try {
                await ToolsService.Delete(name);
                updateOperation(opId, { status: 'success', progress: 100 });
              } catch (err: any) {
                updateOperation(opId, { status: 'failed', error: err.message || String(err) });
              }
              break;
            }
            case 'run': {
              const path = cmd.path || '';
              if (!path) break;
              // Navigate to home and set input — HomePage reactive will trigger pipeline
              pipelineInputPath = path;
              currentPage = 'home';
              break;
            }
          }
        } finally {
          processingApiCommand = false;
        }
      } catch { /* ignore poll errors */ }
    }, 500);
  }

  function stopApiPoll() {
    if (apiPollTimer) {
      clearInterval(apiPollTimer);
      apiPollTimer = null;
    }
  }

  let cleanupDownloadProgress: (() => void) | null = null;
  let cleanupDownloadComplete: (() => void) | null = null;
  let cleanupExtractStart: (() => void) | null = null;
  let cleanupPipelineProgress: (() => void) | null = null;

  onDestroy(() => {
    stopApiPoll();
    cleanupDownloadProgress?.();
    cleanupDownloadComplete?.();
    cleanupExtractStart?.();
    cleanupPipelineProgress?.();
  });

  onMount(async () => {
    startApiPoll();

    cleanupDownloadProgress = onEvent('tool:download:progress', (data: any) => {
      const d = data?.data?.[0] || data?.data || data;
      const toolName = d.tool || d.Tool;
      const bytes = d.bytes || d.Bytes || 0;
      const total = d.total || d.Total || 1;
      const pct = Math.round((bytes / total) * 100);
      updateOperation(`api-install-${toolName}`, { progress: pct });
    });

    cleanupExtractStart = onEvent('tool:extract:start', (data: any) => {
      const d = data?.data?.[0] || data?.data || data;
      const toolName = d.tool || d.Tool;
      if (toolName) {
        updateOperation(`api-install-${toolName}`, { label: `Extracting ${toolName}...`, progress: 100 });
      }
    });

    cleanupDownloadComplete = onEvent('tool:download:complete', (data: any) => {
      const d = data?.data?.[0] || data?.data || data;
      const toolName = d.tool || d.Tool;
      const error = d.error || d.Error;
      if (error) {
        updateOperation(`api-install-${toolName}`, { status: 'failed', error: String(error) });
      } else {
        updateOperation(`api-install-${toolName}`, { status: 'success', progress: 100 });
      }
    });

    cleanupPipelineProgress = onEvent('pipeline:progress', (data: any) => {
      updateFromEvent(data);
    });

    const opId = 'startup-check';
    addOperation({ id: opId, type: 'update', label: 'Checking for updates...', status: 'running', progress: 0 });

    const progressCleanup = onEvent('startup:progress', (data: any) => {
      const d = data?.data?.[0] || data?.data || data;
      const label = d.label || d.phase || 'Updating...';
      updateOperation(opId, { label });
    });

    try {
      const result = await ToolsService.StartupAutoUpdate();
      const applied = result?.autoApplied;
      const appUp = result?.appUpdate;
      const toolCount = result?.toolUpdates || 0;

      if (applied) {
        updateOperation(opId, { status: 'success', progress: 100, label: 'Auto-update complete.' });
      } else if (appUp || toolCount > 0) {
        updateOperation(opId, { status: 'success', progress: 100, label: `Updates available (app: ${appUp}, tools: ${toolCount}).` });
      } else {
        updateOperation(opId, { status: 'success', progress: 100, label: 'Everything up to date.' });
      }
    } catch (e: any) {
      console.error('Startup check failed:', e);
      updateOperation(opId, { status: 'failed', error: String(e) });
    } finally {
      $startupBusy = false;
      progressCleanup();
    }
  });

  function handleFileSelected(e: CustomEvent<{ path: string }>) {
    pipelineInputPath = e.detail.path;
  }

  async function handleBrowse() {
    try {
      const file = await ReconService.PickFile();
      if (file) {
        pipelineInputPath = file;
      }
    } catch (e) {
      console.error('PickFile failed:', e);
    }
  }

  function handleClearFile() {
    pipelineInputPath = '';
  }

  async function handleAppUpdate() {
    const opId = 'app-update';
    addOperation({ id: opId, type: 'update', label: 'Обновление программы...', status: 'running', progress: 0 });
    try {
      await UpdateService.Apply();
      updateOperation(opId, { status: 'success', progress: 100, label: 'Обновление завершено. Перезапустите приложение.' });
    } catch (e: any) {
      console.error('UpdateService.Apply failed:', e);
      updateOperation(opId, { status: 'failed', error: String(e) });
    }
  }

  function handleNavigate(e: CustomEvent<{ page: string }>) {
    currentPage = e.detail.page;
  }
</script>

<div class="app-layout">
  <Header {lang} on:app-update={handleAppUpdate} on:navigate={handleNavigate} />
  <div class="main-area">
    <Sidebar bind:currentPage {lang} />
    <div class="page-content">
      {#if currentPage === 'home'}
        <HomePage {lang} inputPath={pipelineInputPath} startupBusy={$startupBusy} on:select={handleFileSelected} on:browse={handleBrowse} on:clear={handleClearFile} />
      {:else if currentPage === 'tools'}
        <ToolsPage
          {lang}
        />
      {:else if currentPage === 'settings'}
        <SettingsPage {lang} />
      {/if}
    </div>
  </div>
  <OperationsFooter />
</div>

<style>
  .app-layout {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    overflow: hidden;
  }
  .main-area {
    display: flex;
    flex: 1;
    overflow: hidden;
  }
  .page-content {
    flex: 1;
    overflow: auto;
    display: flex;
    flex-direction: column;
  }
</style>
