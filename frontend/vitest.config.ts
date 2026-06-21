import { defineConfig } from 'vitest/config';

// Minimal config for unit tests of pure TS logic (stores, helpers).
// No Svelte plugin needed: tested modules import only from 'svelte/store'.
export default defineConfig({
  test: {
    environment: 'node',
    include: ['src/**/*.{test,spec}.ts'],
  },
});
