<script lang="ts">
  export let items: Array<{
    path: string;
    group: string;
    kind?: string;
    obfuscator?: string;
    recipe?: string;
    selected: boolean;
    skipped?: boolean;
    skipReason?: string;
  }> = [];

  function toggleItem(index: number) {
    if (!items[index].skipped) {
      items[index].selected = !items[index].selected;
      items = items; // trigger reactivity
    }
  }

  function selectAll() {
    items = items.map(i => ({ ...i, selected: !i.skipped }));
  }

  function deselectAll() {
    items = items.map(i => ({ ...i, selected: false }));
  }

  function basename(path: string): string {
    return path.split(/[/\\]/).pop() || path;
  }

  $: selectedCount = items.filter(i => i.selected).length;
  $: totalCount = items.length;
</script>

<div class="file-tree">
  <div class="tree-toolbar">
    <span class="tree-count">{selectedCount}/{totalCount} selected</span>
    <div class="tree-actions">
      <button class="tree-btn" on:click={selectAll}>All</button>
      <button class="tree-btn" on:click={deselectAll}>None</button>
    </div>
  </div>

  <div class="tree-list">
    {#each items as item, i}
      <button
        class="tree-item"
        class:selected={item.selected}
        class:skipped={item.skipped}
        on:click={() => toggleItem(i)}
      >
        <span class="tree-check">
          {#if item.skipped}
            <span class="check-skip">-</span>
          {:else if item.selected}
            <span class="check-on">[x]</span>
          {:else}
            <span class="check-off">[ ]</span>
          {/if}
        </span>

        <span class="tree-name">{basename(item.path)}</span>

        <span class="tree-tags">
          {#if item.group}
            <span class="tag tag-group">{item.group}</span>
          {/if}
          {#if item.kind}
            <span class="tag tag-kind">{item.kind}</span>
          {/if}
          {#if item.obfuscator}
            <span class="tag tag-obfuscator">{item.obfuscator}</span>
          {/if}
          {#if item.recipe}
            <span class="tag tag-recipe">{item.recipe}</span>
          {/if}
          {#if item.skipped && item.skipReason}
            <span class="tag tag-skip">{item.skipReason}</span>
          {/if}
        </span>
      </button>
    {/each}
  </div>
</div>

<style>
  .file-tree {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
  }
  .tree-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 0;
    border-bottom: 1px solid var(--border-subtle);
  }
  .tree-count {
    font-size: 12px;
    color: var(--text-muted);
    font-family: ui-monospace, monospace;
  }
  .tree-actions {
    display: flex;
    gap: 6px;
  }
  .tree-btn {
    all: unset;
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.15s;
  }
  .tree-btn:hover {
    border-color: var(--border-hover);
    color: var(--text-primary);
  }
  .tree-list {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .tree-item {
    all: unset;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 12px;
    transition: background 0.1s;
  }
  .tree-item:hover {
    background: var(--accent-dim);
  }
  .tree-item.skipped {
    opacity: 0.4;
    cursor: default;
  }
  .tree-check {
    font-family: ui-monospace, monospace;
    font-size: 12px;
    flex-shrink: 0;
    width: 24px;
  }
  .check-on { color: var(--accent); }
  .check-off { color: var(--text-muted); }
  .check-skip { color: var(--text-muted); }
  .tree-name {
    color: var(--text-primary);
    flex-shrink: 0;
  }
  .tree-item.skipped .tree-name {
    color: var(--text-muted);
  }
  .tree-tags {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
    margin-left: auto;
  }
  .tag {
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 3px;
    font-family: ui-monospace, monospace;
    white-space: nowrap;
  }
  .tag-group {
    background: var(--accent-dim);
    color: var(--accent);
    border: 1px solid var(--border);
  }
  .tag-kind {
    background: var(--accent-purple-dim);
    color: var(--accent-purple);
    border: 1px solid rgba(191, 95, 255, 0.2);
  }
  .tag-obfuscator {
    background: rgba(255, 51, 102, 0.1);
    color: var(--error);
    border: 1px solid rgba(255, 51, 102, 0.2);
  }
  .tag-recipe {
    background: rgba(0, 191, 255, 0.1);
    color: var(--info);
    border: 1px solid rgba(0, 191, 255, 0.2);
  }
  .tag-skip {
    background: rgba(80, 80, 96, 0.2);
    color: var(--text-muted);
    border: 1px solid rgba(80, 80, 96, 0.3);
  }
</style>
