<script lang="ts">
  export let currentPage: string = 'home';
  let collapsed = false;

  const items = [
    { id: 'home',     icon: '\u2302', label: 'Home' },
    { id: 'scan',     icon: '\u2315', label: 'Scan' },
    { id: 'tools',    icon: '\u2699', label: 'Tools' },
    { id: 'jobs',     icon: '\u25B6', label: 'Jobs' },
    { id: 'settings', icon: '\u2630', label: 'Settings' },
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
        on:click={() => navigate(item.id)}
        title={item.label}
      >
        <span class="nav-icon">{item.icon}</span>
        {#if !collapsed}
          <span class="nav-label">{item.label}</span>
        {/if}
      </button>
    {/each}
  </div>

  <button class="collapse-btn" on:click={() => collapsed = !collapsed} title={collapsed ? 'Expand' : 'Collapse'}>
    <span class="collapse-icon">{collapsed ? '\u00BB' : '\u00AB'}</span>
  </button>
</nav>

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    width: 180px;
    background: var(--bg-sidebar);
    border-right: 1px solid var(--border);
    padding: 8px 0;
    flex-shrink: 0;
    transition: width 0.2s ease;
    overflow: hidden;
  }
  .sidebar.collapsed {
    width: 48px;
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
    gap: 10px;
    padding: 8px 10px;
    border-radius: 6px;
    cursor: pointer;
    color: var(--text-secondary);
    font-size: 13px;
    transition: all 0.15s;
    white-space: nowrap;
  }
  .nav-item:hover {
    background: var(--accent-dim);
    color: var(--text-primary);
  }
  .nav-item.active {
    background: var(--accent-dim);
    color: var(--accent);
    box-shadow: inset 3px 0 0 var(--accent);
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
