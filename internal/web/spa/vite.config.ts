import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// P13.5 · Vite build outputs to ../static/public/react/ so embed.go's
// all:public directive picks them up automatically. Base path /react/
// matches the route registered in server.go.
export default defineConfig({
  plugins: [react()],
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
