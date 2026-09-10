<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import UpdateOverlay from './components/UpdateOverlay.svelte';

  import HomePage from './pages/HomePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import AboutPage from './pages/AboutPage.svelte';
  import { ReconService, UpdateService, ToolsService } from './lib/api';
  import { currentLang, startupBusy, lastRunPath, updateProgress, resetUpdateProgress } from './lib/stores';

  import { onEvent } from './lib/events';
  import { updateFromEvent } from './lib/pipeline';
  import type { Lang } from './lib/i18n';

  let currentPage = $state('home');
  let pipelineInputPath = $state('');
  let pipelineOutputPath = $state('');

  let lang: Lang = $state($currentLang);
  // Keep lang in sync with store
  const unsubLang = currentLang.subscribe(v => lang = v);

  let cleanupPipelineProgress: (() => void) | null = null;
  let cleanupUpdateProgress: (() => void) | null = null;

  onDestroy(() => {
    cleanupPipelineProgress?.();
    cleanupUpdateProgress?.();
    unsubLang();
  });

  onMount(async () => {
    window.addEventListener('unhandledrejection', (e) => {
      console.error('Unhandled promise rejection:', e.reason);
    });

    cleanupPipelineProgress = onEvent('pipeline:progress', (data: any) => {
      updateFromEvent(data);
      // A run started from the HTTP API has no local input path and can arrive
      // while the user sits on another tab. Adopt its target and switch to Home
      // so the pipeline stays visible. updateFromEvent has already moved the
      // phase off 'idle', so HomePage's auto-start effect won't re-launch it —
      // lastRunPath is set as a second guard.
      const d = data?.data?.[0] ?? data?.data ?? data;
      if (!pipelineInputPath && d?.Target) {
        pipelineInputPath = d.Target;
        $lastRunPath = d.Target;
      }
      currentPage = 'home';
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
