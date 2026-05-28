<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Browser } from '@wailsio/runtime';
  import { ConfigService, ReconService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';
  import CopyInstructions from '../components/CopyInstructions.svelte';
  import SettingsToggle from '../components/SettingsToggle.svelte';
  import { currentLang } from '../lib/stores';

  let { lang = 'en' as Lang }: { lang?: Lang } = $props();

  let config: any = $state({});
  let loading = $state(true);
  let saving = $state(false);

  let saveTimer: ReturnType<typeof setTimeout>;

  onDestroy(() => clearTimeout(saveTimer));

  onMount(async () => {
    try {
      const raw = await ConfigService.Get();
      config = raw ? { ...raw } : {};
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

<div class="settings-page page-container">
  <h2 class="page-title">{t(lang, 'settings.title')}</h2>

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
            <SettingsToggle label={t(lang, 'settings.autoUpdateCheck')} hint={t(lang, 'settings.autoUpdateCheckHint')} active={config.AutoUpdateCheck} onToggle={() => toggleField('AutoUpdateCheck')} />
            <SettingsToggle label={t(lang, 'settings.autoUpdateApp')} hint={t(lang, 'settings.autoUpdateAppHint')} active={config.AutoUpdateApp} onToggle={() => toggleField('AutoUpdateApp')} />
            <SettingsToggle label={t(lang, 'settings.autoUpdateTools')} hint={t(lang, 'settings.autoUpdateToolsHint')} active={config.AutoUpdateTools} onToggle={() => toggleField('AutoUpdateTools')} />
          </div>
        </section>

        <!-- Section 2: Decompilation -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.decompilation')}</h3>
          <div class="card-rows">
            <SettingsToggle label={t(lang, 'settings.decompileProject')} hint={t(lang, 'settings.decompileProjectHint')} active={config.DecompileProjectMode} onToggle={() => toggleField('DecompileProjectMode')} />
            <SettingsToggle label={t(lang, 'settings.generatePdb')} hint={t(lang, 'settings.generatePdbHint')} active={config.GeneratePDB} onToggle={() => toggleField('GeneratePDB')} />
            <div class="setting-row">
              <span class="setting-label">{t(lang, 'settings.csharpVersion')}</span>
              <select class="setting-select" bind:value={config.CSharpLanguageVersion} onchange={onConfigChange}>
                <option value="Auto">Auto</option>
                <option value="Latest">Latest</option>
                <option value="11">11</option>
                <option value="10">10</option>
                <option value="9">9</option>
              </select>
            </div>
            <SettingsToggle label={t(lang, 'settings.keepIntermediates')} hint={t(lang, 'settings.keepIntermediatesHint')} active={config.KeepIntermediates} onToggle={() => toggleField('KeepIntermediates')} />
            <SettingsToggle label={t(lang, 'settings.skipSystemLibs')} hint={t(lang, 'settings.skipSystemLibsHint')} active={config.SkipSystemLibs} onToggle={() => toggleField('SkipSystemLibs')} />
            <SettingsToggle label={t(lang, 'settings.stopOnFirstError')} hint={t(lang, 'settings.stopOnFirstErrorHint')} active={config.StopOnFirstError} onToggle={() => toggleField('StopOnFirstError')} />
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
            <SettingsToggle label={t(lang, 'settings.allowDynamicExecution')} hint={t(lang, 'settings.allowDynamicExecutionHint')} active={config.AllowDynamicExecution} onToggle={() => toggleField('AllowDynamicExecution')} />
            <SettingsToggle label={t(lang, 'settings.sandboxWarning')} hint={t(lang, 'settings.sandboxWarningHint')} active={config.SandboxWarning} onToggle={() => toggleField('SandboxWarning')} />
          </div>
        </section>

        <!-- Section: Unreal Engine -->
        <section class="settings-card glass">
          <h3 class="card-title">{t(lang, 'settings.unrealEngine')}</h3>
          <div class="card-rows">
            <SettingsToggle label={t(lang, 'settings.ue5.extractPak')} hint={t(lang, 'settings.ue5.extractPakHint')} active={config.UE5ExtractPAK} onToggle={() => toggleField('UE5ExtractPAK')} />
            <SettingsToggle label={t(lang, 'settings.ue5.sdkDump')} hint={t(lang, 'settings.ue5.sdkDumpHint')} active={config.UE5SDKDump} onToggle={() => toggleField('UE5SDKDump')} />
            <SettingsToggle label={t(lang, 'settings.ue5.extractStrings')} hint={t(lang, 'settings.ue5.extractStringsHint')} active={config.UE5ExtractStrings} onToggle={() => toggleField('UE5ExtractStrings')} />
            <SettingsToggle label={t(lang, 'settings.ue5.ghidraDecompile')} hint={t(lang, 'settings.ue5.ghidraDecompileHint')} active={config.UE5GhidraDecompile} onToggle={() => toggleField('UE5GhidraDecompile')} />
            <SettingsToggle label={t(lang, 'settings.ue5.nameResolution')} hint={t(lang, 'settings.ue5.nameResolutionHint')} active={config.UE5NameResolution} onToggle={() => toggleField('UE5NameResolution')} />
            <SettingsToggle label={t(lang, 'settings.ue5.buildIndexes')} hint={t(lang, 'settings.ue5.buildIndexesHint')} active={config.UE5BuildIndexes} onToggle={() => toggleField('UE5BuildIndexes')} />
            <SettingsToggle label={t(lang, 'settings.ue5.exportHookable')} hint={t(lang, 'settings.ue5.exportHookableHint')} active={config.UE5ExportHookable} onToggle={() => toggleField('UE5ExportHookable')} />
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
            <SettingsToggle label={t(lang, 'settings.logToFile')} hint={t(lang, 'settings.logToFileHint')} active={config.LogToFile} onToggle={() => toggleField('LogToFile')} />
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

</style>
