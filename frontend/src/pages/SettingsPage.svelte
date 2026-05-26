<script lang="ts">
  import { onMount } from 'svelte';
  import { ConfigService } from '../lib/api';

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
</script>

<div class="settings-page">
  <h2 class="settings-title">Settings</h2>

  {#if loading}
    <div class="settings-loading">Loading configuration...</div>
  {:else}
    <div class="settings-content">
      <!-- Pipeline section -->
      <section class="settings-section">
        <h3 class="section-title">Pipeline</h3>

        <label class="setting-row">
          <span class="setting-label">Default output directory</span>
          <input
            class="setting-input"
            type="text"
            bind:value={config.default_output_dir}
            on:input={onConfigChange}
            placeholder="./decompiled"
          />
        </label>

        <label class="setting-row">
          <span class="setting-label">Step timeout (minutes)</span>
          <input
            class="setting-input setting-input-sm"
            type="number"
            bind:value={config.step_timeout_minutes}
            on:input={onConfigChange}
            min="1"
          />
        </label>

        <label class="setting-row">
          <span class="setting-label">Concurrent targets</span>
          <input
            class="setting-input setting-input-sm"
            type="number"
            bind:value={config.concurrent_targets}
            on:input={onConfigChange}
            min="1" max="8"
          />
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.stop_on_first_error} on:change={onConfigChange} />
          <span class="toggle-label">Stop on first error</span>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.keep_intermediates} on:change={onConfigChange} />
          <span class="toggle-label">Keep intermediate files</span>
        </label>
      </section>

      <!-- Skip-list section -->
      <section class="settings-section">
        <h3 class="section-title">Skip List</h3>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.skip_system_libs} on:change={onConfigChange} />
          <span class="toggle-label">Skip system libraries</span>
        </label>
      </section>

      <!-- Decompiler section -->
      <section class="settings-section">
        <h3 class="section-title">Decompiler</h3>

        <label class="setting-row">
          <span class="setting-label">C# language version</span>
          <input
            class="setting-input setting-input-sm"
            type="text"
            bind:value={config.csharp_language_version}
            on:input={onConfigChange}
            placeholder="Latest"
          />
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.generate_pdb} on:change={onConfigChange} />
          <span class="toggle-label">Generate PDB files</span>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.decompile_project_mode} on:change={onConfigChange} />
          <span class="toggle-label">Decompile as project</span>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.generate_callgraph} on:change={onConfigChange} />
          <span class="toggle-label">Generate call graph</span>
        </label>
      </section>

      <!-- Network section -->
      <section class="settings-section">
        <h3 class="section-title">Network</h3>

        <label class="setting-row">
          <span class="setting-label">GitHub token</span>
          <input
            class="setting-input"
            type="password"
            bind:value={config.github_token}
            on:input={onConfigChange}
            placeholder="ghp_..."
          />
        </label>

        <label class="setting-row">
          <span class="setting-label">Download retries</span>
          <input
            class="setting-input setting-input-sm"
            type="number"
            bind:value={config.download_retries}
            on:input={onConfigChange}
            min="1"
          />
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.auto_update_check} on:change={onConfigChange} />
          <span class="toggle-label">Auto-check for updates</span>
        </label>
      </section>

      <!-- Logging section -->
      <section class="settings-section">
        <h3 class="section-title">Logging</h3>

        <label class="setting-row">
          <span class="setting-label">Log level</span>
          <select class="setting-select" bind:value={config.log_level} on:change={onConfigChange}>
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
          </select>
        </label>

        <label class="setting-toggle">
          <input type="checkbox" bind:checked={config.log_to_file} on:change={onConfigChange} />
          <span class="toggle-label">Log to file</span>
        </label>
      </section>
    </div>

    {#if saving}
      <div class="save-indicator">Saving...</div>
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
</style>
