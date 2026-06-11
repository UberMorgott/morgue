<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import UpdateOverlay from './components/UpdateOverlay.svelte';

  import HomePage from './pages/HomePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import AboutPage from './pages/AboutPage.svelte';
  import { ReconService, UpdateService, ToolsService } from './lib/api';
  import { currentLang, startupBusy, apiRunSeq, updateProgress, resetUpdateProgress } from './lib/stores';

  import { onEvent } from './lib/events';
  import { pipelineState, updateFromEvent, addHistoryEntry, resetPipeline } from './lib/pipeline';
  import type { Lang } from './lib/i18n';

  let currentPage = $state('home');
  let pipelineInputPath = $state('');
  let pipelineOutputPath = $state('');

  let lang: Lang = $state($currentLang);
  // Keep lang in sync with store
  const unsubLang = currentLang.subscribe(v => lang = v);

  // --- API command poll: runs at App level so commands are received on any tab ---
  let apiPollTimer: ReturnType<typeof setTimeout> | null = null;
  let pollDelay = 500;
  let processingApiCommand = false;

  function schedulePoll() {
    apiPollTimer = setTimeout(async () => {
      if (processingApiCommand) { schedulePoll(); return; }
      try {
        const cmd = await ToolsService.PollAPICommand();
        pollDelay = 500;
        if (cmd && cmd.action) {
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
                pipelineOutputPath = cmd.output || '';
                currentPage = 'home';
                await tick();
                apiRunSeq.update(n => n + 1);
                break;
              }
            }
          } finally {
            processingApiCommand = false;
          }
        }
      } catch (e) {
        pollDelay = Math.min(pollDelay * 2, 30000);
        console.error('API poll failed:', e);
      }
      schedulePoll();
    }, pollDelay);
  }

  function startApiPoll() {
    if (apiPollTimer) return;
    schedulePoll();
  }

  function stopApiPoll() {
    if (apiPollTimer) {
      clearTimeout(apiPollTimer);
      apiPollTimer = null;
    }
  }

  let cleanupPipelineProgress: (() => void) | null = null;
  let cleanupUpdateProgress: (() => void) | null = null;

  onDestroy(() => {
    stopApiPoll();
    cleanupPipelineProgress?.();
    cleanupUpdateProgress?.();
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

    // App self-update progress. Backend emits selfupdate.Progress; we surface it
    // in a modal overlay. Also covers the startup auto-update path (which emits
    // the same `update:progress` events before auto-relaunching).
    cleanupUpdateProgress = onEvent('update:progress', (evt: any) => {
      // Wails v3 wraps the emitted payload; unwrap it the same way the pipeline
      // handler does (data.data[0] → data.data → data). Reading evt.phase
      // directly yields undefined, which froze the progress bar.
      const data = evt?.data?.[0] ?? evt?.data ?? evt;
      if (!data) return;
      updateProgress.set({
        active: true,
        phase: data.phase ?? '',
        downloaded: data.downloaded ?? 0,
        total: data.total ?? 0,
        percent: data.percent ?? 0,
        version: data.version ?? '',
        error: data.error ?? '',
      });
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
    pipelineOutputPath = '';
  }

  async function handleBrowseFile() {
    try {
      const file = await ReconService.PickFile();
      if (file) {
        pipelineInputPath = file;
        pipelineOutputPath = '';
      }
    } catch (e) {
      console.error('PickFile failed:', e);
    }
  }

  async function handleBrowseDir() {
    try {
      const dir = await ReconService.PickDirectory();
      if (dir) {
        pipelineInputPath = dir;
        pipelineOutputPath = '';
      }
    } catch (e) {
      console.error('PickDirectory failed:', e);
    }
  }

  function handleClearFile() {
    pipelineInputPath = '';
    pipelineOutputPath = '';
  }

  async function handleAppUpdate() {
    // Show the overlay immediately, before the first progress event arrives.
    resetUpdateProgress();
    updateProgress.set({ active: true, phase: 'downloading', downloaded: 0, total: 0, percent: 0, version: '', error: '' });
    try {
      // On success the backend auto-relaunches and quits this process, so this
      // promise may never resolve — the overlay's "restarting" state stays up
      // until the new instance takes over. On failure it rejects; the backend
      // already emitted a PhaseError event, but surface it defensively too.
      await UpdateService.Apply();
    } catch (e: any) {
      console.error('UpdateService.Apply failed:', e);
      updateProgress.update(p => ({ ...p, active: true, phase: 'error', error: String(e?.message ?? e) }));
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
        <HomePage {lang} inputPath={pipelineInputPath} outputPath={pipelineOutputPath} startupBusy={$startupBusy} onselect={handleFileSelected} onbrowsefile={handleBrowseFile} onbrowsedir={handleBrowseDir} onclear={handleClearFile} />
      {:else if currentPage === 'tools'}
        <ToolsPage
          {lang}
        />
      {:else if currentPage === 'settings'}
        <SettingsPage {lang} />
      {:else if currentPage === 'about'}
        <AboutPage {lang} />
      {/if}
    </div>
  </div>
  <UpdateOverlay {lang} />
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
