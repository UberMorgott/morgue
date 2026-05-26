<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let name: string;
  export let installed: boolean = false;
  export let version: string = '';
  export let latestVersion: string = '';
  export let updateAvailable: boolean = false;
  export let category: string = '';
  export let description: string = '';
  export let busy: boolean = false;

  const dispatch = createEventDispatcher();
</script>

<div class="tool-row" class:dimmed={!installed}>
  <div class="tool-info">
    <span class="tool-name">{name}</span>
    {#if category}
      <span class="tool-category">{category}</span>
    {/if}
    {#if description}
      <span class="tool-desc">{description}</span>
    {/if}
  </div>
  <div class="tool-version">
    {#if installed}
      <span class="ver-current">{version || '—'}</span>
      {#if latestVersion}
        <span class="ver-sep">→</span>
        <span class="ver-latest" class:ver-new={updateAvailable}>{latestVersion}</span>
      {/if}
    {:else}
      {#if latestVersion}
        <span class="ver-available">{latestVersion} {t(lang, 'tools.available')}</span>
      {:else}
        <span class="ver-none">—</span>
      {/if}
    {/if}
  </div>
  <div class="tool-actions">
    {#if !installed}
      <button class="action-btn action-download" on:click={() => dispatch('install', { name })} disabled={busy}>
        {t(lang, 'tools.download')}
      </button>
    {:else if updateAvailable}
      <button class="action-btn action-update" on:click={() => dispatch('install', { name })} disabled={busy}>
        {t(lang, 'tools.update')}
      </button>
    {:else}
      <span class="up-to-date">{t(lang, 'tools.upToDate')}</span>
    {/if}
    {#if installed}
      <button class="action-btn action-delete" on:click={() => dispatch('delete', { name })} disabled={busy}>
        {t(lang, 'tools.delete')}
      </button>
    {/if}
  </div>
</div>

<style>
  .tool-row {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 12px; border-radius: 6px;
    background: var(--bg-card); border: 1px solid var(--border-subtle);
    transition: all 0.15s;
  }
  .tool-row.dimmed { opacity: 0.5; }
  .tool-row:hover { border-color: var(--border); }
  .tool-info { display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0; }
  .tool-name { font-size: clamp(14px, 1.5vw, 22px); font-weight: 600; color: var(--text-primary); font-family: ui-monospace, monospace; }
  .tool-category { font-size: clamp(10px, 1vw, 13px); padding: 2px 7px; border-radius: 3px; background: var(--accent-dim); color: var(--accent); text-transform: uppercase; letter-spacing: 0.5px; }
  .tool-desc { font-size: clamp(11px, 1.2vw, 15px); color: var(--text-secondary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tool-version { display: flex; align-items: center; gap: 4px; font-size: clamp(12px, 1.2vw, 16px); font-family: ui-monospace, monospace; flex-shrink: 0; min-width: 120px; }
  .ver-current { color: var(--text-secondary); }
  .ver-sep { color: var(--text-muted); }
  .ver-latest { color: var(--text-muted); }
  .ver-new { color: var(--accent); font-weight: 600; }
  .ver-none { color: var(--text-muted); font-style: italic; }
  .ver-available { color: var(--accent); font-weight: 500; }
  .tool-actions { display: flex; gap: 6px; flex-shrink: 0; }
  .action-btn { all: unset; font-size: clamp(12px, 1.2vw, 15px); padding: 6px 14px; border-radius: 5px; cursor: pointer; transition: all 0.15s; }
  .action-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .action-download { border: 1px solid var(--accent); color: var(--accent); }
  .action-download:hover:not(:disabled) { background: var(--accent-dim); }
  .action-update { background: var(--accent); color: var(--bg-page); font-weight: 600; }
  .action-update:hover:not(:disabled) { box-shadow: 0 0 8px var(--accent-dim); }
  .action-delete { border: 1px solid var(--error); color: var(--error); }
  .action-delete:hover:not(:disabled) { background: rgba(255, 51, 102, 0.1); }
  .up-to-date { font-size: 10px; color: var(--text-muted); }
</style>
