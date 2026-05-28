<script lang="ts">
  let { value = 0, size = 36, variant = 'accent' as 'accent' | 'success' | 'error', label = '' }: {
    value?: number;
    size?: number;
    variant?: 'accent' | 'success' | 'error';
    label?: string;
  } = $props();

  const colors: Record<string, string> = {
    accent: '#ff8a20',
    success: '#55eea0',
    error: '#ff4466',
  };

  let strokeWidth = $derived(Math.max(2, size * 0.1));
  let radius = $derived((size - strokeWidth) / 2);
  let circumference = $derived(2 * Math.PI * radius);
  let offset = $derived(value >= 0 ? circumference - (value / 100) * circumference : circumference * 0.75);
  let color = $derived(colors[variant] || colors.accent);
  let isIndeterminate = $derived(value < 0);
  let labelSize = $derived(Math.max(8, size * 0.28));
</script>

<div class="progress-ring" style="width: {size}px; height: {size}px;">
  <svg
    width={size}
    height={size}
    viewBox="0 0 {size} {size}"
    class:indeterminate={isIndeterminate}
  >
    <!-- Background ring -->
    <circle
      cx={size / 2}
      cy={size / 2}
      r={radius}
      fill="none"
      stroke="rgba(255,255,255,0.08)"
      stroke-width={strokeWidth}
    />
    <!-- Fill ring -->
    <circle
      class="fill-ring"
      cx={size / 2}
      cy={size / 2}
      r={radius}
      fill="none"
      stroke={color}
      stroke-width={strokeWidth}
      stroke-dasharray={circumference}
      stroke-dashoffset={offset}
      stroke-linecap="round"
      transform="rotate(-90 {size / 2} {size / 2})"
    />
  </svg>
  {#if label}
    <span class="ring-label" style="font-size: {labelSize}px; color: {color};">{label}</span>
  {/if}
</div>

<style>
  .progress-ring {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .fill-ring {
    transition: stroke-dashoffset 0.4s cubic-bezier(0.4, 0, 0.2, 1);
  }

  svg.indeterminate {
    animation: ring-spin 1.2s linear infinite;
  }

  svg.indeterminate .fill-ring {
    transition: none;
  }

  @keyframes ring-spin {
    to { transform: rotate(360deg); }
  }

  .ring-label {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    font-family: 'Orbitron', 'Consolas', 'Courier New', monospace;
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
    pointer-events: none;
    user-select: none;
  }
</style>
