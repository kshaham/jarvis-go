import { cpSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// excalidrawFonts — copy Excalidraw's font files into the build output at
// /react/fonts/ so the whiteboard loads them same-origin (paired with
// window.EXCALIDRAW_ASSET_PATH='/react/' in index.html). This avoids the
// esm.sh CDN, which the app's font-src 'self' CSP blocks (231 console
// errors on /whiteboard otherwise). Build-only; runs after the bundle is
// written (emptyOutDir has already fired). Dev mode still uses the CDN.
function excalidrawFonts() {
  return {
    name: 'excalidraw-self-hosted-fonts',
    apply: 'build' as const,
    closeBundle() {
      const src = path.resolve(__dirname, 'node_modules/@excalidraw/excalidraw/dist/prod/fonts');
      const dest = path.resolve(__dirname, '../static/public/react/fonts');
      cpSync(src, dest, { recursive: true });
    },
  };
}

// P13.5 · Vite build outputs to ../static/public/react/ so embed.go's
// all:public directive picks them up automatically. Base path /react/
// matches the route registered in server.go.
export default defineConfig({
  plugins: [react(), excalidrawFonts()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  base: '/react/',
  build: {
    outDir: '../static/public/react',
    emptyOutDir: true,
    sourcemap: true,
    rollupOptions: {
      output: {
        // Keep the heavy data-viz vendors in their own stable, cacheable
        // chunks. tldraw is intentionally absent — it's split
        // automatically by the React.lazy import in WhiteboardScreen.
        manualChunks: {
          'vendor-charts': ['recharts'],
          'vendor-flow': ['@xyflow/react', 'd3-force'],
        },
      },
    },
  },
  server: {
    port: 5174,
    proxy: {
      // /api/events (the SSE stream) is covered by this /api proxy.
      '/api': 'http://localhost:3555',
    },
  },
});
