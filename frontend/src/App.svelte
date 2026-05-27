<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';

  import HomePage from './pages/HomePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { ReconService, UpdateService, ToolsService } from './lib/api';
  import { currentLang, startupBusy } from './lib/stores';

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
              try {
                await ToolsService.Install(name);
              } catch (err: any) {
                console.error(`API install ${name} failed:`, err);
              }
              break;
            }
            case 'install-all': {
              try {
                const statuses = await ToolsService.CheckAll();
                const missing = (statuses || []).filter((s: any) => !(s.Installed ?? s.installed));
                for (const s of missing) {
                  const name = s.Name ?? s.name ?? '';
                  if (!name) continue;
                  try {
                    await ToolsService.Install(name);
                  } catch (e: any) {
                    console.error(`API install ${name} failed:`, e);
                  }
                }
              } catch (err: any) {
                console.error('API install-all failed:', err);
              }
              break;
            }
            case 'delete': {
              const name = cmd.tool || '';
              if (!name) break;
              try {
                await ToolsService.Delete(name);
              } catch (err: any) {
                console.error(`API delete ${name} failed:`, err);
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

  let cleanupPipelineProgress: (() => void) | null = null;

  onDestroy(() => {
    stopApiPoll();
    cleanupPipelineProgress?.();
  });

  onMount(async () => {
    startApiPoll();

    cleanupPipelineProgress = onEvent('pipeline:progress', (data: any) => {
      updateFromEvent(data);
    });

    try {
      await ToolsService.StartupAutoUpdate();
    } catch (e: any) {
      console.error('Startup check failed:', e);
    } finally {
      $startupBusy = false;
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
    try {
      await UpdateService.Apply();
    } catch (e: any) {
      console.error('UpdateService.Apply failed:', e);
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
