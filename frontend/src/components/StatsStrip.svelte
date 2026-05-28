<script lang="ts">
  let { platform, techStack, fileCount, totalSize, obfuscationCount = 0 }: {
    platform: string;
    techStack: string;
    fileCount: number;
    totalSize: string;
    obfuscationCount?: number;
  } = $props();
</script>

<div class="stats-strip glass">
  <!-- Platform badge -->
  <div class="stat-item">
    <span class="tag tag-kind">{platform}</span>
    {#if techStack}
      <span class="stat-tech">{techStack}</span>
    {/if}
  </div>

  <span class="stat-divider"></span>

  <!-- File count -->
  <div class="stat-item">
    <svg class="stat-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" />
      <polyline points="14 2 14 8 20 8" />
    </svg>
    <span class="stat-number">{fileCount}</span>
    <span class="stat-label">files</span>
  </div>

  <span class="stat-divider"></span>

  <!-- Total size -->
  <div class="stat-item">
    <svg class="stat-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
      <path d="M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z" />
    </svg>
    <span class="stat-number">{totalSize}</span>
  </div>

  <!-- Obfuscation count (conditional) -->
  {#if obfuscationCount > 0}
    <span class="stat-divider"></span>
    <div class="stat-item stat-obfuscation">
      <svg class="stat-icon-warn" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
      </svg>
      <span class="stat-number warn">{obfuscationCount}</span>
      <span class="stat-label warn">obfuscated</span>
    </div>
  {/if}
</div>

<style>
  .stats-strip {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 10px 20px;
    width: 100%;
    animation: fade-in 0.35s cubic-bezier(0.25, 0.46, 0.45, 0.94);
  }

  .stat-item {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .stat-divider {
    width: 1px;
    height: 20px;
    background: var(--glass-border);
    flex-shrink: 0;
  }

  .stat-icon {
    color: var(--text-muted);
    flex-shrink: 0;
  }

  .stat-icon-warn {
    color: var(--warning);
    flex-shrink: 0;
  }

  .stat-number {
    font-family: 'Orbitron', sans-serif;
    font-size: 0.88rem;
    font-weight: 600;
    color: var(--accent);
    letter-spacing: 0.5px;
  }

  .stat-number.warn {
    color: var(--warning);
  }

  .stat-label {
    font-size: 0.75rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }

  .stat-label.warn {
    color: var(--warning);
  }

  .stat-tech {
    font-size: 0.78rem;
    color: var(--text-secondary);
    margin-left: 4px;
  }

  .tag-kind {
    background: var(--accent-dim);
    color: var(--accent);
    border-color: var(--accent);
  }

  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }
</style>
