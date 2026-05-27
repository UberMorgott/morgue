<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Browser } from '@wailsio/runtime';
  import { ConfigService, ReconService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';
  import CopyInstructions from '../components/CopyInstructions.svelte';
  import { currentLang } from '../lib/stores';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  let config: any = $state({});
  let loading = $state(true);
  let saving = $state(false);

  let saveTimer: ReturnType<typeof setTimeout>;

  onDestroy(() => clearTimeout(saveTimer));

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

  function onConfigChange() {
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveConfig, 500);
  }

  function toggleField(field: string) {
    config[field] = !config[field];
    onConfigChange();
  }

  function handleToggleKey(e: KeyboardEvent, field: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      toggleField(field);
    }
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
              <div class="toggle" class:active={config.AutoUpdateCheck} onclick={() => toggleField('AutoUpdateCheck')} onkeydown={(e) => handleToggleKey(e, 'AutoUpdateCheck')} role="switch" tabindex="0" aria-checked={config.AutoUpdateCheck}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.autoUpdateApp')}</span>
              <div class="toggle" class:active={config.AutoUpdateApp} onclick={() => toggleField('AutoUpdateApp')} onkeydown={(e) => handleToggleKey(e, 'AutoUpdateApp')} role="switch" tabindex="0" aria-checked={config.AutoUpdateApp}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.autoUpdateTools')}</span>
              <div class="toggle" class:active={config.AutoUpdateTools} onclick={() => toggleField('AutoUpdateTools')} onkeydown={(e) => handleToggleKey(e, 'AutoUpdateTools')} role="switch" tabindex="0" aria-checked={config.AutoUpdateTools}></div>
            </div>
          </div>
        </section>

        <!-- Section 2: Decompilation -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.decompilation')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.decompileProject')}</span>
              <div class="toggle" class:active={config.DecompileProjectMode} onclick={() => toggleField('DecompileProjectMode')} onkeydown={(e) => handleToggleKey(e, 'DecompileProjectMode')} role="switch" tabindex="0" aria-checked={config.DecompileProjectMode}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.generatePdb')}</span>
              <div class="toggle" class:active={config.GeneratePDB} onclick={() => toggleField('GeneratePDB')} onkeydown={(e) => handleToggleKey(e, 'GeneratePDB')} role="switch" tabindex="0" aria-checked={config.GeneratePDB}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.csharpVersion')}</span>
              <select class="setting-select" bind:value={config.CSharpLanguageVersion} onchange={onConfigChange}>
                <option value="Latest">Latest</option>
                <option value="11">11</option>
                <option value="10">10</option>
                <option value="9">9</option>
              </select>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.keepIntermediates')}</span>
              <div class="toggle" class:active={config.KeepIntermediates} onclick={() => toggleField('KeepIntermediates')} onkeydown={(e) => handleToggleKey(e, 'KeepIntermediates')} role="switch" tabindex="0" aria-checked={config.KeepIntermediates}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.skipSystemLibs')}</span>
              <div class="toggle" class:active={config.SkipSystemLibs} onclick={() => toggleField('SkipSystemLibs')} onkeydown={(e) => handleToggleKey(e, 'SkipSystemLibs')} role="switch" tabindex="0" aria-checked={config.SkipSystemLibs}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.stopOnFirstError')}</span>
              <div class="toggle" class:active={config.StopOnFirstError} onclick={() => toggleField('StopOnFirstError')} onkeydown={(e) => handleToggleKey(e, 'StopOnFirstError')} role="switch" tabindex="0" aria-checked={config.StopOnFirstError}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.maxFileSize')}</span>
              <input class="setting-input setting-input-num" type="number" bind:value={config.MaxFileSizeMB} oninput={onConfigChange} min="0" />
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.stepTimeout')}</span>
              <input class="setting-input setting-input-num" type="number" bind:value={config.StepTimeoutMinutes} oninput={onConfigChange} min="1" />
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.outputDir')}</span>
              <div class="setting-with-browse">
                <input class="setting-input setting-input-wide" type="text" bind:value={config.DefaultOutputDir} oninput={onConfigChange} placeholder="./decompiled" />
                <button class="browse-btn" onclick={pickOutputDir}>{t(lang, 'settings.browse')}</button>
              </div>
            </div>
            <div class="setting-row-block">
              <div class="setting-row">
                <span class="setting-label">{t(lang, 'settings.githubToken')}</span>
                <div class="setting-with-browse">
                  <input class="setting-input setting-input-wide" type="password" bind:value={config.GitHubToken} oninput={onConfigChange} />
                  <button class="browse-btn" onclick={() => Browser.OpenURL('https://github.com/settings/tokens/new?description=Morgue+Decompiler&scopes=public_repo')}>{t(lang, 'settings.createToken')}</button>
                </div>
              </div>
              <span class="setting-hint">{t(lang, 'settings.githubTokenHint')}</span>
            </div>
          </div>
        </section>

        <!-- Section 3: Security -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.security')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.allowDynamicExecution')}</span>
              <div class="toggle" class:active={config.AllowDynamicExecution} onclick={() => toggleField('AllowDynamicExecution')} onkeydown={(e) => handleToggleKey(e, 'AllowDynamicExecution')} role="switch" tabindex="0" aria-checked={config.AllowDynamicExecution}></div>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.sandboxWarning')}</span>
              <div class="toggle" class:active={config.SandboxWarning} onclick={() => toggleField('SandboxWarning')} onkeydown={(e) => handleToggleKey(e, 'SandboxWarning')} role="switch" tabindex="0" aria-checked={config.SandboxWarning}></div>
            </div>
          </div>
        </section>

        <!-- Section: Unreal Engine -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.unrealEngine')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.extractPak')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.extractPakHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5ExtractPAK} onclick={() => toggleField('UE5ExtractPAK')} onkeydown={(e) => handleToggleKey(e, 'UE5ExtractPAK')} role="switch" tabindex="0" aria-checked={config.UE5ExtractPAK}></div>
            </div>
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.sdkDump')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.sdkDumpHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5SDKDump} onclick={() => toggleField('UE5SDKDump')} onkeydown={(e) => handleToggleKey(e, 'UE5SDKDump')} role="switch" tabindex="0" aria-checked={config.UE5SDKDump}></div>
            </div>
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.extractStrings')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.extractStringsHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5ExtractStrings} onclick={() => toggleField('UE5ExtractStrings')} onkeydown={(e) => handleToggleKey(e, 'UE5ExtractStrings')} role="switch" tabindex="0" aria-checked={config.UE5ExtractStrings}></div>
            </div>
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.ghidraDecompile')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.ghidraDecompileHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5GhidraDecompile} onclick={() => toggleField('UE5GhidraDecompile')} onkeydown={(e) => handleToggleKey(e, 'UE5GhidraDecompile')} role="switch" tabindex="0" aria-checked={config.UE5GhidraDecompile}></div>
            </div>
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.nameResolution')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.nameResolutionHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5NameResolution} onclick={() => toggleField('UE5NameResolution')} onkeydown={(e) => handleToggleKey(e, 'UE5NameResolution')} role="switch" tabindex="0" aria-checked={config.UE5NameResolution}></div>
            </div>
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.buildIndexes')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.buildIndexesHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5BuildIndexes} onclick={() => toggleField('UE5BuildIndexes')} onkeydown={(e) => handleToggleKey(e, 'UE5BuildIndexes')} role="switch" tabindex="0" aria-checked={config.UE5BuildIndexes}></div>
            </div>
            <div class="setting-row">
              <div class="setting-label-with-hint">
                <span class="setting-label">{t(lang, 'settings.ue5.exportHookable')}</span>
                <span class="setting-hint-icon" title={t(lang, 'settings.ue5.exportHookableHint')}>?</span>
              </div>
              <div class="toggle" class:active={config.UE5ExportHookable} onclick={() => toggleField('UE5ExportHookable')} onkeydown={(e) => handleToggleKey(e, 'UE5ExportHookable')} role="switch" tabindex="0" aria-checked={config.UE5ExportHookable}></div>
            </div>
          </div>
        </section>

        <!-- Section 4: Logging -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.logging')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.logLevel')}</span>
              <select class="setting-select" bind:value={config.LogLevel} onchange={onConfigChange}>
                <option value="debug">debug</option>
                <option value="info">info</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </select>
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.logToFile')}</span>
              <div class="toggle" class:active={config.LogToFile} onclick={() => toggleField('LogToFile')} onkeydown={(e) => handleToggleKey(e, 'LogToFile')} role="switch" tabindex="0" aria-checked={config.LogToFile}></div>
            </div>
          </div>
        </section>

        <!-- Section 5: AI Integration -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.aiIntegration')}</h3>
          <div class="card-rows">
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.copyInstructions')}</span>
              <CopyInstructions {lang} />
            </div>
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.apiStatus')}</span>
              <span class="setting-value-mono">localhost:19876</span>
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

  .setting-row-block {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .setting-value-mono {
    font-size: clamp(11px, 0.85vw, 13px);
    font-family: ui-monospace, monospace;
    color: var(--text-primary);
    flex-shrink: 0;
  }

  .setting-hint {
    font-size: clamp(9px, 0.7vw, 11px);
    color: var(--text-muted);
    padding-left: 2px;
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

  .setting-label-with-hint {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 1;
    min-width: 0;
  }

  .setting-hint-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: 10px;
    cursor: help;
    flex-shrink: 0;
    transition: color 0.15s, border-color 0.15s;
  }
  .setting-hint-icon:hover {
    color: var(--accent);
    border-color: var(--accent);
  }
</style>
