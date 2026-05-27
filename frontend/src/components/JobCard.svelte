<script lang="ts">
  let { target = '', status = 'completed', duration = '', recipe = '' }: {
    target?: string;
    status?: 'running' | 'completed' | 'failed';
    duration?: string;
    recipe?: string;
  } = $props();

  function basename(path: string): string {
    return path.split(/[/\\]/).pop() || path;
  }
</script>

<div class="job-card" class:running={status === 'running'} class:failed={status === 'failed'}>
  <div class="job-header">
    <span class="job-status-dot"></span>
    <span class="job-target">{basename(target)}</span>
    {#if duration}
      <span class="job-duration">{duration}</span>
    {/if}
  </div>
  {#if recipe}
    <div class="job-meta">
      <span class="job-recipe">{recipe}</span>
    </div>
  {/if}
</div>

<style>
  .job-card {
    padding: 10px 12px;
    border: 1px solid var(--border-subtle);
    border-radius: 6px;
    background: var(--bg-card);
    transition: all 0.15s;
  }
  .job-card:hover {
    border-color: var(--border);
  }
  .job-card.running {
    border-color: var(--accent);
    box-shadow: 0 0 8px var(--accent-dim);
  }
  .job-card.failed {
    border-color: var(--error);
  }
  .job-header {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .job-status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--accent);
  }
  .job-card.running .job-status-dot {
    background: var(--accent);
    animation: pulse 1.5s ease-in-out infinite;
  }
  .job-card.failed .job-status-dot {
    background: var(--error);
  }
  .job-target {
    font-size: 13px;
    color: var(--text-primary);
    font-weight: 500;
  }
  .job-duration {
    font-size: 11px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
    margin-left: auto;
  }
  .job-meta {
    margin-top: 4px;
    padding-left: 16px;
  }
  .job-recipe {
    font-size: 11px;
    color: var(--info);
    font-family: ui-monospace, monospace;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }
</style>
