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

  import { onEvent, parseEventV2, EVENT_V2 } from './lib/events';
  import { get } from 'svelte/store';
  import { updateFromEvent, applyEventV2, startPipeline, isRunning, pipelineState } from './lib/pipeline';
  import type { Lang } from './lib/i18n';

  let currentPage = $state('home');
  let pipelineInputPath = $state('');
  let pipelineOutputPath = $state('');

  let lang: Lang = $state($currentLang);
  // Keep lang in sync with store
  const unsubLang = currentLang.subscribe(v => lang = v);

  let cleanupPipelineProgress: (() => void) | null = null;
  let cleanupPipelineV2: (() => void) | null = null;
  let cleanupUpdateProgress: (() => void) | null = null;

  onDestroy(() => {
    cleanupPipelineProgress?.();
    cleanupPipelineV2?.();
    cleanupUpdateProgress?.();
    unsubLang();
  });

  onMount(async () => {
    window.addEventListener('unhandledrejection', (e) => {
      console.error('Unhandled promise rejection:', e.reason);
    });

    // events v2: authoritative run identity, backend clock, targets and outcome.
    cleanupPipelineV2 = onEvent(EVENT_V2, (raw: any) => {
      const ev = parseEventV2(raw);
      if (!ev) return;
      const wasRunning = get(isRunning);
      const prevRun = get(pipelineState).runId;
      const res = applyEventV2(ev);
      // A new run this GUI did not start (HTTP API / CLI, or reconnect): adopt
      // its input and switch to Home once, not on every event.
      if (res === 'applied' && ev.run !== prevRun && !wasRunning) {
        const target = get(pipelineState).inputPath;
        // lastRunPath first: guards HomePage's auto-start effect from
        // re-launching the adopted path.
        $lastRunPath = target;
        pipelineInputPath = target;
        pipelineOutputPath = '';
        currentPage = 'home';
      }
    });

    cleanupPipelineProgress = onEvent('pipeline:progress', (data: any) => {
      const d = data?.data?.[0] ?? data?.data ?? data;
      // With a v2 engine the v2 handler owns adoption; legacy events only enrich.
      if (get(pipelineState).runId) { updateFromEvent(data); return; }
      // A progress event while the local store is not running means the run
      // was started elsewhere (HTTP API / CLI): GUI-started runs call
      // startPipeline before PipelineService.StartSpec. Reset the previous run's
      // state and start the elapsed clock before applying the event, adopt the
      // target, and switch to Home once — not on every event, so the user can
      // keep browsing other pages during the run.
      if (!get(isRunning) && !d?.Done) {
        const target = d?.Target || pipelineInputPath;
        // lastRunPath first: guards HomePage's auto-start effect from
        // re-launching the adopted path.
        $lastRunPath = target;
        pipelineInputPath = target;
        pipelineOutputPath = '';
        startPipeline(target);
        currentPage = 'home';
      }
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
