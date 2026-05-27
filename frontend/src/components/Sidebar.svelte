<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  let { currentPage = $bindable('home'), lang = 'en' as Lang }: {
    currentPage?: string;
    lang?: Lang;
  } = $props();

  let collapsed = $state(false);

  const items = [
    { id: 'home',     icon: '\u2302', key: 'sidebar.home' },
    { id: 'tools',    icon: '\u2699', key: 'sidebar.tools' },
    { id: 'settings', icon: '\u2630', key: 'sidebar.settings' },
  ];

  function navigate(id: string) {
    currentPage = id;
  }
</script>

<nav class="sidebar" class:collapsed>
  <div class="nav-items">
    {#each items as item}
      <button
        class="nav-item"
        class:active={currentPage === item.id}
        onclick={() => navigate(item.id)}
        title={t(lang, item.key)}
      >
        <span class="nav-icon">{item.icon}</span>
        {#if !collapsed}
          <span class="nav-label">{t(lang, item.key)}</span>
        {/if}
      </button>
    {/each}
  </div>

  <button class="collapse-btn" onclick={() => collapsed = !collapsed} title={collapsed ? t(lang, 'sidebar.expand') : t(lang, 'sidebar.collapse')}>
    <span class="collapse-icon">{collapsed ? '\u00BB' : '\u00AB'}</span>
  </button>
</nav>

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    width: clamp(160px, 20vw, 280px);
    background: rgba(24, 18, 30, 0.95);
    border-right: 1px solid rgba(255, 155, 55, 0.12);
    padding: 8px 0;
    flex-shrink: 0;
    transition: width 0.2s ease;
    overflow: hidden;
  }
  .sidebar.collapsed {
    width: clamp(40px, 6vw, 80px);
  }
  .nav-items {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 0 6px;
  }
  .nav-item {
    all: unset;
    display: flex;
    align-items: center;
    gap: clamp(10px, 1.5vw, 20px);
    padding: clamp(8px, 1.2vw, 16px) clamp(10px, 1.5vw, 20px);
    border-left: 3px solid transparent;
    cursor: pointer;
    color: var(--text-secondary);
    font-size: clamp(13px, 1.8vw, 26px);
    transition: all 0.15s;
    white-space: nowrap;
  }
  .nav-item:hover {
    background: rgba(255, 140, 40, 0.06);
    color: var(--text-primary);
  }
  .nav-item.active {
    background: rgba(255, 130, 30, 0.15);
    border-left: 3px solid #ff8a20;
    color: #fff5eb;
  }
  .nav-icon {
    font-size: 16px;
    width: 20px;
    text-align: center;
    flex-shrink: 0;
  }
  .nav-label {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .collapse-btn {
    all: unset;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 8px;
    margin: 0 6px;
    border-radius: 6px;
    cursor: pointer;
    color: var(--text-muted);
    font-size: 14px;
    transition: all 0.15s;
  }
  .collapse-btn:hover {
    background: var(--accent-dim);
    color: var(--text-secondary);
  }
  .collapse-icon {
    font-size: 16px;
  }
</style>
