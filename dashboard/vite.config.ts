import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'path'

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  // Load env so VITE_API_PORT is available at config-build time
  const env = loadEnv(mode, process.cwd(), '')
  // Default to 47274 — the port the daemon's embedded dashboard listens on (#929/#931).
  // Override with VITE_API_PORT=<n> for non-default installations.
  const apiPort = env.VITE_API_PORT ?? '47274'
  const apiBase = `http://127.0.0.1:${apiPort}`

  return {
    plugins: [react()],
    resolve: {
      alias: {
        '@': resolve(__dirname, './src'),
      },
    },
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: apiBase,
          changeOrigin: true,
        },
        '/ws': {
          target: apiBase.replace('http://', 'ws://'),
          ws: true,
        },
      },
    },
    build: {
      outDir: 'dist',
      sourcemap: true,
      rollupOptions: {
        output: {
          manualChunks: {
            vendor: ['react', 'react-dom', 'react-router-dom'],
            query: ['@tanstack/react-query'],
            radix: [
              '@radix-ui/react-dialog',
              '@radix-ui/react-tabs',
              '@radix-ui/react-tooltip',
              '@radix-ui/react-dropdown-menu',
              '@radix-ui/react-select',
            ],
            // React Flow lazy chunk — loaded only when the flow detail panel opens (#1150)
            'flow-dag': ['@xyflow/react', '@xyflow/system', 'dagre'],
            // Cosmograph WebGL renderer — split from the main bundle so non-graph
            // pages (Paths, Flows, Topology, Docs …) never download the GPU renderer.
            // Combined with the lazy GraphRoute in App.tsx this saves ~500 KB gzipped
            // on initial load for users who only use non-graph surfaces. (#1249 perf)
            cosmograph: ['@cosmograph/react'],
          },
        },
      },
    },
    test: {
      globals: true,
      environment: 'jsdom',
      setupFiles: ['./tests/setup.ts'],
      css: false,
    },
  }
})
