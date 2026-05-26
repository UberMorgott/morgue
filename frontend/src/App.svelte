<script lang="ts">
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import StatusBar from './components/StatusBar.svelte';
  import HomePage from './pages/HomePage.svelte';
  import ScanPage from './pages/ScanPage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import JobsPage from './pages/JobsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { currentLang } from './lib/stores';
  import type { Lang } from './lib/i18n';

  let currentPage = 'home';
  let scanInputPath = '';
  let statusPhase = '';
  let statusTarget = '';
  let statusElapsed = '';

  let lang: Lang;
  currentLang.subscribe(v => lang = v);

  function handleNavigate(e: CustomEvent<{ page: string; path?: string }>) {
    currentPage = e.detail.page;
    if (e.detail.path) {
      scanInputPath = e.detail.path;
    }
  }

  function handleBrowse() {
    // In real Wails, this would open a native folder dialog
    // For now, switch to scan with empty path
    currentPage = 'scan';
  }

  function handleStartPipeline(e: CustomEvent<{ paths: string[]; inputPath: string }>) {
    currentPage = 'jobs';
  }
</script>

<div class="app-layout">
  <Header {lang} />
  <div class="main-area">
    <Sidebar bind:currentPage {lang} />
    <div class="page-content">
      {#if currentPage === 'home'}
        <HomePage {lang} on:navigate={handleNavigate} on:browse={handleBrowse} />
      {:else if currentPage === 'scan'}
        <ScanPage {lang} inputPath={scanInputPath} on:start-pipeline={handleStartPipeline} />
      {:else if currentPage === 'tools'}
        <ToolsPage {lang} />
      {:else if currentPage === 'jobs'}
        <JobsPage {lang} />
      {:else if currentPage === 'settings'}
        <SettingsPage {lang} />
      {/if}
    </div>
  </div>
  <StatusBar {lang} phase={statusPhase} target={statusTarget} elapsed={statusElapsed} />
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
