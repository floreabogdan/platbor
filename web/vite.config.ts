import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// Dev: Vite serves the SPA and proxies API + registry-protocol paths to the Go
// server on :8080. Build: output goes to web/dist, which the binary embeds.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: Object.fromEntries(
      ['/api', '/healthz', '/readyz', '/v2', '/npm', '/nuget', '/generic'].map((p) => [
        p,
        { target: 'http://localhost:8080', changeOrigin: true },
      ]),
    ),
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
  },
});
