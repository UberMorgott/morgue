<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  type StageId = 'scan' | 'recon' | 'tools' | 'execute' | 'done';
  type StageStatus = 'pending' | 'active' | 'done' | 'error';

  let { stages, stageIds, lang }: {
    stages: Record<StageId, StageStatus>;
    stageIds: StageId[];
    lang: Lang;
  } = $props();
</script>

<div class="stepper">
  {#each stageIds as id, i (id)}
    {@const status = stages[id]}
    <div class="stage" class:stage-done={status === 'done'} class:stage-active={status === 'active'} class:stage-error={status === 'error'} class:stage-pending={status === 'pending'}>
      <div class="stage-circle">
        {#if status === 'done'}
          <svg width="14" height="14" viewBox="0 0 14 14"><path d="M2.5 7l3 3 6-6" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>
        {:else if status === 'error'}
          <svg width="14" height="14" viewBox="0 0 14 14"><path d="M3.5 3.5l7 7M10.5 3.5l-7 7" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round"/></svg>
        {:else}
          <span class="stage-dot"></span>
        {/if}
      </div>
      <span class="stage-label">{t(lang, `home.stage.${id}`)}</span>
    </div>
    {#if i < stageIds.length - 1}
      {@const nextStatus = stages[stageIds[i + 1]]}
      <div class="stage-line"
        class:line-done={status === 'done' && (nextStatus === 'done' || nextStatus === 'active')}
        class:line-active={status === 'done' && nextStatus === 'active'}
        class:line-error={status === 'error' || nextStatus === 'error'}
      ></div>
    {/if}
  {/each}
</div>

<style>
  /* -- Stage stepper (CSS grid) -- */
  .stepper {
    display: grid;
    grid-template-columns: 32px 1fr 32px 1fr 32px 1fr 32px 1fr 32px;
    align-items: start;
    width: 100%;
    padding: 8px 16px;
  }

  .stage {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    z-index: 1;
  }

  .stage-circle {
    width: 32px;
    height: 32px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    border: 2px solid var(--border);
    background: var(--bg-card-solid);
    color: var(--text-muted);
    transition: all 0.3s ease;
  }

  .stage-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-muted);
    opacity: 0.4;
  }

  .stage-label {
    font-size: 0.72rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.3px;
    font-weight: 500;
    transition: color 0.3s;
    white-space: nowrap;
  }

  .stage-done .stage-circle {
    border-color: var(--success);
    background: var(--success-dim);
    color: var(--success);
    box-shadow: 0 0 8px rgba(85, 238, 160, 0.3);
  }
  .stage-done .stage-label { color: var(--success); }

  .stage-active .stage-circle {
    border-color: transparent;
    background: var(--accent-dim);
    color: var(--accent);
    box-shadow: none;
    position: relative;
  }
  .stage-active .stage-circle::before {
    content: '';
    position: absolute;
    inset: -2px;
    border-radius: 50%;
    border: 2px solid transparent;
    border-top-color: var(--accent);
    border-right-color: var(--accent);
    animation: spin-neon 1s linear infinite;
    filter: drop-shadow(0 0 6px var(--accent));
  }
  .stage-active .stage-dot {
    background: var(--accent);
    opacity: 1;
    animation: pulse-neon 2s ease-in-out infinite;
  }
  .stage-active .stage-label { color: var(--accent); font-weight: 600; }
  @keyframes spin-neon { to { transform: rotate(360deg); } }

  .stage-error .stage-circle {
    border-color: var(--error);
    background: var(--error-dim);
    color: var(--error);
    box-shadow: 0 0 8px rgba(255, 68, 102, 0.3);
  }
  .stage-error .stage-label { color: var(--error); }

  .stage-line {
    width: 100%;
    height: 2px;
    background: var(--border-subtle);
    margin-top: 15px;
    align-self: start;
    transition: background 0.3s;
  }
  .line-done { background: var(--success); box-shadow: 0 0 4px rgba(85, 238, 160, 0.3); }
  .line-active { background: var(--accent); box-shadow: 0 0 6px var(--accent-glow); animation: pulse-neon 2s ease-in-out infinite; }
  .line-error { background: var(--error); }
</style>
