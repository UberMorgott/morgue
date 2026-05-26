<script lang="ts">
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import StatusBar from './components/StatusBar.svelte';
  import HomePage from './pages/HomePage.svelte';
  import ScanPage from './pages/ScanPage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import JobsPage from './pages/JobsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';

  let currentPage = 'home';
  let scanInputPath = '';
  let statusPhase = '';
  let statusTarget = '';
  let statusElapsed = '';

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
  <Header />
  <div class="main-area">
    <Sidebar bind:currentPage />
    <div class="page-content">
      {#if currentPage === 'home'}
        <HomePage on:navigate={handleNavigate} on:browse={handleBrowse} />
      {:else if currentPage === 'scan'}
        <ScanPage inputPath={scanInputPath} on:start-pipeline={handleStartPipeline} />
      {:else if currentPage === 'tools'}
        <ToolsPage />
      {:else if currentPage === 'jobs'}
        <JobsPage />
      {:else if currentPage === 'settings'}
        <SettingsPage />
      {/if}
    </div>
  </div>
  <StatusBar phase={statusPhase} target={statusTarget} elapsed={statusElapsed} />
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
