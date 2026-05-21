/**
 * E2E: Enrichment cost estimator dialog (#1287)
 *
 * Tests run HEADLESS against the Vite dev server (port 5173).
 * Without a live daemon, the /api/enrichments/.../estimate endpoint returns
 * 404. The test mocks the API response so the dialog can be tested in isolation.
 *
 * Deliverables:
 *   - Screenshot of the cost estimate dialog (test-results/cost-modal/*.png)
 *   - Structural assertions: dialog renders with tiers table + confirm button
 */

import { test, expect, type Page, type Route } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'
import fs from 'fs'

const __filename = fileURLToPath(import.meta.url)
const __dirname  = path.dirname(__filename)

const BASE_URL    = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const PENDING_URL = `${BASE_URL}/pending/fixture-a`
const SHOT_DIR    = path.join(__dirname, '..', '..', 'test-results', 'cost-modal')

// ─────────────────────────────────────────────────────────────────────────────
// Mock data
// ─────────────────────────────────────────────────────────────────────────────

const MOCK_ESTIMATE = {
  tiers: [
    { band: 'critical', model: 'sonnet', count: 110, est_tokens: 88_000,    est_usd: 0.26 },
    { band: 'high',     model: 'haiku',  count: 890, est_tokens: 445_000,   est_usd: 0.27 },
    { band: 'medium',   model: 'haiku',  count: 1420, est_tokens: 710_000,  est_usd: 0.43 },
    { band: 'low',      model: 'haiku',  count: 3430, est_tokens: 1_372_000, est_usd: 0.83 },
  ],
  already_enriched: 380,
  total_est_tokens: 2_615_000,
  total_est_usd:    1.79,
  est_minutes:      12,
}

const MOCK_ENRICHMENTS = {
  items: [
    {
      candidate_id: 'cand-001',
      subject_id:   'ep-001',
      kind:         'describe_entity',
      entity_kind:  'http_endpoint',
      score:        90,
      criticality_band: 'critical',
    },
    {
      candidate_id: 'cand-002',
      subject_id:   'svc-001',
      kind:         'describe_entity',
      entity_kind:  'Service',
      score:        65,
      criticality_band: 'high',
    },
  ],
  total: 2,
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

function mkdirShot() {
  fs.mkdirSync(SHOT_DIR, { recursive: true })
}

async function screenshot(page: Page, name: string) {
  mkdirShot()
  await page.screenshot({
    path: path.join(SHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

async function interceptAPIs(page: Page) {
  // Mock enrichments list so the toolbar "Run enrichment" button appears.
  await page.route('**/api/enrichments/fixture-a', (route: Route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(MOCK_ENRICHMENTS),
    })
  })
  // Mock the estimate endpoint.
  await page.route('**/api/enrichments/fixture-a/estimate', (route: Route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(MOCK_ESTIMATE),
    })
  })
  // Mock repairs (empty) to avoid 404 noise.
  await page.route('**/api/repairs/fixture-a', (route: Route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items: [], total: 0 }),
    })
  })
  // Mock progress (empty) to suppress progress panel.
  await page.route('**/api/enrichments/fixture-a/progress', (route: Route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        tiers: [],
        overall_done: 0,
        overall_total: 0,
      }),
    })
  })
}

async function navigateToEnrichments(page: Page) {
  await page.goto(PENDING_URL, { waitUntil: 'domcontentloaded', timeout: 30_000 })
  // Switch to the Enrichment candidates tab.
  await page.getByRole('tab', { name: /Enrichment candidates/i }).waitFor({ state: 'visible', timeout: 10_000 })
  await page.getByRole('tab', { name: /Enrichment candidates/i }).click()
  await page.waitForTimeout(400)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

test.describe('Enrichment cost estimator dialog — #1287', () => {
  test.beforeEach(async ({ page }) => {
    await interceptAPIs(page)
    await navigateToEnrichments(page)
  })

  test('VIEW — cost estimator dialog renders with tier breakdown', async ({ page }) => {
    // Click "Run enrichment" button.
    const runBtn = page.getByTestId('run-enrichment-btn')
    await runBtn.waitFor({ state: 'visible', timeout: 8_000 })
    await runBtn.click()

    // Wait for the modal to appear.
    const modal = page.getByTestId('enrichment-cost-modal')
    await modal.waitFor({ state: 'visible', timeout: 5_000 })

    // Take the primary screenshot (deliverable).
    await page.waitForTimeout(300) // let animations settle
    await screenshot(page, '1-cost-estimator-dialog')
  })

  test('dialog shows cost breakdown table', async ({ page }) => {
    await page.getByTestId('run-enrichment-btn').click()
    const modal = page.getByTestId('enrichment-cost-modal')
    await modal.waitFor({ state: 'visible', timeout: 5_000 })

    // Breakdown table must be present.
    await expect(modal.getByTestId('cost-breakdown-table')).toBeVisible()

    // Critical tier row (110 entities).
    await expect(modal.getByText('Critical')).toBeVisible()
    // Total cost cell (tfoot row, not the button).
    await expect(modal.getByRole('cell', { name: '$1.79' })).toBeVisible()
  })

  test('dialog cancel closes the modal', async ({ page }) => {
    await page.getByTestId('run-enrichment-btn').click()
    await page.getByTestId('enrichment-cost-modal').waitFor({ state: 'visible', timeout: 5_000 })

    await page.getByTestId('cost-modal-cancel').click()
    await expect(page.getByTestId('enrichment-cost-modal')).not.toBeVisible()

    await screenshot(page, '2-after-cancel')
  })

  test('Escape key closes the modal', async ({ page }) => {
    await page.getByTestId('run-enrichment-btn').click()
    await page.getByTestId('enrichment-cost-modal').waitFor({ state: 'visible', timeout: 5_000 })

    await page.keyboard.press('Escape')
    await expect(page.getByTestId('enrichment-cost-modal')).not.toBeVisible()
  })

  test('confirm button shows total cost', async ({ page }) => {
    await page.getByTestId('run-enrichment-btn').click()
    const modal = page.getByTestId('enrichment-cost-modal')
    await modal.waitFor({ state: 'visible', timeout: 5_000 })

    const confirmBtn = page.getByTestId('cost-modal-confirm')
    await expect(confirmBtn).toBeVisible()
    // Should include cost in button text.
    await expect(confirmBtn).toContainText('$1.79')
  })

  test('backdrop click closes the modal', async ({ page }) => {
    await page.getByTestId('run-enrichment-btn').click()
    await page.getByTestId('enrichment-cost-modal').waitFor({ state: 'visible', timeout: 5_000 })

    // Click outside the dialog (top-left corner of backdrop).
    await page.getByTestId('cost-modal-backdrop').click({ position: { x: 10, y: 10 } })
    await expect(page.getByTestId('enrichment-cost-modal')).not.toBeVisible()
  })
})
