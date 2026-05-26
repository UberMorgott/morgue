<script lang="ts">
  import { onMount } from 'svelte';
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
  import type { Lang } from './lib/i18n';

  let currentPage = 'home';
  let pipelineInputPath = '';

  let lang: Lang;
  currentLang.subscribe(v => lang = v);

  onMount(async () => {
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
