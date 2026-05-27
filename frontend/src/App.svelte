<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';

  import HomePage from './pages/HomePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { ReconService, UpdateService, ToolsService } from './lib/api';
  import { currentLang, startupBusy, apiRunSeq } from './lib/stores';

  import { onEvent } from './lib/events';
  import { pipelineState, updateFromEvent, addHistoryEntry, resetPipeline } from './lib/pipeline';
  import type { Lang } from './lib/i18n';

  let currentPage = $state('home');
  let pipelineInputPath = $state('');

  let lang: Lang = $state($currentLang);
  // Keep lang in sync with store
  const unsubLang = currentLang.subscribe(v => lang = v);

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
                const missing = (statuses || []).filter((s: any) => !s.Installed);
                for (const s of missing) {
                  const name = s.Name ?? '';
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
              resetPipeline();
              pipelineInputPath = path;
              currentPage = 'home';
              await tick();
              apiRunSeq.update(n => n + 1);
              break;
            }
          }
        } finally {
          processingApiCommand = false;
        }
      } catch (e) { console.error('API poll failed:', e); }
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
    unsubLang();
  });

  onMount(async () => {
    window.addEventListener('unhandledrejection', (e) => {
      console.error('Unhandled promise rejection:', e.reason);
    });

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

  function handleFileSelected(detail: { path: string }) {
    pipelineInputPath = detail.path;
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

  function handleNavigate(detail: { page: string }) {
    currentPage = detail.page;
  }
</script>

<div class="app-layout">
  <Header {lang} onappupdate={handleAppUpdate} onnavigate={handleNavigate} />
  <div class="main-area">
    <Sidebar bind:currentPage {lang} />
    <div class="page-content">
      {#if currentPage === 'home'}
        <HomePage {lang} inputPath={pipelineInputPath} startupBusy={$startupBusy} onselect={handleFileSelected} onbrowse={handleBrowse} onclear={handleClearFile} />
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
