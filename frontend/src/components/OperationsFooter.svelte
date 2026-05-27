<script lang="ts">
  import { operations, activeOperation, clearCompleted } from '../lib/operations';
  import type { Operation, OperationType } from '../lib/operations';
  import { t, type Lang } from '../lib/i18n';
  import { currentLang } from '../lib/stores';

  let lang: Lang;
  currentLang.subscribe(v => lang = v);

  let expanded = false;

  function toggle() {
    if ($operations.length > 0) expanded = !expanded;
  }

  function typeIcon(type: OperationType): string {
    switch (type) {
      case 'download': return '⬇';
      case 'update': return '↑';
      case 'delete': return '🗑';
      case 'pipeline': return '⚙';
      default: return '●';
    }
  }

  function hasCompleted(ops: Operation[]): boolean {
    return ops.some(op => op.status === 'success' || op.status === 'failed');
  }
</script>

<footer class="ops-footer" class:expanded>
  <!-- Collapsed bar — always visible -->
  <div class="ops-bar" on:click={toggle} role="button" tabindex="0" on:keydown={(e) => e.key === 'Enter' && toggle()}>
    <div class="ops-left">
      {#if $activeOperation}
        <span class="ops-icon">{typeIcon($activeOperation.type)}</span>
        <span class="ops-label">{$activeOperation.label}</span>
      {:else}
        <span class="ops-dot"></span>
        <span class="ops-idle">{t(lang, 'status.ready')}</span>
      {/if}
    </div>

    {#if $activeOperation && $activeOperation.progress > 0 && $activeOperation.progress < 100}
      <div class="ops-progress-track">
        <div class="ops-progress-fill" style="width: {$activeOperation.progress}%"></div>
      </div>
    {/if}

    <div class="ops-right">
      {#if $activeOperation}
        <span class="ops-percent">{Math.round($activeOperation.progress)}%</span>
      {:else if $operations.length > 0}
        <span class="ops-count">{$operations.length}</span>
      {/if}
      {#if $operations.length > 0}
        <span class="ops-chevron">{expanded ? '▾' : '▴'}</span>
      {/if}
    </div>
  </div>

  <!-- Expanded panel -->
  {#if expanded}
    <div class="ops-panel">
      <div class="ops-panel-header">
        <span class="ops-panel-title">Операции</span>
        {#if hasCompleted($operations)}
          <button class="ops-clear-btn" on:click|stopPropagation={clearCompleted}>Очистить</button>
        {/if}
      </div>
      <div class="ops-list">
        {#each $operations as op (op.id)}
          <div class="ops-row" class:ops-row-running={op.status === 'running'} class:ops-row-failed={op.status === 'failed'} class:ops-row-success={op.status === 'success'}>
            <span class="ops-row-icon">{typeIcon(op.type)}</span>
            <span class="ops-row-label">{op.label}</span>
            {#if op.status === 'running' && op.progress > 0}
              <div class="ops-row-progress">
                <div class="ops-row-progress-fill" style="width: {op.progress}%"></div>
              </div>
            {/if}
            <span class="ops-row-status">
              {#if op.status === 'success'}
                <span class="status-icon status-success">✓</span>
              {:else if op.status === 'failed'}
                <span class="status-icon status-failed" title={op.error || ''}>✗</span>
              {:else if op.status === 'running'}
                <span class="status-icon status-running"><span class="mini-spinner"></span></span>
              {:else}
                <span class="status-icon status-pending">○</span>
              {/if}
            </span>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</footer>

<style>
  .ops-footer {
    position: relative;
    flex-shrink: 0;
    z-index: 100;
    font-family: "Play", "Segoe UI", system-ui, sans-serif;
  }

  .ops-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: clamp(26px, 4vw, 52px);
    padding: 0 clamp(12px, 1.5vw, 24px);
    background: rgba(24, 18, 30, 0.95);
    border-top: 1px solid rgba(255, 155, 55, 0.12);
    font-size: clamp(11px, 1.5vw, 22px);
    cursor: pointer;
    gap: 12px;
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
  }

  .ops-left {
    display: flex;
    align-items: center;
    gap: 10px;
    overflow: hidden;
    flex-shrink: 1;
    min-width: 0;
  }

  .ops-icon {
    font-size: 20px;
    flex-shrink: 0;
  }

  .ops-label {
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ops-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--success);
    box-shadow: 0 0 8px rgba(85, 238, 160, 0.5);
    flex-shrink: 0;
  }

  .ops-idle {
    color: var(--text-muted);
  }

  .ops-progress-track {
    flex: 1;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    overflow: hidden;
    min-width: 80px;
  }

  .ops-progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    box-shadow: 0 0 8px var(--accent-glow);
    transition: width 0.3s ease;
  }

  .ops-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }

  .ops-percent {
    color: var(--accent);
    font-weight: 600;
  }

  .ops-count {
    color: var(--text-muted);
    font-size: 18px;
    background: var(--accent-dim);
    padding: 1px 8px;
    border-radius: 10px;
  }

  .ops-chevron {
    color: var(--text-muted);
    font-size: 16px;
    transition: transform 0.2s;
  }

  /* ── Expanded panel ── */

  .ops-panel {
    position: absolute;
    bottom: clamp(26px, 4vw, 52px);
    left: 0;
    right: 0;
    max-height: 250px;
    background: rgba(24, 18, 30, 0.97);
    border-top: 1px solid rgba(255, 155, 55, 0.12);
    backdrop-filter: blur(24px);
    -webkit-backdrop-filter: blur(24px);
    display: flex;
    flex-direction: column;
    animation: slide-up-panel 0.2s ease;
  }

  @keyframes slide-up-panel {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .ops-panel-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 24px;
    border-bottom: 1px solid rgba(255, 155, 55, 0.08);
    flex-shrink: 0;
  }

  .ops-panel-title {
    font-size: 20px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 1px;
  }

  .ops-clear-btn {
    all: unset;
    font-size: 18px;
    color: var(--text-muted);
    cursor: pointer;
    padding: 2px 10px;
    border-radius: 4px;
    transition: all 0.15s;
    font-family: inherit;
  }

  .ops-clear-btn:hover {
    color: var(--accent);
    background: var(--accent-dim);
  }

  .ops-list {
    overflow-y: auto;
    padding: 6px 24px 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .ops-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 10px;
    border-radius: 6px;
    font-size: 20px;
    background: rgba(255, 140, 40, 0.03);
    transition: background 0.15s;
  }

  .ops-row-running {
    background: rgba(255, 138, 32, 0.08);
  }

  .ops-row-failed {
    background: rgba(255, 68, 102, 0.06);
  }

  .ops-row-success {
    opacity: 0.6;
  }

  .ops-row-icon {
    font-size: 18px;
    flex-shrink: 0;
    width: 24px;
    text-align: center;
  }

  .ops-row-label {
    color: var(--text-secondary);
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .ops-row-progress {
    width: 100px;
    height: 3px;
    background: var(--border);
    border-radius: 2px;
    overflow: hidden;
    flex-shrink: 0;
  }

  .ops-row-progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    box-shadow: 0 0 6px var(--accent-glow);
    transition: width 0.3s ease;
  }

  .ops-row-status {
    flex-shrink: 0;
    width: 24px;
    text-align: center;
  }

  .status-icon {
    font-size: 18px;
    font-weight: 700;
  }

  .status-success {
    color: var(--success);
    text-shadow: 0 0 6px rgba(85, 238, 160, 0.5);
  }

  .status-failed {
    color: var(--error);
    text-shadow: 0 0 6px rgba(255, 68, 102, 0.5);
  }

  .status-running {
    color: var(--accent);
  }

  .status-pending {
    color: var(--text-muted);
  }

  .mini-spinner {
    display: inline-block;
    width: 14px;
    height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
