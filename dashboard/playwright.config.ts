import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright config for headless E2E tests.
 *
 * The dashboard is served from the Vite dev server (port 5173) which proxies
 * /api/* to the archigraph daemon on port 47274. If no daemon is running,
 * API calls will 502/404 — the tests degrade gracefully by checking UI
 * structure only (not graph data).
 */
export default defineConfig({
  testDir: './tests/e2e',
  outputDir: './test-results',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: 'list',

  use: {
    baseURL: process.env.TEST_BASE_URL ?? 'http://localhost:5173',
    // Headless by default; set HEADLESS=false for debugging
    headless: process.env.HEADLESS !== 'false',
    viewport: { width: 1440, height: 900 },
    screenshot: 'on',
    trace: 'on-first-retry',
  },

  projects: [
    {
      name: 'chromium-headless',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Start Vite dev server before running tests
  webServer: {
    command: 'npx vite --port 5173',
    url: 'http://localhost:5173',
    reuseExistingServer: true,
    timeout: 30000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
