<script lang="ts">
  import ProgressRing from './ProgressRing.svelte';

  let {
    toolsNeeded = [] as string[],
    toolsInstalled = [] as string[],
    downloadingTool = '',
    downloadProgress = 0,
    downloadBytes = 0,
    downloadTotalBytes = 0,
    lastMessage = '',
  }: {
    toolsNeeded?: string[];
    toolsInstalled?: string[];
    downloadingTool?: string;
    downloadProgress?: number;
    downloadBytes?: number;
    downloadTotalBytes?: number;
    lastMessage?: string;
  } = $props();

  type ToolState = 'ready' | 'downloading' | 'extracting' | 'pending';

  function getToolState(name: string): ToolState {
    if (toolsInstalled.includes(name)) return 'ready';
    if (name === downloadingTool) {
      const msg = lastMessage.toLowerCase();
      if (msg.includes('extract') || msg.includes('распаков')) return 'extracting';
      return 'downloading';
    }
    return 'pending';
  }

  function formatMB(bytes: number): string {
    return (bytes / 1048576).toFixed(0);
  }
</script>

<div class="tools-panel glass neon-border animate-in" style="animation-delay: 0.15s;">
  <div class="tools-header">
    <span class="tools-title">⚙ ИНСТРУМЕНТЫ</span>
  </div>

  <div class="tools-list">
    {#each toolsNeeded as tool (tool)}
      {@const state = getToolState(tool)}
      <div class="tool-item">
        <div class="tool-ring">
          {#if state === 'ready'}
            <ProgressRing value={100} variant="success" label="✓" />
          {:else if state === 'downloading' || state === 'extracting'}
            <ProgressRing value={downloadProgress} variant="accent" label="{downloadProgress}%" />
          {:else}
            <ProgressRing value={0} variant="accent" label="" />
          {/if}
        </div>
        <span class="tool-name">{tool}</span>
        <span class="tool-status" class:status-ready={state === 'ready'} class:status-warm={state === 'downloading' || state === 'extracting'} class:status-muted={state === 'pending'}>
          {#if state === 'ready'}
            Готов
          {:else if state === 'downloading'}
            {formatMB(downloadBytes)} / {formatMB(downloadTotalBytes)} MB Скачивание...
          {:else if state === 'extracting'}
            Распаковка...
          {:else}
            Ожидание
          {/if}
        </span>
      </div>
    {/each}
  </div>
</div>

<style>
  .tools-panel {
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .tools-header {
    padding: 14px 20px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .tools-title {
    font-family: 'Orbitron', sans-serif;
    font-size: 0.82rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-heading);
  }

  .tools-list {
    display: flex;
    flex-direction: column;
  }

  .tool-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 20px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .tool-item:last-child {
    border-bottom: none;
  }

  .tool-ring {
    flex-shrink: 0;
  }

  .tool-name {
    font-size: 0.88rem;
    font-weight: 600;
    color: var(--text-primary);
    font-family: ui-monospace, monospace;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tool-status {
    font-size: 0.78rem;
    flex-shrink: 0;
    text-align: right;
    white-space: nowrap;
  }

  .status-ready {
    color: var(--success);
    font-weight: 600;
  }

  .status-warm {
    color: var(--accent);
    font-weight: 500;
  }

  .status-muted {
    color: var(--text-muted);
  }
</style>
