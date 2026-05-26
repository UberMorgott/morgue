<script lang="ts">
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import StatusBar from './components/StatusBar.svelte';
  import HomePage from './pages/HomePage.svelte';
  import PipelinePage from './pages/PipelinePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { ReconService } from './lib/api';
  import { currentLang } from './lib/stores';
  import type { Lang } from './lib/i18n';

  let currentPage = 'home';
  let pipelineInputPath = '';

  let statusPhase = '';
  let statusTarget = '';
  let statusElapsed = '';
  let statusProgress = 0;
  let statusLabel = '';

  let lang: Lang;
  currentLang.subscribe(v => lang = v);

  function handleNavigate(e: CustomEvent<{ page: string; path?: string }>) {
    currentPage = e.detail.page;
    if (e.detail.path) {
      pipelineInputPath = e.detail.path;
    }
  }

  async function handleBrowseFolder() {
    try {
      const dir = await ReconService.PickDirectory();
      if (dir) {
        pipelineInputPath = dir;
        currentPage = 'pipeline';
      }
    } catch (e) {
      console.error('PickDirectory failed:', e);
    }
  }

  async function handleBrowseFile() {
    try {
      const file = await ReconService.PickFile();
      if (file) {
        pipelineInputPath = file;
        currentPage = 'pipeline';
      }
    } catch (e) {
      console.error('PickFile failed:', e);
    }
  }
</script>

<div class="app-layout">
  <Header {lang} />
  <div class="main-area">
    <Sidebar bind:currentPage {lang} />
    <div class="page-content">
      {#if currentPage === 'home'}
        <HomePage {lang} on:navigate={handleNavigate} on:browse-folder={handleBrowseFolder} on:browse-file={handleBrowseFile} />
      {:else if currentPage === 'pipeline'}
        <PipelinePage
          {lang}
          inputPath={pipelineInputPath}
          bind:statusPhase
          bind:statusProgress
          bind:statusLabel
        />
      {:else if currentPage === 'tools'}
        <ToolsPage
          {lang}
          bind:statusPhase
          bind:statusProgress
          bind:statusLabel
        />
      {:else if currentPage === 'settings'}
        <SettingsPage {lang} />
      {/if}
    </div>
  </div>
  <StatusBar
    {lang}
    phase={statusPhase}
    target={statusTarget}
    elapsed={statusElapsed}
    progress={statusProgress}
    progressLabel={statusLabel}
  />
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
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
</style>
