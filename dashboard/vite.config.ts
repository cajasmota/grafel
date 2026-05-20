import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'path'

// https://vitejs.dev/config/
export default defineConfig(({ mode }) => {
  // Load env so VITE_API_PORT is available at config-build time
  const env = loadEnv(mode, process.cwd(), '')
  const apiPort = env.VITE_API_PORT ?? '31000'
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
