# Morgue UX Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign Morgue frontend to a reactive single-page pipeline flow with global StatusBar progress, enhanced Tools page with version/update checking, and native directory picker in Settings.

**Architecture:** Replace current multi-page flow (Home→Scan→Jobs) with Home→PipelinePage single reactive view. Enhance ToolsService with GitHub version checking. StatusBar becomes a reactive progress display for all background operations. All new text gets en/ru i18n.

**Tech Stack:** Svelte 4, Tailwind CSS, Go (Wails v3 services), @wailsio/runtime Events

---

### Task 1: Extend Go ToolStatus with LatestVersion and UpdateAvailable

**Files:**
- Modify: `internal/tools/types.go`
- Modify: `internal/tools/manager.go`
- Modify: `internal/tools/github.go`

- [ ] **Step 1: Add fields to ToolStatus**

In `internal/tools/types.go`, replace `ToolStatus`:

```go
// ToolStatus holds the installed state of a tool.
type ToolStatus struct {
	Name            string `json:"Name"`
	Installed       bool   `json:"Installed"`
	Path            string `json:"Path"`
	Version         string `json:"Version"`
	LatestVersion   string `json:"LatestVersion"`
	UpdateAvailable bool   `json:"UpdateAvailable"`
	Category        string `json:"Category"`
	Description     string `json:"Description"`
	Optional        bool   `json:"Optional"`
}
```

- [ ] **Step 2: Add CheckAllWithUpdates to Manager**

In `internal/tools/manager.go`, add:

```go
// CheckAllWithUpdates returns status of all tools including latest GitHub versions.
func (m *Manager) CheckAllWithUpdates() []ToolStatus {
	statuses := make([]ToolStatus, 0, len(Registry))
	for _, t := range Registry {
		st := m.Check(t.Name)
		st.Category = t.Category.String()
		st.Description = t.Description
		st.Optional = t.Optional

		if t.Method == MethodGitHubRelease && t.Repo != "" {
			release, err := fetchLatestRelease(t.Repo, os.Getenv("GITHUB_TOKEN"))
			if err == nil {
				st.LatestVersion = release.TagName
				if st.Installed && st.Version != "" && st.Version != release.TagName {
					st.UpdateAvailable = true
				}
			}
		}
		statuses = append(statuses, st)
	}
	return statuses
}

// Delete removes a tool's directory from disk.
func (m *Manager) Delete(name string) error {
	_, ok := FindByName(name)
	if !ok {
		return fmt.Errorf("unknown tool: %s", name)
	}
	return os.RemoveAll(filepath.Join(m.baseDir, name))
}
```

- [ ] **Step 3: Update Check() to populate new fields**

In `internal/tools/manager.go`, update `Check`:

```go
func (m *Manager) Check(name string) ToolStatus {
	tool, ok := FindByName(name)
	if !ok {
		return ToolStatus{Name: name}
	}

	path := filepath.Join(m.baseDir, name, tool.Binary)
	_, err := os.Stat(path)
	return ToolStatus{
		Name:        name,
		Installed:   err == nil,
		Path:        path,
		Category:    tool.Category.String(),
		Description: tool.Description,
		Optional:    tool.Optional,
	}
}
```

- [ ] **Step 4: Verify it compiles**

Run: `cd /d/MorgDEV/morgue && go build ./internal/tools/`
Expected: no errors

- [ ] **Step 5: Commit**

```bash
git add internal/tools/types.go internal/tools/manager.go
git commit -m "feat(tools): add LatestVersion, UpdateAvailable, Delete to ToolStatus/Manager"
```

---

### Task 2: Extend ToolsService and ReconService for frontend

**Files:**
- Modify: `internal/services/tools_service.go`
- Modify: `internal/services/recon_service.go`

- [ ] **Step 1: Add CheckAllWithUpdates, Delete, UpdateTool to ToolsService**

In `internal/services/tools_service.go`:

```go
// CheckAllWithUpdates returns tool statuses including latest versions from GitHub.
func (s *ToolsService) CheckAllWithUpdates() []tools.ToolStatus {
	return s.manager.CheckAllWithUpdates()
}

// Delete removes a tool from disk.
func (s *ToolsService) Delete(name string) error {
	return s.manager.Delete(name)
}
```

- [ ] **Step 2: Add PickFile to ReconService**

In `internal/services/recon_service.go`, add:

```go
// PickFile opens a native file picker dialog.
func (s *ReconService) PickFile() (string, error) {
	app := application.Get()
	if app == nil {
		return "", nil
	}
	return app.Dialog.OpenFile().
		CanChooseDirectories(false).
		CanChooseFiles(true).
		SetTitle("Select Binary File").
		PromptForSingleSelection()
}
```

- [ ] **Step 3: Add download progress emission to Install**

In `internal/services/tools_service.go`, update `Install`:

```go
func (s *ToolsService) Install(name string) error {
	if app := application.Get(); app != nil {
		app.Event.Emit("tool:download:start", map[string]string{"tool": name})
	}
	err := s.manager.Install(name)
	if app := application.Get(); app != nil {
		if err != nil {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": name, "error": err.Error()})
		} else {
			app.Event.Emit("tool:download:complete", map[string]interface{}{"tool": name, "error": nil})
		}
		app.Event.Emit("tool:installed", name)
	}
	return err
}
```

- [ ] **Step 4: Verify compilation and regenerate bindings**

```bash
cd /d/MorgDEV/morgue && go build ./internal/services/
wails3 generate bindings ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/services/ frontend/bindings/
git commit -m "feat(services): add CheckAllWithUpdates, Delete, PickFile, download events"
```

---

### Task 3: Add new i18n keys

**Files:**
- Modify: `frontend/src/lib/i18n.ts`

- [ ] **Step 1: Add pipeline view, tools page, and statusbar translations**

Add to `translations` object in `i18n.ts`:

```typescript
// Pipeline view
'pipeline.scanning': { en: 'Scanning...', ru: 'Сканирование...' },
'pipeline.foundBinaries': { en: 'binaries found', ru: 'бинарников найдено' },
'pipeline.recon': { en: 'Analyzing...', ru: 'Анализ...' },
'pipeline.checkingTools': { en: 'Checking tools...', ru: 'Проверка инструментов...' },
'pipeline.allToolsReady': { en: 'All tools ready', ru: 'Все инструменты готовы' },
'pipeline.missingTools': { en: 'Missing tools', ru: 'Недостающие инструменты' },
'pipeline.installMissing': { en: 'Install missing', ru: 'Установить недостающие' },
'pipeline.executing': { en: 'Decompiling...', ru: 'Декомпиляция...' },
'pipeline.step': { en: 'Step', ru: 'Шаг' },
'pipeline.done': { en: 'Done', ru: 'Готово' },
'pipeline.outputPath': { en: 'Output', ru: 'Результат' },
'pipeline.filesDecompiled': { en: 'files decompiled', ru: 'файлов декомпилировано' },
'pipeline.totalTime': { en: 'Total time', ru: 'Общее время' },
'pipeline.error': { en: 'Error', ru: 'Ошибка' },

// Home — open button
'home.openFolder': { en: 'Open folder', ru: 'Открыть папку' },
'home.openFile': { en: 'Open file', ru: 'Открыть файл' },

// Tools page enhanced
'tools.version': { en: 'Version', ru: 'Версия' },
'tools.latest': { en: 'Latest', ru: 'Последняя' },
'tools.updateAvailable': { en: 'Update available', ru: 'Доступно обновление' },
'tools.update': { en: 'Update', ru: 'Обновить' },
'tools.updateAll': { en: 'Update all', ru: 'Обновить все' },
'tools.downloadAll': { en: 'Download all', ru: 'Скачать все' },
'tools.delete': { en: 'Delete', ru: 'Удалить' },
'tools.download': { en: 'Download', ru: 'Скачать' },
'tools.notInstalled': { en: 'Not installed', ru: 'Не установлен' },
'tools.upToDate': { en: 'Up to date', ru: 'Актуален' },
'tools.checking': { en: 'Checking updates...', ru: 'Проверка обновлений...' },

// StatusBar enhanced
'status.downloading': { en: 'Downloading', ru: 'Скачивание' },
'status.installing': { en: 'Installing', ru: 'Установка' },

// Settings — folder picker
'settings.browse': { en: 'Browse...', ru: 'Обзор...' },
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/lib/i18n.ts
git commit -m "feat(i18n): add pipeline, tools, statusbar translations en/ru"
```

---

### Task 4: Redesign StatusBar with progress bar

**Files:**
- Modify: `frontend/src/components/StatusBar.svelte`

- [ ] **Step 1: Rewrite StatusBar**

Replace entire `StatusBar.svelte`:

```svelte
<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let phase: string = '';
  export let target: string = '';
  export let elapsed: string = '';
  export let progress: number = 0;
  export let progressLabel: string = '';
</script>

<footer class="status-bar">
  <div class="status-left">
    {#if phase}
      <span class="status-phase">{phase}</span>
      {#if progressLabel}
        <span class="status-label">{progressLabel}</span>
      {/if}
      {#if target}
        <span class="status-target">{target}</span>
      {/if}
    {:else}
      <span class="status-idle">{t(lang, 'status.ready')}</span>
    {/if}
  </div>

  {#if progress > 0 && progress < 100}
    <div class="status-progress">
      <div class="status-progress-fill" style="width: {progress}%"></div>
    </div>
  {/if}

  <div class="status-right">
    {#if progress > 0 && progress < 100}
      <span class="status-percent">{Math.round(progress)}%</span>
    {/if}
    {#if elapsed}
      <span class="status-elapsed">{elapsed}</span>
    {/if}
  </div>
</footer>

<style>
  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    height: 26px;
    padding: 0 12px;
    background: var(--bg-sidebar);
    border-top: 1px solid var(--border);
    font-size: 11px;
    font-family: ui-monospace, monospace;
    flex-shrink: 0;
    gap: 8px;
  }
  .status-left {
    display: flex;
    align-items: center;
    gap: 8px;
    overflow: hidden;
    flex-shrink: 0;
  }
  .status-phase {
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .status-label {
    color: var(--text-secondary);
  }
  .status-target {
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 300px;
  }
  .status-idle {
    color: var(--text-muted);
  }
  .status-progress {
    flex: 1;
    height: 4px;
    background: var(--border);
    border-radius: 2px;
    overflow: hidden;
    min-width: 60px;
  }
  .status-progress-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    transition: width 0.3s ease;
  }
  .status-right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .status-percent {
    color: var(--accent);
    font-weight: 600;
  }
  .status-elapsed {
    color: var(--text-muted);
  }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/StatusBar.svelte
git commit -m "feat(statusbar): add progress bar, percent, label support"
```

---

### Task 5: Redesign HomePage with Open buttons

**Files:**
- Modify: `frontend/src/pages/HomePage.svelte`

- [ ] **Step 1: Rewrite HomePage**

Replace entire `HomePage.svelte`:

```svelte
<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import DropZone from '../components/DropZone.svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  const dispatch = createEventDispatcher();

  function handleSelect(e: CustomEvent<{ path: string }>) {
    dispatch('navigate', { page: 'pipeline', path: e.detail.path });
  }

  function handleBrowseFolder() {
    dispatch('browse-folder');
  }

  function handleBrowseFile() {
    dispatch('browse-file');
  }
</script>

<div class="home-page">
  <div class="home-hero">
    <h1 class="hero-title">{t(lang, 'home.title')}</h1>
    <p class="hero-subtitle">{t(lang, 'home.subtitle')}</p>
  </div>

  <DropZone {lang} on:select={handleSelect} on:browse={handleBrowseFolder} />

  <div class="home-actions">
    <button class="open-btn" on:click={handleBrowseFolder}>
      <span class="open-icon">📁</span>
      {t(lang, 'home.openFolder')}
    </button>
    <button class="open-btn open-btn-secondary" on:click={handleBrowseFile}>
      <span class="open-icon">📄</span>
      {t(lang, 'home.openFile')}
    </button>
  </div>
</div>

<style>
  .home-page {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 24px;
    height: 100%;
    padding: 32px;
  }
  .home-hero { text-align: center; }
  .hero-title {
    font-size: 28px;
    font-weight: 700;
    color: var(--text-primary);
    margin: 0 0 8px 0;
    letter-spacing: -0.5px;
  }
  .hero-subtitle {
    font-size: 14px;
    color: var(--text-muted);
    margin: 0;
  }
  .home-actions {
    display: flex;
    gap: 12px;
  }
  .open-btn {
    all: unset;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    border-radius: 8px;
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    font-size: 13px;
    cursor: pointer;
    transition: all 0.15s;
  }
  .open-btn:hover { box-shadow: 0 0 16px var(--accent-dim); }
  .open-btn-secondary {
    background: transparent;
    border: 1px solid var(--accent);
    color: var(--accent);
  }
  .open-btn-secondary:hover {
    background: var(--accent-dim);
  }
  .open-icon { font-size: 16px; }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/HomePage.svelte
git commit -m "feat(home): add Open Folder/File buttons, remove stats cards"
```

---

### Task 6: Create PipelinePage (reactive single-page flow)

**Files:**
- Create: `frontend/src/pages/PipelinePage.svelte`

- [ ] **Step 1: Create PipelinePage.svelte**

```svelte
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { ReconService, PipelineService, ToolsService } from '../lib/api';
  import { onEvent } from '../lib/events';
  import ProgressBar from '../components/ProgressBar.svelte';
  import LogViewer from '../components/LogViewer.svelte';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';
  export let inputPath: string = '';

  type PipelineStep = 'scan' | 'recon' | 'tools' | 'execute' | 'done' | 'error';

  let currentStep: PipelineStep = 'scan';
  let scanResults: Array<{ path: string; group: string }> = [];
  let reconResults: Array<{ path: string; group: string; kind: string; obfuscator: string; recipe: string }> = [];
  let toolsNeeded: Array<{ name: string; installed: boolean; category: string }> = [];
  let missingTools: string[] = [];
  let installingMissing = false;

  let pipelineProgress = 0;
  let pipelineStepLabel = '';
  let pipelineTotal = 0;
  let pipelineCurrent = 0;
  let logEntries: Array<{ level: 'info' | 'warn' | 'error'; message: string }> = [];

  let outputPath = '';
  let totalTime = '';
  let filesCount = 0;
  let errorMessage = '';

  let cleanups: Array<() => void> = [];

  // Expose progress for parent StatusBar binding
  export let statusPhase = '';
  export let statusProgress = 0;
  export let statusLabel = '';

  onMount(async () => {
    cleanups.push(
      onEvent('pipeline:progress', (data: any) => {
        const d = data?.data?.[0] || data?.data || data;
        if (d.Progress || d.progress) {
          const p = d.Progress || d.progress;
          pipelineTotal = p.Total || p.total || 1;
          pipelineCurrent = p.Step || p.step || 0;
          pipelineProgress = ((pipelineCurrent + 1) / pipelineTotal) * 100;
          pipelineStepLabel = `${t(lang, 'pipeline.step')} ${pipelineCurrent + 1}/${pipelineTotal}: ${p.Name || p.name || ''}`;
          statusPhase = t(lang, 'pipeline.executing');
          statusProgress = pipelineProgress;
          statusLabel = pipelineStepLabel;
        }
        if (d.Done || d.done) {
          currentStep = 'done';
          pipelineProgress = 100;
          outputPath = d.Output || d.output || inputPath;
          filesCount = d.Files || d.files || 0;
          totalTime = d.Duration || d.duration || '';
          statusPhase = '';
          statusProgress = 0;
          statusLabel = '';
        }
        if (d.Error || d.error) {
          const err = d.Error || d.error;
          errorMessage = typeof err === 'string' ? err : err.message || JSON.stringify(err);
          logEntries = [...logEntries, { level: 'error', message: errorMessage }];
        }
      }),
      onEvent('pipeline:log', (data: any) => {
        const msg = typeof data === 'string' ? data : data?.data?.[0] || data?.message || '';
        if (msg) logEntries = [...logEntries, { level: 'info', message: msg }];
      }),
    );

    await runPipeline();
  });

  onDestroy(() => {
    cleanups.forEach(fn => fn());
  });

  async function runPipeline() {
    // Step 1: Scan
    currentStep = 'scan';
    statusPhase = t(lang, 'pipeline.scanning');
    statusProgress = 0;
    try {
      const targets = await ReconService.ScanDirectory(inputPath);
      scanResults = (targets || []).map((t: any) => ({
        path: t.path || t.Path,
        group: t.group || t.Group || '',
      }));
    } catch (e: any) {
      errorMessage = e.message || String(e);
      currentStep = 'error';
      statusPhase = '';
      return;
    }

    if (scanResults.length === 0) {
      errorMessage = 'No binaries found';
      currentStep = 'error';
      statusPhase = '';
      return;
    }

    // Step 2: Recon
    currentStep = 'recon';
    statusPhase = t(lang, 'pipeline.recon');
    reconResults = [];
    for (const target of scanResults) {
      try {
        const result = await ReconService.ClassifyFile(target.path);
        reconResults = [...reconResults, {
          path: target.path,
          group: target.group,
          kind: result?.Kind || result?.kind || '',
          obfuscator: result?.Obfuscator || result?.obfuscator || '',
          recipe: result?.Recipe || result?.recipe || '',
        }];
      } catch (e) {
        reconResults = [...reconResults, {
          path: target.path,
          group: target.group,
          kind: '?', obfuscator: '', recipe: '',
        }];
      }
    }

    // Step 3: Tools check
    currentStep = 'tools';
    statusPhase = t(lang, 'pipeline.checkingTools');
    try {
      const statuses = await ToolsService.CheckAll();
      toolsNeeded = (statuses || []).map((s: any) => ({
        name: s.Name || s.name,
        installed: s.Installed || s.installed || false,
        category: s.Category || s.category || '',
      }));
      missingTools = toolsNeeded.filter(t => !t.installed).map(t => t.name);
    } catch (e) {
      missingTools = [];
    }

    if (missingTools.length === 0) {
      await startExecution();
    }
    // else: wait for user to click "Install missing"
  }

  async function installMissingAndContinue() {
    installingMissing = true;
    statusPhase = t(lang, 'status.installing');
    try {
      await ToolsService.InstallAll();
      missingTools = [];
      installingMissing = false;
      await startExecution();
    } catch (e: any) {
      errorMessage = e.message || String(e);
      installingMissing = false;
    }
  }

  async function startExecution() {
    currentStep = 'execute';
    statusPhase = t(lang, 'pipeline.executing');
    logEntries = [];
    try {
      await PipelineService.Run(inputPath, '');
    } catch (e: any) {
      if (!errorMessage) {
        errorMessage = e.message || String(e);
        currentStep = 'error';
        statusPhase = '';
      }
    }
  }
</script>

<div class="pipeline-page">
  <!-- Step 1: Scan -->
  <section class="pipeline-step" class:collapsed={currentStep !== 'scan' && scanResults.length > 0}>
    <h3 class="step-title">
      <span class="step-num">1</span>
      {t(lang, 'scan.title')}
      {#if scanResults.length > 0}
        <span class="step-badge">{scanResults.length} {t(lang, 'pipeline.foundBinaries')}</span>
      {/if}
    </h3>
    {#if currentStep === 'scan'}
      <div class="step-loading"><span class="spinner"></span> {t(lang, 'pipeline.scanning')}</div>
    {:else if scanResults.length > 0 && currentStep === 'scan'}
      <!-- shown in collapsed badge -->
    {/if}
  </section>

  <!-- Step 2: Recon -->
  {#if currentStep !== 'scan' || scanResults.length > 0}
    <section class="pipeline-step" class:collapsed={currentStep !== 'recon' && reconResults.length > 0}>
      <h3 class="step-title">
        <span class="step-num">2</span>
        {t(lang, 'pipeline.recon')}
      </h3>
      {#if currentStep === 'recon'}
        <div class="step-content">
          {#each reconResults as r}
            <div class="recon-row">
              <span class="recon-path">{r.path.split(/[/\\]/).pop()}</span>
              <span class="recon-tag">{r.kind}</span>
              {#if r.obfuscator}<span class="recon-tag tag-obf">{r.obfuscator}</span>{/if}
            </div>
          {/each}
          {#if reconResults.length < scanResults.length}
            <div class="step-loading"><span class="spinner"></span> {t(lang, 'pipeline.recon')}</div>
          {/if}
        </div>
      {:else if reconResults.length > 0}
        <div class="step-summary">
          {#each reconResults as r}
            <span class="recon-tag-sm">{r.kind}{r.obfuscator ? ` / ${r.obfuscator}` : ''}</span>
          {/each}
        </div>
      {/if}
    </section>
  {/if}

  <!-- Step 3: Tools -->
  {#if currentStep === 'tools' || currentStep === 'execute' || currentStep === 'done'}
    <section class="pipeline-step" class:collapsed={currentStep !== 'tools'}>
      <h3 class="step-title">
        <span class="step-num">3</span>
        {t(lang, 'pipeline.checkingTools')}
        {#if missingTools.length === 0 && currentStep !== 'tools'}
          <span class="step-badge ok">✓ {t(lang, 'pipeline.allToolsReady')}</span>
        {:else if missingTools.length > 0}
          <span class="step-badge warn">⚠ {missingTools.length} {t(lang, 'pipeline.missingTools')}</span>
        {/if}
      </h3>
      {#if currentStep === 'tools' && missingTools.length > 0}
        <div class="step-content">
          <div class="tools-list-mini">
            {#each toolsNeeded as tool}
              <div class="tool-mini" class:tool-ok={tool.installed} class:tool-missing={!tool.installed}>
                <span>{tool.installed ? '✅' : '❌'}</span>
                <span>{tool.name}</span>
              </div>
            {/each}
          </div>
          <button class="install-btn" on:click={installMissingAndContinue} disabled={installingMissing}>
            {installingMissing ? t(lang, 'tools.installing') : t(lang, 'pipeline.installMissing')}
          </button>
        </div>
      {/if}
    </section>
  {/if}

  <!-- Step 4: Execute -->
  {#if currentStep === 'execute' || currentStep === 'done'}
    <section class="pipeline-step" class:collapsed={currentStep === 'done'}>
      <h3 class="step-title">
        <span class="step-num">4</span>
        {t(lang, 'pipeline.executing')}
      </h3>
      {#if currentStep === 'execute'}
        <div class="step-content">
          <ProgressBar percent={pipelineProgress} label={pipelineStepLabel} />
          <div class="pipeline-log">
            <LogViewer {lang} entries={logEntries} />
          </div>
        </div>
      {/if}
    </section>
  {/if}

  <!-- Step 5: Done -->
  {#if currentStep === 'done'}
    <section class="pipeline-step done-step">
      <h3 class="step-title">
        <span class="step-num">✓</span>
        {t(lang, 'pipeline.done')}
      </h3>
      <div class="step-content done-content">
        <div class="done-stat"><strong>{t(lang, 'pipeline.outputPath')}:</strong> <span class="selectable">{outputPath}</span></div>
        {#if filesCount > 0}
          <div class="done-stat">{filesCount} {t(lang, 'pipeline.filesDecompiled')}</div>
        {/if}
        {#if totalTime}
          <div class="done-stat">{t(lang, 'pipeline.totalTime')}: {totalTime}</div>
        {/if}
      </div>
    </section>
  {/if}

  <!-- Error -->
  {#if currentStep === 'error'}
    <section class="pipeline-step error-step">
      <h3 class="step-title error-title">
        <span class="step-num">!</span>
        {t(lang, 'pipeline.error')}
      </h3>
      <div class="step-content">
        <div class="error-msg">{errorMessage}</div>
      </div>
    </section>
  {/if}
</div>

<style>
  .pipeline-page {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    padding: 20px;
    overflow-y: auto;
  }
  .pipeline-step {
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    border-radius: 8px;
    padding: 14px 16px;
    transition: all 0.3s ease;
  }
  .pipeline-step.collapsed {
    padding: 8px 16px;
  }
  .pipeline-step.collapsed .step-content { display: none; }
  .step-title {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }
  .step-num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    background: var(--accent-dim);
    color: var(--accent);
    font-size: 11px;
    font-weight: 700;
    flex-shrink: 0;
  }
  .step-badge {
    font-size: 11px;
    font-weight: 500;
    color: var(--text-muted);
    margin-left: auto;
  }
  .step-badge.ok { color: var(--success, #22c55e); }
  .step-badge.warn { color: var(--warning, #eab308); }
  .step-content { margin-top: 12px; }
  .step-loading {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    font-size: 12px;
    padding: 8px 0;
  }
  .spinner {
    width: 14px; height: 14px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
  }
  .step-summary {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-top: 6px;
  }
  .recon-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 0;
    font-size: 12px;
  }
  .recon-path {
    color: var(--text-secondary);
    font-family: ui-monospace, monospace;
  }
  .recon-tag {
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--accent-dim);
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .recon-tag-sm {
    font-size: 10px;
    padding: 1px 5px;
    border-radius: 3px;
    background: var(--accent-dim);
    color: var(--accent);
  }
  .tag-obf { background: rgba(191, 95, 255, 0.15); color: #bf5fff; }
  .tools-list-mini {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 12px;
  }
  .tool-mini {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    padding: 4px 8px;
    border-radius: 4px;
  }
  .tool-ok { color: var(--text-secondary); }
  .tool-missing { color: var(--error); background: rgba(255, 51, 102, 0.08); }
  .install-btn {
    all: unset;
    font-size: 12px;
    padding: 8px 16px;
    border-radius: 6px;
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    cursor: pointer;
  }
  .install-btn:hover { box-shadow: 0 0 12px var(--accent-dim); }
  .install-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .pipeline-log { height: 180px; margin-top: 8px; }
  .done-step { border-color: var(--success, #22c55e); }
  .done-content {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary);
  }
  .error-step { border-color: var(--error); }
  .error-title { color: var(--error); }
  .error-msg {
    font-size: 12px;
    color: var(--error);
    font-family: ui-monospace, monospace;
    padding: 8px;
    background: rgba(255, 51, 102, 0.08);
    border-radius: 4px;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/PipelinePage.svelte
git commit -m "feat: add PipelinePage — reactive single-page decompilation flow"
```

---

### Task 7: Redesign ToolsPage with versions and updates

**Files:**
- Modify: `frontend/src/pages/ToolsPage.svelte`
- Modify: `frontend/src/components/ToolRow.svelte`

- [ ] **Step 1: Rewrite ToolRow.svelte**

Replace entire `ToolRow.svelte`:

```svelte
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
      <span class="ver-none">{t(lang, 'tools.notInstalled')}</span>
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
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 12px;
    border-radius: 6px;
    background: var(--bg-card);
    border: 1px solid var(--border-subtle);
    transition: all 0.15s;
  }
  .tool-row.dimmed { opacity: 0.5; }
  .tool-row:hover { border-color: var(--border); }
  .tool-info {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
  }
  .tool-name {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-primary);
    font-family: ui-monospace, monospace;
  }
  .tool-category {
    font-size: 9px;
    padding: 1px 5px;
    border-radius: 3px;
    background: var(--accent-dim);
    color: var(--accent);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .tool-desc {
    font-size: 11px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tool-version {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 11px;
    font-family: ui-monospace, monospace;
    flex-shrink: 0;
    min-width: 120px;
  }
  .ver-current { color: var(--text-secondary); }
  .ver-sep { color: var(--text-muted); }
  .ver-latest { color: var(--text-muted); }
  .ver-new { color: var(--accent); font-weight: 600; }
  .ver-none { color: var(--text-muted); font-style: italic; }
  .tool-actions {
    display: flex;
    gap: 6px;
    flex-shrink: 0;
  }
  .action-btn {
    all: unset;
    font-size: 11px;
    padding: 4px 10px;
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.15s;
  }
  .action-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .action-download {
    border: 1px solid var(--accent);
    color: var(--accent);
  }
  .action-download:hover:not(:disabled) { background: var(--accent-dim); }
  .action-update {
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
  }
  .action-update:hover:not(:disabled) { box-shadow: 0 0 8px var(--accent-dim); }
  .action-delete {
    border: 1px solid var(--error);
    color: var(--error);
  }
  .action-delete:hover:not(:disabled) { background: rgba(255, 51, 102, 0.1); }
  .up-to-date {
    font-size: 10px;
    color: var(--text-muted);
  }
</style>
```

- [ ] **Step 2: Rewrite ToolsPage.svelte**

Replace entire `ToolsPage.svelte`:

```svelte
<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import ToolRow from '../components/ToolRow.svelte';
  import { ToolsService } from '../lib/api';
  import { t, type Lang } from '../lib/i18n';

  export let lang: Lang = 'en';

  // Expose for parent StatusBar
  export let statusPhase = '';
  export let statusProgress = 0;
  export let statusLabel = '';

  const dispatch = createEventDispatcher();

  let tools: Array<{
    name: string; installed: boolean; path: string; version: string;
    latestVersion: string; updateAvailable: boolean; category: string;
    description: string;
  }> = [];
  let loading = true;
  let busy = false;

  onMount(async () => {
    await loadTools();
  });

  async function loadTools() {
    loading = true;
    try {
      const statuses = await ToolsService.CheckAllWithUpdates();
      tools = (statuses || []).map((s: any) => ({
        name: s.Name || s.name,
        installed: s.Installed || s.installed || false,
        path: s.Path || s.path || '',
        version: s.Version || s.version || '',
        latestVersion: s.LatestVersion || s.latestVersion || '',
        updateAvailable: s.UpdateAvailable || s.updateAvailable || false,
        category: s.Category || s.category || '',
        description: s.Description || s.description || '',
      }));
    } catch (e) {
      console.error('CheckAllWithUpdates failed:', e);
    } finally {
      loading = false;
    }
  }

  async function installTool(e: CustomEvent<{ name: string }>) {
    busy = true;
    statusPhase = t(lang, 'status.downloading');
    statusLabel = e.detail.name;
    try {
      await ToolsService.Install(e.detail.name);
      await loadTools();
    } catch (e: any) {
      console.error('Install failed:', e);
    } finally {
      busy = false;
      statusPhase = '';
      statusLabel = '';
    }
  }

  async function deleteTool(e: CustomEvent<{ name: string }>) {
    busy = true;
    try {
      await ToolsService.Delete(e.detail.name);
      await loadTools();
    } catch (e: any) {
      console.error('Delete failed:', e);
    } finally {
      busy = false;
    }
  }

  async function downloadAll() {
    busy = true;
    const missing = tools.filter(t => !t.installed);
    for (let i = 0; i < missing.length; i++) {
      statusPhase = t(lang, 'status.downloading');
      statusLabel = `${missing[i].name} (${i + 1}/${missing.length})`;
      statusProgress = ((i) / missing.length) * 100;
      try {
        await ToolsService.Install(missing[i].name);
      } catch (e) {
        console.error('Install failed:', missing[i].name, e);
      }
    }
    statusPhase = '';
    statusProgress = 0;
    statusLabel = '';
    busy = false;
    await loadTools();
  }

  async function updateAll() {
    busy = true;
    const outdated = tools.filter(t => t.updateAvailable);
    for (let i = 0; i < outdated.length; i++) {
      statusPhase = t(lang, 'status.downloading');
      statusLabel = `${outdated[i].name} (${i + 1}/${outdated.length})`;
      statusProgress = ((i) / outdated.length) * 100;
      try {
        await ToolsService.Install(outdated[i].name);
      } catch (e) {
        console.error('Update failed:', outdated[i].name, e);
      }
    }
    statusPhase = '';
    statusProgress = 0;
    statusLabel = '';
    busy = false;
    await loadTools();
  }

  $: missingCount = tools.filter(t => !t.installed).length;
  $: outdatedCount = tools.filter(t => t.updateAvailable).length;
</script>

<div class="tools-page">
  <div class="tools-header">
    <h2 class="tools-title">{t(lang, 'tools.title')}</h2>
    <div class="tools-actions">
      {#if missingCount > 0}
        <button class="header-btn" on:click={downloadAll} disabled={busy}>
          {t(lang, 'tools.downloadAll')} ({missingCount})
        </button>
      {/if}
      {#if outdatedCount > 0}
        <button class="header-btn header-btn-accent" on:click={updateAll} disabled={busy}>
          {t(lang, 'tools.updateAll')} ({outdatedCount})
        </button>
      {/if}
    </div>
  </div>

  {#if loading}
    <div class="tools-loading">{t(lang, 'tools.checking')}</div>
  {:else}
    <div class="tools-list">
      {#each tools as tool}
        <ToolRow
          {lang}
          name={tool.name}
          installed={tool.installed}
          version={tool.version}
          latestVersion={tool.latestVersion}
          updateAvailable={tool.updateAvailable}
          category={tool.category}
          description={tool.description}
          {busy}
          on:install={installTool}
          on:delete={deleteTool}
        />
      {/each}
    </div>
  {/if}
</div>

<style>
  .tools-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    padding: 20px;
    gap: 16px;
  }
  .tools-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-shrink: 0;
  }
  .tools-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }
  .tools-actions {
    display: flex;
    gap: 8px;
  }
  .header-btn {
    all: unset;
    font-size: 12px;
    padding: 6px 14px;
    border-radius: 6px;
    border: 1px solid var(--accent);
    color: var(--accent);
    cursor: pointer;
    transition: all 0.15s;
  }
  .header-btn:hover:not(:disabled) { background: var(--accent-dim); }
  .header-btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .header-btn-accent {
    background: var(--accent);
    color: var(--bg-page);
    font-weight: 600;
    border: none;
  }
  .header-btn-accent:hover:not(:disabled) { box-shadow: 0 0 12px var(--accent-dim); }
  .tools-list {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .tools-loading {
    color: var(--text-muted);
    padding: 24px;
    text-align: center;
  }
</style>
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/ToolsPage.svelte frontend/src/components/ToolRow.svelte
git commit -m "feat(tools): redesign with versions, updates, download/delete actions"
```

---

### Task 8: Update SettingsPage with folder picker for output dir

**Files:**
- Modify: `frontend/src/pages/SettingsPage.svelte`

- [ ] **Step 1: Add folder picker to output dir**

In `SettingsPage.svelte`, replace the output dir setting row:

```svelte
<!-- Replace the outputDir label/input block -->
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
```

Add the `pickOutputDir` function and import in the `<script>` block:

```typescript
import { ConfigService, ReconService } from '../lib/api';

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
```

Add CSS for `.setting-with-browse` and `.browse-btn`:

```css
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
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/SettingsPage.svelte
git commit -m "feat(settings): add native folder picker for output directory"
```

---

### Task 9: Wire App.svelte to new pages and StatusBar

**Files:**
- Modify: `frontend/src/App.svelte`

- [ ] **Step 1: Rewrite App.svelte**

Replace entire `App.svelte`:

```svelte
<script lang="ts">
  import Header from './components/Header.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import StatusBar from './components/StatusBar.svelte';
  import HomePage from './pages/HomePage.svelte';
  import PipelinePage from './pages/PipelinePage.svelte';
  import ToolsPage from './pages/ToolsPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { ReconService } from './lib/api';
  import { currentLang } from './lib/stores';
  import type { Lang } from './lib/i18n';

  let currentPage = 'home';
  let pipelineInputPath = '';

  // Global StatusBar state
  let statusPhase = '';
  let statusTarget = '';
  let statusElapsed = '';
  let statusProgress = 0;
  let statusLabel = '';

  let lang: Lang;
  currentLang.subscribe(v => lang = v);

  function handleNavigate(e: CustomEvent<{ page: string; path?: string }>) {
    currentPage = e.detail.page;
    if (e.detail.path) {
      pipelineInputPath = e.detail.path;
    }
  }

  async function handleBrowseFolder() {
    try {
      const dir = await ReconService.PickDirectory();
      if (dir) {
        pipelineInputPath = dir;
        currentPage = 'pipeline';
      }
    } catch (e) {
      console.error('PickDirectory failed:', e);
    }
  }

  async function handleBrowseFile() {
    try {
      const file = await ReconService.PickFile();
      if (file) {
        pipelineInputPath = file;
        currentPage = 'pipeline';
      }
    } catch (e) {
      console.error('PickFile failed:', e);
    }
  }
</script>

<div class="app-layout">
  <Header {lang} />
  <div class="main-area">
    <Sidebar bind:currentPage {lang} />
    <div class="page-content">
      {#if currentPage === 'home'}
        <HomePage {lang} on:navigate={handleNavigate} on:browse-folder={handleBrowseFolder} on:browse-file={handleBrowseFile} />
      {:else if currentPage === 'pipeline'}
        <PipelinePage
          {lang}
          inputPath={pipelineInputPath}
          bind:statusPhase
          bind:statusProgress
          bind:statusLabel
        />
      {:else if currentPage === 'tools'}
        <ToolsPage
          {lang}
          bind:statusPhase
          bind:statusProgress
          bind:statusLabel
        />
      {:else if currentPage === 'settings'}
        <SettingsPage {lang} />
      {/if}
    </div>
  </div>
  <StatusBar
    {lang}
    phase={statusPhase}
    target={statusTarget}
    elapsed={statusElapsed}
    progress={statusProgress}
    progressLabel={statusLabel}
  />
</div>

<style>
  .app-layout {
    display: flex;
    flex-direction: column;
    width: 100%;
    height: 100%;
    overflow: hidden;
  }
  .main-area {
    display: flex;
    flex: 1;
    overflow: hidden;
  }
  .page-content {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
</style>
```

- [ ] **Step 2: Update Sidebar to show Pipeline instead of Scan/Jobs**

In `Sidebar.svelte`, replace `scan` and `jobs` nav items with `pipeline`:

Replace the sidebar items data (find the buttons for scan and jobs, replace with pipeline):
- Remove `scan` button
- Remove `jobs` button  
- Keep: `home`, `pipeline` (new), `tools`, `settings`

Update sidebar button for pipeline:
```svelte
<button class="nav-btn" class:active={currentPage === 'pipeline'} on:click={() => currentPage = 'pipeline'}>
  <span class="nav-icon">▶</span>
  <span class="nav-label">{t(lang, 'sidebar.pipeline')}</span>
</button>
```

- [ ] **Step 3: Add sidebar.pipeline i18n key**

In `i18n.ts`, add:
```typescript
'sidebar.pipeline': { en: 'Pipeline', ru: 'Pipeline' },
```

- [ ] **Step 4: Regenerate bindings (PickFile added)**

```bash
cd /d/MorgDEV/morgue && wails3 generate bindings ./...
```

- [ ] **Step 5: Commit**

```bash
git add frontend/src/App.svelte frontend/src/components/Sidebar.svelte frontend/src/lib/i18n.ts frontend/bindings/
git commit -m "feat: wire App.svelte to PipelinePage, updated Sidebar, global StatusBar"
```

---

### Task 10: Delete unused pages and verify build

**Files:**
- Delete: `frontend/src/pages/ScanPage.svelte`
- Delete: `frontend/src/pages/JobsPage.svelte`

- [ ] **Step 1: Remove unused pages**

```bash
rm frontend/src/pages/ScanPage.svelte
rm frontend/src/pages/JobsPage.svelte
```

- [ ] **Step 2: Build frontend**

```bash
cd /d/MorgDEV/morgue/frontend && npm run build
```

Expected: no errors, clean build.

- [ ] **Step 3: Build full Go binary**

```bash
cd /d/MorgDEV/morgue && go build -o morgue-test.exe ./cmd/morgue
```

Expected: no errors.

- [ ] **Step 4: Test in Playwright**

```bash
cd /d/MorgDEV/morgue/frontend && npx vite --port 34115
```

Navigate to `http://localhost:34115` via Playwright, verify:
- Home page shows DropZone + Open Folder + Open File buttons
- Sidebar has: Home, Pipeline, Tools, Settings (no Scan, no Jobs)
- Tools page shows tool list (dimmed/bright)
- Settings page has folder picker for output dir
- StatusBar shows "Ready"

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "chore: remove ScanPage/JobsPage, verify clean build"
```
