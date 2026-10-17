import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// The build is embedded into the Go binary (internal/httpd/ui/dist) and served
// under /ui/. In dev, `npm run dev` proxies the API to a running daemon.
export default defineConfig({
  base: '/ui/',
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/httpd/ui/dist',
    emptyOutDir: true,
    assetsInlineLimit: 0,
    chunkSizeWarningLimit: 800,
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:47621' },
  },
});
