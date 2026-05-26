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

  let saveTimer: ReturnType<typeof setTimeout>;
  function onConfigChange() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveConfig, 500);
  }

  function toggleField(field: string) {
    config[field] = !config[field];
    config = config;
    onConfigChange();
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
    <div class="settings-scroll">
      <!-- Language -->
      <div class="settings-lang-row">
        <span class="setting-label">{t(lang, 'settings.language')}</span>
        <select class="setting-select" bind:value={$currentLang}>
          <option value="en">English</option>
          <option value="ru">Русский</option>
        </select>
      </div>

      <div class="settings-grid">
        <!-- Section 1: Updates -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.updates')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.autoUpdateCheck')}</span>
              <div class="toggle" class:active={config.AutoUpdateCheck} on:click={() => toggleField('AutoUpdateCheck')} role="switch" tabindex="0" aria-checked={config.AutoUpdateCheck}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.autoUpdateApp')}</span>
              <div class="toggle" class:active={config.AutoUpdateApp} on:click={() => toggleField('AutoUpdateApp')} role="switch" tabindex="0" aria-checked={config.AutoUpdateApp}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.autoUpdateTools')}</span>
              <div class="toggle" class:active={config.AutoUpdateTools} on:click={() => toggleField('AutoUpdateTools')} role="switch" tabindex="0" aria-checked={config.AutoUpdateTools}></div>
            </div>
          </div>
        </section>

        <!-- Section 2: Decompilation -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.decompilation')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.decompileProject')}</span>
              <div class="toggle" class:active={config.DecompileProjectMode} on:click={() => toggleField('DecompileProjectMode')} role="switch" tabindex="0" aria-checked={config.DecompileProjectMode}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.generatePdb')}</span>
              <div class="toggle" class:active={config.GeneratePDB} on:click={() => toggleField('GeneratePDB')} role="switch" tabindex="0" aria-checked={config.GeneratePDB}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.csharpVersion')}</span>
              <select class="setting-select" bind:value={config.CSharpLanguageVersion} on:change={onConfigChange}>
                <option value="Latest">Latest</option>
                <option value="11">11</option>
                <option value="10">10</option>
                <option value="9">9</option>
              </select>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.keepIntermediates')}</span>
              <div class="toggle" class:active={config.KeepIntermediates} on:click={() => toggleField('KeepIntermediates')} role="switch" tabindex="0" aria-checked={config.KeepIntermediates}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.skipSystemLibs')}</span>
              <div class="toggle" class:active={config.SkipSystemLibs} on:click={() => toggleField('SkipSystemLibs')} role="switch" tabindex="0" aria-checked={config.SkipSystemLibs}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.stopOnFirstError')}</span>
              <div class="toggle" class:active={config.StopOnFirstError} on:click={() => toggleField('StopOnFirstError')} role="switch" tabindex="0" aria-checked={config.StopOnFirstError}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.maxFileSize')}</span>
              <input class="setting-input setting-input-num" type="number" bind:value={config.MaxFileSizeMB} on:input={onConfigChange} min="0" />
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.stepTimeout')}</span>
              <input class="setting-input setting-input-num" type="number" bind:value={config.StepTimeoutMinutes} on:input={onConfigChange} min="1" />
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.outputDir')}</span>
              <div class="setting-with-browse">
                <input class="setting-input setting-input-wide" type="text" bind:value={config.DefaultOutputDir} on:input={onConfigChange} placeholder="./decompiled" />
                <button class="browse-btn" on:click={pickOutputDir}>{t(lang, 'settings.browse')}</button>
              </div>
            </div>
          </div>
        </section>

        <!-- Section 3: Security -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.security')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.allowDynamicExecution')}</span>
              <div class="toggle" class:active={config.AllowDynamicExecution} on:click={() => toggleField('AllowDynamicExecution')} role="switch" tabindex="0" aria-checked={config.AllowDynamicExecution}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.sandboxWarning')}</span>
              <div class="toggle" class:active={config.SandboxWarning} on:click={() => toggleField('SandboxWarning')} role="switch" tabindex="0" aria-checked={config.SandboxWarning}></div>
            </div>
          </div>
        </section>

        <!-- Section 4: Logging -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.logging')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.logLevel')}</span>
              <select class="setting-select" bind:value={config.LogLevel} on:change={onConfigChange}>
                <option value="debug">debug</option>
                <option value="info">info</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </select>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.logToFile')}</span>
              <div class="toggle" class:active={config.LogToFile} on:click={() => toggleField('LogToFile')} role="switch" tabindex="0" aria-checked={config.LogToFile}></div>
            </div>
          </div>
        </section>
      </div>
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
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: clamp(16px, 1.3vw, 22px);
    font-weight: 600;
    color: var(--text-heading);
    letter-spacing: 1px;
    margin: 0;
    flex-shrink: 0;
  }

  .settings-loading {
    color: var(--text-muted);
    padding: 24px;
    text-align: center;
    font-size: clamp(12px, 1vw, 15px);
  }

  .settings-scroll {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 20px;
    padding-bottom: 24px;
  }

  .settings-lang-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 4px;
  }

  .settings-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 16px;
  }

  .settings-card {
    padding: 16px 18px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .card-title {
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: clamp(10px, 0.85vw, 13px);
    font-weight: 600;
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 1px;
    margin: 0;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border-subtle);
  }

  .card-rows {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .setting-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 32px;
  }

  .setting-label {
    font-size: clamp(11px, 0.9vw, 14px);
    color: var(--text-secondary);
    flex-shrink: 1;
    min-width: 0;
  }

  .setting-input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    padding: 6px 10px;
    font-size: clamp(11px, 0.85vw, 13px);
    font-family: ui-monospace, monospace;
    transition: border-color 0.15s, box-shadow 0.15s;
  }
  .setting-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 8px var(--accent-glow-soft);
  }

  .setting-input-num {
    width: 72px;
    text-align: center;
    flex-shrink: 0;
  }

  .setting-input-wide {
    width: 200px;
    flex-shrink: 0;
  }

  .setting-select {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: 6px;
    color: var(--text-primary);
    padding: 6px 10px;
    font-size: clamp(11px, 0.85vw, 13px);
    cursor: pointer;
    transition: border-color 0.15s;
    flex-shrink: 0;
  }
  .setting-select:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 8px var(--accent-glow-soft);
  }

  .setting-with-browse {
    display: flex;
    gap: 6px;
    align-items: center;
    flex-shrink: 0;
  }

  .browse-btn {
    all: unset;
    font-size: clamp(10px, 0.8vw, 12px);
    padding: 6px 10px;
    border-radius: 6px;
    border: 1px solid var(--accent);
    color: var(--accent);
    cursor: pointer;
    white-space: nowrap;
    transition: all 0.15s;
  }
  .browse-btn:hover {
    background: var(--accent-dim);
  }

  .save-indicator {
    position: fixed;
    bottom: 40px;
    right: 20px;
    font-size: clamp(10px, 0.8vw, 12px);
    color: var(--accent);
    background: var(--bg-card-solid);
    border: 1px solid var(--accent);
    border-radius: 6px;
    padding: 5px 12px;
    box-shadow: 0 0 12px var(--accent-glow-soft);
    animation: fadeInOut 1.2s ease;
    z-index: 100;
  }

  @keyframes fadeInOut {
    0% { opacity: 0; transform: translateY(6px); }
    15% { opacity: 1; transform: translateY(0); }
    80% { opacity: 1; }
    100% { opacity: 0; }
  }
</style>
