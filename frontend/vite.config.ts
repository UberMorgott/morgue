import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: parseInt(process.env.WAILS_VITE_PORT || '9245'),
    strictPort: true,
  },
});
