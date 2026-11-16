import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// The page is served at the root of wherever it is mounted (a proxy may
// strip a prefix), so every URL in it is relative.
export default defineConfig({
  plugins: [svelte()],
  base: './',
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    modulePreload: { polyfill: false },
  },
  server: {
    // `npm run dev` beside a running `agent-desk serve`.
    proxy: { '/api': 'http://127.0.0.1:7740' },
  },
});
