<script lang="ts">
  import { onMount } from 'svelte';
  import { ConfigService, ReconService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';
  import { currentLang } from '../lib/stores';

  export let lang: Lang = 'en';

  let config: any = {};
  let loading = true;
  let saving = false;

  onMount(async () => {
    try {
      config = await ConfigService.Get() || {};
    } catch (e) {
      console.error('Get config failed:', e);
    } finally {
      loading = false;
    }
  });

  async function saveConfig() {
    saving = true;
    try {
      await ConfigService.Save(config);
    } catch (e) {
      console.error('Save config failed:', e);
    } finally {
      saving = false;
    }
  }

  // Auto-save on change with debounce
  let saveTimer: ReturnType<typeof setTimeout>;
  function onConfigChange() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveConfig, 500);
  }

  async function pickOutputDir() {
    try {
      const dir = await ReconService.PickDirectory();
      if (dir) {
        config.DefaultOutputDir = dir;
        onConfigChange();
      }
    } catch (e) {
      console.error('PickDirectory failed:', e);
    }
  }
</script>

<div class="settings-page">
  <h2 class="settings-title">{t(lang, 'settings.title')}</h2>

  {#if loading}
    <div class="settings-loading">{t(lang, 'settings.loading')}</div>
  {:else}
    <div class="settings-content">
      <!-- Language section -->
      <section class="settings-section">
        <h3 class="section-title">{t(lang, 'settings.language')}</h3>
        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.language')}</span>
          <select class="setting-select" bind:value={$currentLang}>
            <option value="en">English</option>
            <option value="ru">Русский</option>
          </select>
        </label>
      </section>

      <!-- Pipeline section -->
      <section class="settings-section">
        <h3 class="section-title">{t(lang, 'settings.pipeline')}</h3>

        <div class="setting-row">
          <span class="setting-label">{t(lang, 'settings.outputDir')}</span>
          <div class="setting-with-browse">
            <input
              class="setting-input"
              type="text"
              bind:value={config.DefaultOutputDir}
              on:input={onConfigChange}
              placeholder="./decompiled"
            />
            <button class="browse-btn" on:click={pickOutputDir}>{t(lang, 'settings.browse')}</button>
          </div>
        </div>

        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.stepTimeout')}</span>
          <input
            class="setting-input setting-input-sm"
            type="number"
            bind:value={config.StepTimeoutMinutes}
            on:input={onConfigChange}
            min="1"
          />
        </label>

        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.concurrentTargets')}</span>
          <input
            class="setting-input setting-input-sm"
            type="number"
            bind:value={config.ConcurrentTargets}
            on:input={onConfigChange}
            min="1" max="8"
          />
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.StopOnFirstError} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.stopOnFirstError')}</span>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.KeepIntermediates} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.keepIntermediates')}</span>
        </label>
      </section>

      <!-- Skip-list section -->
      <section class="settings-section">
        <h3 class="section-title">{t(lang, 'settings.skipList')}</h3>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.SkipSystemLibs} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.skipSystemLibs')}</span>
        </label>
      </section>

      <!-- Decompiler section -->
      <section class="settings-section">
        <h3 class="section-title">{t(lang, 'settings.decompiler')}</h3>

        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.csharpVersion')}</span>
          <input
            class="setting-input setting-input-sm"
            type="text"
            bind:value={config.CSharpLanguageVersion}
            on:input={onConfigChange}
            placeholder="Latest"
          />
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.GeneratePDB} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.generatePdb')}</span>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.DecompileProjectMode} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.decompileProject')}</span>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.GenerateCallgraph} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.generateCallgraph')}</span>
        </label>
      </section>

      <!-- Network section -->
      <section class="settings-section">
        <h3 class="section-title">{t(lang, 'settings.network')}</h3>

        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.githubToken')}</span>
          <input
            class="setting-input"
            type="password"
            bind:value={config.GitHubToken}
            on:input={onConfigChange}
            placeholder="ghp_..."
          />
        </label>

        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.downloadRetries')}</span>
          <input
            class="setting-input setting-input-sm"
            type="number"
            bind:value={config.DownloadRetries}
            on:input={onConfigChange}
            min="1"
          />
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.AutoUpdateCheck} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.autoUpdateCheck')}</span>
        </label>
      </section>

      <!-- Logging section -->
      <section class="settings-section">
        <h3 class="section-title">{t(lang, 'settings.logging')}</h3>

        <label class="setting-row">
          <span class="setting-label">{t(lang, 'settings.logLevel')}</span>
          <select class="setting-select" bind:value={config.LogLevel} on:change={onConfigChange}>
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
          </select>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.LogToFile} on:change={onConfigChange} />
          <span class="toggle-label">{t(lang, 'settings.logToFile')}</span>
        </label>
      </section>
    </div>

    {#if saving}
      <div class="save-indicator">{t(lang, 'settings.saving')}</div>
    {/if}
  {/if}
</div>

<style>
  .settings-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 20px;
    gap: 16px;
  }
  .settings-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
    flex-shrink: 0;
  }
  .settings-loading {
    color: var(--text-muted);
    padding: 24px;
    text-align: center;
  }
  .settings-content {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 24px;
    padding-bottom: 20px;
  }
  .settings-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .section-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin: 0;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--border-subtle);
  }
  .setting-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .setting-label {
    font-size: 13px;
    color: var(--text-secondary);
  }
  .setting-input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
    padding: 6px 10px;
    font-size: 12px;
    font-family: ui-monospace, monospace;
    width: 240px;
    transition: border-color 0.15s;
  }
  .setting-input:focus {
    outline: none;
    border-color: var(--accent);
  }
  .setting-input-sm {
    width: 80px;
    text-align: center;
  }
  .setting-select {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text-primary);
    padding: 6px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .setting-select:focus {
    outline: none;
    border-color: var(--accent);
  }
  .setting-toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
  }
  .setting-toggle input[type="checkbox"] {
    accent-color: var(--accent);
    width: 16px;
    height: 16px;
    cursor: pointer;
  }
  .toggle-label {
    font-size: 13px;
    color: var(--text-secondary);
  }
  .save-indicator {
    position: fixed;
    bottom: 32px;
    right: 20px;
    font-size: 11px;
    color: var(--accent);
    background: var(--bg-card);
    border: 1px solid var(--accent);
    border-radius: 4px;
    padding: 4px 10px;
    animation: fadeInOut 1s ease;
  }
  @keyframes fadeInOut {
    0% { opacity: 0; }
    20% { opacity: 1; }
    80% { opacity: 1; }
    100% { opacity: 0; }
  }
  .setting-with-browse {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .browse-btn {
    all: unset;
    font-size: 11px;
    padding: 6px 10px;
    border-radius: 4px;
    border: 1px solid var(--accent);
    color: var(--accent);
    cursor: pointer;
    white-space: nowrap;
    transition: all 0.15s;
  }
  .browse-btn:hover { background: var(--accent-dim); }
</style>
