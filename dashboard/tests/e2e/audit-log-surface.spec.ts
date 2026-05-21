/**
 * E2E: Audit Log surface — headless smoke (#1258)
 *
 * Verifies:
 *   1. /audit-log route loads without JS console errors
 *   2. Page heading "Audit Log" is present
 *   3. "Audit Log" entry appears in the Operate nav menu
 *   4. Filters row (search input) is present
 *   5. Export button is present
 *   6. Live tail toggle is present and labelled "Live"
 *   7. Screenshot captured for VIEW review
 */

import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const AUDIT_URL = `${BASE_URL}/audit-log`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'audit-log')

// ─────────────────────────────────────────────────────────────────────────────

test.describe('Audit Log surface — #1258', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text()
        // Ignore network errors expected in CI (no live daemon)
        if (
          !text.includes('Failed to load resource') &&
          !text.includes('ERR_CONNECTION') &&
          !text.includes('net::ERR')
        ) {
          consoleErrors.push(text)
        }
      }
    })
  })

  test('audit-log route loads without JS errors', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    // Allow time for React to hydrate
    await page.waitForTimeout(1500)
    expect(consoleErrors, `JS console errors: ${consoleErrors.join('\n')}`).toHaveLength(0)
  })

  test('page heading is present', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    await page.getByRole('heading', { level: 1, name: /audit log/i }).waitFor({
      state: 'visible',
      timeout: 10_000,
    })
  })

  test('audit-log page has data-testid root element', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    await expect(page.getByTestId('audit-log-page')).toBeVisible({ timeout: 10_000 })
  })

  test('search input is present', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    const searchInput = page.getByTestId('search-input')
    await expect(searchInput).toBeVisible({ timeout: 10_000 })
  })

  test('live tail toggle is present and shows Live state', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    const toggle = page.getByTestId('live-tail-toggle')
    await expect(toggle).toBeVisible({ timeout: 10_000 })
    await expect(toggle).toContainText('Live')
  })

  test('refresh and export buttons are present', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    await expect(page.getByTestId('refresh-btn')).toBeVisible({ timeout: 10_000 })
    await expect(page.getByTestId('export-btn')).toBeVisible({ timeout: 10_000 })
  })

  test('Audit Log appears in Operate nav menu', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })

    // Open the Operate menu
    const operateBtn = page.getByTestId('operate-menu')
    await operateBtn.waitFor({ state: 'visible', timeout: 10_000 })
    await operateBtn.click()

    const auditItem = page.getByRole('menuitem', { name: /audit log/i })
    await expect(auditItem).toBeVisible({ timeout: 5_000 })
  })

  test('empty state renders when no entries exist', async ({ page }) => {
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })

    // Wait for loading to complete — either audit-list renders or empty state
    await page.waitForTimeout(2_000)

    const list = page.getByTestId('audit-list')
    await expect(list).toBeVisible({ timeout: 10_000 })
  })

  test('screenshot — audit-log light mode', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1_500)

    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, 'audit-log-light.png'),
      fullPage: true,
    })
  })

  test('screenshot — audit-log dark mode', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.emulateMedia({ colorScheme: 'dark' })
    await page.goto(AUDIT_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1_500)

    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, 'audit-log-dark.png'),
      fullPage: true,
    })
  })
})
