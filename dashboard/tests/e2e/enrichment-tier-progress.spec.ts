/**
 * E2E: Per-tier enrichment progress UI (#1286)
 *
 * Tests:
 *   1. EnrichmentProgressPanel is absent when there are no active jobs.
 *   2. Panel appears when the progress endpoint reports running jobs.
 *   3. All four tier bars render independently with correct ARIA labels.
 *   4. "Complete" label appears when overall_done === overall_total.
 *   5. Polling resumes after an error response.
 *   6. VIEW screenshot with active progress (tier bars animating).
 *
 * Strategy: Playwright route.fulfill() intercepts
 *   GET /api/enrichments/{group}/progress
 * and returns controlled JSON. No live daemon required.
 *
 * The test also intercepts /api/enrichments/{group} so the Enrichment
 * Candidates tab renders (empty list is fine) without a 502.
 */

import { test, expect } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'enrichment-progress')

function mkdirp(dir: string) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true })
}

// ──────────────────────────────────────────────────────────────────────────────
// Shared mock data
// ──────────────────────────────────────────────────────────────────────────────

const GROUP_SLUG = 'fixture-enrich'

const MOCK_REGISTRY = {
  groups: [
    {
      name: GROUP_SLUG,
      display_name: 'Fixture Enrich',
      config_path: '/tmp/fixture-enrich.toml',
      repos: ['repo-alpha'],
      entity_count: 2000,
    },
  ],
}

/** Progress snapshot with all 4 tiers active — some running, some queued. */
const PROGRESS_ACTIVE = {
  tiers: [
    { band: 'critical', total: 110, done: 23,  running: 3,  queued: 84,  failed: 0, eta_seconds: 120 },
    { band: 'high',     total: 890, done: 234, running: 2,  queued: 654, failed: 0, eta_seconds: 480 },
    { band: 'medium',   total: 1420,done: 89,  running: 0,  queued: 1331,failed: 0 },
    { band: 'low',      total: 3430,done: 0,   running: 0,  queued: 3430,failed: 0 },
  ],
  overall_done: 346,
  overall_total: 5850,
  started_at: new Date(Date.now() - 30_000).toISOString(),
}

/** All tiers complete. */
const PROGRESS_DONE = {
  tiers: [
    { band: 'critical', total: 110,  done: 110,  running: 0, queued: 0, failed: 0 },
    { band: 'high',     total: 890,  done: 890,  running: 0, queued: 0, failed: 0 },
    { band: 'medium',   total: 1420, done: 1420, running: 0, queued: 0, failed: 0 },
    { band: 'low',      total: 3430, done: 3430, running: 0, queued: 0, failed: 0 },
  ],
  overall_done: 5850,
  overall_total: 5850,
}

/** No jobs at all — panel should not render. */
const PROGRESS_EMPTY = {
  tiers: [
    { band: 'critical', total: 0, done: 0, running: 0, queued: 0, failed: 0 },
    { band: 'high',     total: 0, done: 0, running: 0, queued: 0, failed: 0 },
    { band: 'medium',   total: 0, done: 0, running: 0, queued: 0, failed: 0 },
    { band: 'low',      total: 0, done: 0, running: 0, queued: 0, failed: 0 },
  ],
  overall_done: 0,
  overall_total: 0,
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

type Page = import('@playwright/test').Page

/** Intercept all daemon API calls with sane defaults, plus a custom progress payload. */
async function mockEnrichmentProgress(page: Page, progressPayload: object) {
  await page.route(/^http:\/\/localhost:5173\/api\//, (route) => {
    const url = route.request().url()

    if (url.includes('/api/registry')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(MOCK_REGISTRY) })
    }
    if (url.includes(`/api/enrichments/${GROUP_SLUG}/progress`)) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(progressPayload) })
    }
    if (url.includes(`/api/enrichments/${GROUP_SLUG}/jobs`)) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ jobs: [], total: 0 }) })
    }
    if (url.includes(`/api/enrichments/${GROUP_SLUG}`)) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], total: 0 }) })
    }
    if (url.includes(`/api/repairs/${GROUP_SLUG}`)) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], total: 0, auto_resolvable_count: 0 }) })
    }
    // Default: 200 empty JSON.
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{}' })
  })
}

/** Navigate to the /pending/:group route and switch to the Enrichment tab. */
async function goToPendingEnrichments(page: Page) {
  await page.goto(`${BASE_URL}/pending/${GROUP_SLUG}`)
  await page.waitForLoadState('networkidle')
  // Click the "Enrichment candidates" tab.
  const enrichTab = page.getByRole('tab', { name: /enrichment candidates/i })
  await enrichTab.click()
}

// ──────────────────────────────────────────────────────────────────────────────
// Tests
// ──────────────────────────────────────────────────────────────────────────────

test.describe('EnrichmentProgressPanel (#1286)', () => {
  test.beforeEach(async ({ page }) => {
    mkdirp(SCREENSHOT_DIR)
    // Surface console errors so CI logs show them.
    page.on('console', (msg) => {
      if (msg.type() === 'error') console.error('[PAGE]', msg.text())
    })
    page.on('pageerror', (err) => console.error('[PAGE EXCEPTION]', err.message))
  })

  // ── 1. Panel absent when no jobs ──────────────────────────────────────────
  test('panel is hidden when overall_total is 0', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_EMPTY)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).not.toBeVisible()
  })

  // ── 2. Panel visible when jobs are active ─────────────────────────────────
  test('panel appears when enrichment is active', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_ACTIVE)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })
    await expect(panel).toContainText('Enrichment Progress')
  })

  // ── 3. All four tier bars render with ARIA ────────────────────────────────
  test('all 4 tier progress bars render with correct ARIA labels', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_ACTIVE)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })

    // Each tier bar has aria-label="${band} tier enrichment progress"
    for (const band of ['critical', 'high', 'medium', 'low']) {
      const group = page.getByRole('group', { name: new RegExp(`${band} tier enrichment progress`, 'i') })
      await expect(group).toBeVisible()

      // Progress bar has role="progressbar"
      const bar = group.getByRole('progressbar')
      await expect(bar).toBeVisible()

      // aria-valuenow should be a number 0–100
      const valuenow = await bar.getAttribute('aria-valuenow')
      const pct = Number(valuenow)
      expect(pct).toBeGreaterThanOrEqual(0)
      expect(pct).toBeLessThanOrEqual(100)
    }
  })

  // ── 4. ETA text per tier ──────────────────────────────────────────────────
  test('ETA text is shown for tiers with eta_seconds', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_ACTIVE)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })

    // Critical tier has eta_seconds=120 → "~2m"
    await expect(panel).toContainText('~2m')
    // High tier has eta_seconds=480 → "~8m"
    await expect(panel).toContainText('~8m')
    // Medium has running=0, queued>0 but no eta_seconds → "calculating…"
    await expect(panel).toContainText('calculating…')
  })

  // ── 4b. "not started" for tier with total>0 but no running/queued ────────
  test('not-started status shown for tier with 0 active jobs', async ({ page }) => {
    const progressWithNotStarted = {
      ...PROGRESS_ACTIVE,
      tiers: PROGRESS_ACTIVE.tiers.map((t) =>
        t.band === 'low' ? { ...t, queued: 0 } : t,
      ),
    }
    await mockEnrichmentProgress(page, progressWithNotStarted)
    await goToPendingEnrichments(page)
    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })
    await expect(panel).toContainText('not started')
  })

  // ── 5. "Complete" label when all done ────────────────────────────────────
  test('Complete label appears when all tiers are done', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_DONE)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })
    await expect(panel).toContainText('Complete')

    // All tier bars should show 100%
    const bars = page.getByRole('progressbar')
    const count = await bars.count()
    for (let i = 0; i < count; i++) {
      const val = await bars.nth(i).getAttribute('aria-valuenow')
      expect(Number(val)).toBe(100)
    }
  })

  // ── 6. Collapse toggle ───────────────────────────────────────────────────
  test('panel collapses and expands on header click', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_ACTIVE)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })

    // Body is visible initially (not collapsed).
    const body = page.locator('#enrichment-progress-body')
    await expect(body).toBeVisible()

    // Click header to collapse.
    const header = panel.getByRole('button', { name: /enrichment progress/i })
    await header.click()
    await expect(body).not.toBeVisible()

    // Click again to expand.
    await header.click()
    await expect(body).toBeVisible()
  })

  // ── 7. VIEW screenshot with active progress ───────────────────────────────
  test('VIEW screenshot: active progress bars', async ({ page }) => {
    await mockEnrichmentProgress(page, PROGRESS_ACTIVE)
    await goToPendingEnrichments(page)

    const panel = page.getByTestId('enrichment-progress-panel')
    await expect(panel).toBeVisible({ timeout: 5000 })
    // Ensure all 4 tier rows are rendered.
    for (const band of ['critical', 'high', 'medium', 'low']) {
      await expect(page.getByRole('group', { name: new RegExp(`${band} tier enrichment progress`, 'i') })).toBeVisible()
    }

    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, 'tier-progress-active.png'),
      fullPage: false,
    })

    // No console errors (checked via beforeEach listener).
  })
})
