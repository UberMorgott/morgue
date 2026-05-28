<script lang="ts">
  import { t, type Lang } from '../lib/i18n';

  let { lang, groups, obfuscations }: {
    lang: Lang;
    groups: Array<{ kind: string; language: string; count: number; examples: string[] }>;
    obfuscations: Array<{ name: string; deobfuscator: string | null; affectedFiles: string[] }>;
  } = $props();

  function issueUrl(name: string): string {
    return `https://github.com/UberMorgott/morgue/issues/new?title=Deobfuscator+request:+${encodeURIComponent(name)}&labels=enhancement`;
  }
</script>

<div class="composition-panel glass neon-border animate-in">
  <h3 class="panel-title">&#x2B21; {t(lang, 'composition.title')}</h3>

  <!-- Groups section -->
  <div class="groups">
    {#each groups as g (g.kind)}
      <div class="group-row">
        <span class="group-count">{g.count}</span>
        <span class="group-kind">{g.kind}</span>
        <span class="lang-badge">{g.language}</span>
        <span class="group-examples">
          {g.examples.slice(0, 2).join(', ')}
          {#if g.count > 2}
            <span class="more-hint">{t(lang, 'composition.andMore').replace('{n}', String(g.count - 2))}</span>
          {/if}
        </span>
      </div>
    {/each}
  </div>

  <!-- Obfuscation section -->
  {#if obfuscations.length > 0}
    <div class="obfuscation-section">
      {#each obfuscations as ob (ob.name)}
        {#if ob.deobfuscator}
          <div class="obf-block obf-warning">
            <svg class="obf-icon" width="16" height="16" viewBox="0 0 16 16">
              <path d="M8 1L1 14h14L8 1z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/>
              <path d="M8 6v4M8 11.5v.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
            </svg>
            <div class="obf-text">
              <strong>{ob.name}</strong>
              <span class="obf-detail">&rarr; {ob.deobfuscator} {t(lang, 'composition.autoApply')} &middot; {ob.affectedFiles.length} {t(lang, 'composition.filesAffected')}</span>
            </div>
          </div>
        {:else}
          <div class="obf-block obf-error">
            <svg class="obf-icon" width="16" height="16" viewBox="0 0 16 16">
              <circle cx="8" cy="8" r="6.5" fill="none" stroke="currentColor" stroke-width="1.5"/>
              <path d="M8 4.5v4M8 10.5v.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/>
            </svg>
            <div class="obf-text">
              <strong>{ob.name}</strong>
              <span class="obf-detail">{t(lang, 'composition.noDeobfuscator')}</span>
              <a class="obf-request" href={issueUrl(ob.name)} target="_blank" rel="noopener">
                &#x1F4DD; {t(lang, 'composition.requestSupport').replace('{name}', ob.name)}
              </a>
            </div>
          </div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .composition-panel {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 18px 20px;
    border-radius: var(--radius);
  }

  .panel-title {
    font-family: 'Orbitron', 'Play', sans-serif;
    font-size: 0.88rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1.5px;
    color: var(--text-heading);
    margin: 0;
  }

  /* Groups */
  .groups {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .group-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 0;
  }

  .group-count {
    font-family: 'Orbitron', monospace;
    font-size: 14px;
    font-weight: 700;
    color: var(--accent);
    min-width: 28px;
    text-align: right;
  }

  .group-kind {
    font-size: 0.84rem;
    font-weight: 600;
    color: var(--text-primary);
  }

  .lang-badge {
    display: inline-flex;
    align-items: center;
    padding: 2px 8px;
    font-size: 0.72rem;
    font-weight: 500;
    border-radius: 6px;
    background: var(--info-dim);
    color: var(--info);
    border: 1px solid rgba(85, 187, 255, 0.2);
    letter-spacing: 0.3px;
  }

  .group-examples {
    font-size: 0.78rem;
    font-family: 'Consolas', 'Courier New', monospace;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  .more-hint {
    color: var(--text-muted);
    opacity: 0.7;
    font-style: italic;
  }

  /* Obfuscation section */
  .obfuscation-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 14px;
    border-top: 1px solid var(--border-subtle);
  }

  .obf-block {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 14px;
    border-radius: var(--radius-sm);
  }

  .obf-warning {
    background: var(--warning-dim);
    border: 1px solid rgba(255, 204, 68, 0.2);
    color: var(--warning);
  }

  .obf-error {
    background: var(--error-dim);
    border: 1px solid rgba(255, 68, 102, 0.2);
    color: var(--error);
  }

  .obf-icon {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .obf-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 0.82rem;
  }

  .obf-text strong {
    font-weight: 600;
  }

  .obf-detail {
    font-size: 0.78rem;
    opacity: 0.85;
  }

  .obf-request {
    font-size: 0.78rem;
    color: var(--info);
    text-decoration: none;
    margin-top: 2px;
    cursor: pointer;
  }
  .obf-request:hover {
    text-decoration: underline;
    color: var(--accent-warm);
  }
</style>
