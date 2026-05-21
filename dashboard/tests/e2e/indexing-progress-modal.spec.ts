/**
 * E2E: IndexingProgressModal + useIndexProgress (#1191 Sub-D)
 *
 * Tests:
 *   1. Landing page reindex button visible on card hover → modal opens.
 *   2. Graph route Rebuild button → modal opens.
 *   3. Modal close button dismisses modal.
 *   4. Modal close while streaming shows minimised toaster.
 *   5. SSE progress events update the per-repo bars (mock SSE via route interception).
 *   6. VIEW screenshot at 42% completion with multiple repos.
 *
 * The tests run headless. When no daemon is running, the landing and graph pages
 * render their loading/error states — the reindex buttons are still present and
 * the modal itself is pure client-side, so all assertions pass.
 *
 * SSE interception: Playwright's route.fulfill() with streaming body lets us
 * inject controlled progress events.
 */

import { test, expect } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'indexing-progress')

function mkdirp(dir: string) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true })
}

// ──────────────────────────────────────────────────────────────────────────────
// SSE helpers
// ──────────────────────────────────────────────────────────────────────────────

/** Build a minimal SSE text block from an array of [event, data] pairs. */
function buildSse(events: Array<[string, unknown]>): string {
  return events
    .map(([evt, data]) => `event: ${evt}\ndata: ${JSON.stringify(data)}\n\n`)
    .join('')
}

/** Intercept /api/index-progress/* and stream controlled SSE events. */
async function interceptProgress(
  page: Parameters<typeof test['use']>[0] extends infer _ ? import('@playwright/test').Page : never,
  groupSlug: string,
  sseBody: string,
) {
  await page.route(`**/api/index-progress/${groupSlug}`, (route) => {
    route.fulfill({
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
        Connection: 'keep-alive',
      },
      body: sseBody,
    })
  })
}

// ──────────────────────────────────────────────────────────────────────────────
// Test data
// ──────────────────────────────────────────────────────────────────────────────

const GROUP_SLUG = 'fixture-a'

/** Progress snapshot at ~42% overall (two repos, different phases). */
const PROGRESS_42_PCT = buildSse([
  ['connected', { group: GROUP_SLUG }],
  [
    'progress',
    {
      group: GROUP_SLUG,
      repos: [
        {
          slug: 'repo-alpha',
          phase: 'extracting_ast',
          files_done: 120,
          files_total: 200,
          current_file: 'src/services/auth/TokenValidator.ts',
          phase_started_at: new Date(Date.now() - 12_000).toISOString(),
        },
        {
          slug: 'repo-beta',
          phase: 'scanning',
          files_done: 24,
          files_total: 300,
          eta_ms: 35_000,
          phase_started_at: new Date(Date.now() - 4_000).toISOString(),
        },
      ],
    },
  ],
])

const PROGRESS_DONE = buildSse([
  ['connected', { group: GROUP_SLUG }],
  [
    'progress',
    {
      group: GROUP_SLUG,
      repos: [
        { slug: 'repo-alpha', phase: 'done', files_done: 200, files_total: 200 },
        { slug: 'repo-beta', phase: 'done', files_done: 300, files_total: 300 },
      ],
    },
  ],
  ['close', { group: GROUP_SLUG, status: 'done' }],
])

// ──────────────────────────────────────────────────────────────────────────────
// Mock registry (so landing page renders group cards)
// ──────────────────────────────────────────────────────────────────────────────

const MOCK_REGISTRY = {
  groups: [
    {
      name: GROUP_SLUG,
      display_name: 'Fixture A',
      config_path: '/tmp/fixture-a.toml',
      repos: ['repo-alpha', 'repo-beta'],
      entity_count: 1500,
    },
  ],
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

test.describe('IndexingProgressModal', () => {
  test.beforeEach(async ({ page }) => {
    mkdirp(SCREENSHOT_DIR)
  })

  test('landing: reindex button appears on card hover and opens modal', async ({ page }) => {
    // Intercept registry so the group card renders.
    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    await interceptProgress(page, GROUP_SLUG, PROGRESS_42_PCT)

    await page.goto(`${BASE_URL}/`)
    await page.waitForLoadState('networkidle')

    // Find the reindex button — it may be hidden until hover.
    const reindexBtn = page.getByTestId(`reindex-btn-${GROUP_SLUG}`)

    // Hover the card to reveal button.
    const card = page.locator('[data-card]').first()
    await card.hover()

    // Wait for button to be visible (opacity transition).
    await expect(reindexBtn).toBeVisible({ timeout: 3000 })
    await reindexBtn.click()

    // Modal should appear.
    const modal = page.getByTestId('indexing-progress-modal')
    await expect(modal).toBeVisible({ timeout: 3000 })
    await expect(modal).toContainText('Indexing your group')
  })

  test('modal shows overall progress bar and per-repo rows', async ({ page }) => {
    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    await interceptProgress(page, GROUP_SLUG, PROGRESS_42_PCT)

    await page.goto(`${BASE_URL}/`)
    await page.waitForLoadState('networkidle')

    const card = page.locator('[data-card]').first()
    await card.hover()
    await page.getByTestId(`reindex-btn-${GROUP_SLUG}`).click()

    const modal = page.getByTestId('indexing-progress-modal')
    await expect(modal).toBeVisible()

    // Wait for progress to stream in.
    await expect(page.getByTestId('overall-bar')).toBeVisible({ timeout: 5000 })

    // Repo rows.
    await expect(page.getByTestId('repo-row-repo-alpha')).toBeVisible()
    await expect(page.getByTestId('repo-row-repo-beta')).toBeVisible()

    // Phase badge on repo-alpha.
    await expect(page.getByTestId('repo-phase-repo-alpha')).toContainText('Extracting AST')

    // Current file shown.
    await expect(page.getByTestId('repo-file-repo-alpha')).toContainText('TokenValidator.ts')
  })

  test('VIEW screenshot at 42% completion', async ({ page }) => {
    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    await interceptProgress(page, GROUP_SLUG, PROGRESS_42_PCT)

    await page.goto(`${BASE_URL}/`)
    await page.waitForLoadState('networkidle')

    const card = page.locator('[data-card]').first()
    await card.hover()
    await page.getByTestId(`reindex-btn-${GROUP_SLUG}`).click()

    const modal = page.getByTestId('indexing-progress-modal')
    await expect(modal).toBeVisible()

    // Wait for repo rows to appear.
    await expect(page.getByTestId('repo-row-repo-alpha')).toBeVisible({ timeout: 5000 })

    // VIEW screenshot.
    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, 'modal-42pct.png'),
      fullPage: false,
    })
  })

  test('close button dismisses modal', async ({ page }) => {
    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    await interceptProgress(page, GROUP_SLUG, PROGRESS_42_PCT)

    await page.goto(`${BASE_URL}/`)
    await page.waitForLoadState('networkidle')

    const card = page.locator('[data-card]').first()
    await card.hover()
    await page.getByTestId(`reindex-btn-${GROUP_SLUG}`).click()

    const modal = page.getByTestId('indexing-progress-modal')
    await expect(modal).toBeVisible()

    await page.getByTestId('modal-close').click()
    await expect(modal).not.toBeVisible({ timeout: 2000 })
  })

  test('closing modal while indexing shows minimised toaster', async ({ page }) => {
    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    // Use 42% progress (not done) so status stays 'indexing'.
    await interceptProgress(page, GROUP_SLUG, PROGRESS_42_PCT)

    await page.goto(`${BASE_URL}/`)
    await page.waitForLoadState('networkidle')

    const card = page.locator('[data-card]').first()
    await card.hover()
    await page.getByTestId(`reindex-btn-${GROUP_SLUG}`).click()

    // Wait for progress to stream.
    await expect(page.getByTestId('repo-row-repo-alpha')).toBeVisible({ timeout: 5000 })

    // Close modal.
    await page.getByTestId('modal-close').click()

    // Toaster should appear because indexing is still running.
    const toaster = page.getByTestId('indexing-toaster')
    await expect(toaster).toBeVisible({ timeout: 3000 })
    await expect(toaster).toContainText(GROUP_SLUG)
  })

  test('graph route: Rebuild button opens modal', async ({ page }) => {
    // Log console errors for debugging.
    page.on('console', (msg) => {
      if (msg.type() === 'error') console.error('PAGE ERROR:', msg.text())
    })
    page.on('pageerror', (err) => console.error('PAGE EXCEPTION:', err.message))

    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    await interceptProgress(page, GROUP_SLUG, PROGRESS_42_PCT)

    // Intercept daemon API routes to prevent 502s.
    // Use a specific hostname pattern to avoid matching Vite's /src/api/*.ts module requests.
    await page.route(/^http:\/\/localhost:5173\/api\//, (route) => {
      const url = route.request().url()
      if (url.includes('/api/registry')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) })
      }
      if (url.includes(`/api/index-progress/${GROUP_SLUG}`)) {
        return route.fulfill({
          status: 200,
          headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' },
          body: PROGRESS_42_PCT,
        })
      }
      if (url.includes('/api/graph/') && url.includes('/labels')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ labels: [] }),
        })
      }
      if (url.includes('/api/graph/')) {
        return route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ nodes: [], edges: [], communities: [], all_edge_kinds: [], total_node_count: 0 }),
        })
      }
      // Default: 200 empty JSON for any other daemon API call.
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
    })

    await page.goto(`${BASE_URL}/graph/${GROUP_SLUG}`)
    await page.waitForLoadState('networkidle')

    // Rebuild button is in the top-right overlay (visible once !isLoading && !error).
    const rebuildBtn = page.getByTestId('graph-reindex-btn')
    await expect(rebuildBtn).toBeVisible({ timeout: 10000 })
    await rebuildBtn.click()

    const modal = page.getByTestId('indexing-progress-modal')
    await expect(modal).toBeVisible({ timeout: 3000 })
  })

  test('modal auto-closes after done event (no console errors)', async ({ page }) => {
    const consoleErrors: string[] = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })

    await page.route('**/api/registry', (route) =>
      route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) }),
    )
    await interceptProgress(page, GROUP_SLUG, PROGRESS_DONE)

    await page.goto(`${BASE_URL}/`)
    await page.waitForLoadState('networkidle')

    const card = page.locator('[data-card]').first()
    await card.hover()
    await page.getByTestId(`reindex-btn-${GROUP_SLUG}`).click()

    const modal = page.getByTestId('indexing-progress-modal')
    await expect(modal).toBeVisible()

    // Wait for done state — overall bar should be 100%.
    await expect(page.getByTestId('overall-pct')).toHaveText('100%', { timeout: 5000 })
    await expect(modal).toContainText('Indexing complete')

    // Modal auto-closes after 2 s delay.
    await expect(modal).not.toBeVisible({ timeout: 5000 })

    // Zero console errors.
    expect(consoleErrors).toHaveLength(0)
  })
})
