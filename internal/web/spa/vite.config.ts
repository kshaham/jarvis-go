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
  },
  server: {
    port: 5174,
    proxy: {
      '/api': 'http://localhost:3555',
      '/sse': 'http://localhost:3555',
    },
  },
});
