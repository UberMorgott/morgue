<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  let {
    lang = 'en' as Lang,
    name,
    installed = false,
    version = '',
    latestVersion = '',
    updateAvailable = false,
    category = '',
    description = '',
    busy = false,
    checking = false,
    runtimeDeps = [] as Array<{kind: string, available: boolean, version: string, local: boolean}>,
    oninstall,
    ondelete,
    oninstallruntime,
  }: {
    lang?: Lang;
    name: string;
    installed?: boolean;
    version?: string;
    latestVersion?: string;
    updateAvailable?: boolean;
    category?: string;
    description?: string;
    busy?: boolean;
    checking?: boolean;
    runtimeDeps?: Array<{kind: string, available: boolean, version: string, local: boolean}>;
    oninstall?: (detail: { name: string }) => void;
    ondelete?: (detail: { name: string }) => void;
    oninstallruntime?: (detail: { kind: string }) => void;
  } = $props();
</script>

<div class="tool-row" class:dimmed={!installed} class:has-deps={runtimeDeps.length > 0}>
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
      {#if checking}
        <span class="ver-spinner"></span>
      {:else if latestVersion}
        <span class="ver-sep">→</span>
        <span class="ver-latest" class:ver-new={updateAvailable}>{latestVersion}</span>
      {/if}
    {:else}
      {#if checking}
        <span class="ver-spinner"></span>
      {:else if latestVersion}
        <span class="ver-available">{latestVersion} {t(lang, 'tools.available')}</span>
      {:else}
        <span class="ver-none">—</span>
      {/if}
    {/if}
  </div>
  <div class="tool-actions">
    {#if !installed}
      <button class="action-btn action-download" onclick={() => oninstall?.({ name })} disabled={busy}>
        {t(lang, 'tools.download')}
      </button>
    {:else if updateAvailable}
      <button class="action-btn action-update" onclick={() => oninstall?.({ name })} disabled={busy}>
        {t(lang, 'tools.update')}
      </button>
    {:else}
      <span class="up-to-date">{t(lang, 'tools.upToDate')}</span>
    {/if}
    {#if installed}
      <button class="action-btn action-delete" onclick={() => ondelete?.({ name })} disabled={busy}>
        {t(lang, 'tools.delete')}
      </button>
    {/if}
  </div>
</div>
{#if runtimeDeps.length > 0}
  {#each runtimeDeps as dep}
    <div class="runtime-dep-row">
      <span class="dep-branch">&nbsp;</span>
      <span class="dep-name">{dep.kind === 'dotnet' ? '.NET SDK' : dep.kind === 'java' ? 'Java JRE' : dep.kind}</span>
      {#if dep.available}
        <span class="dep-indicator dep-ok"></span>
        <span class="dep-version">{dep.version || '—'}</span>
        <span class="dep-source">{dep.local ? t(lang, 'runtimes.local') : t(lang, 'runtimes.system')}</span>
      {:else}
        <span class="dep-indicator dep-missing"></span>
        <span class="dep-missing-text">{t(lang, 'runtimes.missing')}</span>
        <button class="dep-install-btn" onclick={() => oninstallruntime?.({ kind: dep.kind })} disabled={busy}>
          {t(lang, 'runtimes.install')}
        </button>
      {/if}
    </div>
  {/each}
{/if}

<style>
  .tool-row {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 12px; border-radius: 6px;
    background: var(--bg-card); border: 1px solid var(--border-subtle);
    transition: all 0.15s;
  }
  .tool-row.dimmed { opacity: 0.5; }
  .tool-row.has-deps { border-radius: 6px 6px 0 0; border-bottom: none; }
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
  .ver-spinner {
    display: inline-block; width: 12px; height: 12px;
    border: 2px solid var(--border-subtle); border-top-color: var(--accent);
    border-radius: 50%; animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }

  .runtime-dep-row {
    display: flex; align-items: center; gap: 8px;
    padding: 4px 12px 4px 24px; margin-top: -1px;
    font-size: 11px; color: var(--text-muted);
    background: color-mix(in srgb, var(--bg-card) 60%, transparent);
    border: 1px solid var(--border-subtle); border-top: none;
    border-radius: 0 0 6px 6px;
  }
  .dep-branch { color: var(--text-muted); font-family: ui-monospace, monospace; user-select: none; }
  .dep-name { font-weight: 600; font-family: ui-monospace, monospace; color: var(--text-secondary); min-width: 70px; }
  .dep-indicator { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
  .dep-ok { background: var(--success, #22c55e); }
  .dep-missing { background: var(--error, #ef4444); }
  .dep-version { font-family: ui-monospace, monospace; color: var(--text-secondary); }
  .dep-source { font-size: 9px; padding: 1px 4px; border-radius: 3px; background: var(--border-subtle); color: var(--text-muted); }
  .dep-missing-text { color: var(--text-muted); font-style: italic; }
  .dep-install-btn {
    all: unset; font-size: 10px; padding: 2px 8px; border-radius: 3px;
    border: 1px solid var(--accent); color: var(--accent); cursor: pointer;
    transition: all 0.15s;
  }
  .dep-install-btn:hover:not(:disabled) { background: var(--accent-dim); }
  .dep-install-btn:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
