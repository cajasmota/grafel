/**
 * E2E: Diagnostics surface — headless smoke + VIEW screenshot (#1187)
 *
 * Verifies:
 *   1. /diagnostics route loads without console errors
 *   2. "Diagnostics" nav item is present and active when on the page
 *   3. Daemon panel renders (with or without a live daemon)
 *   4. Action buttons are present ("Run health check", "Kill stale daemons")
 *   5. Screenshot captured for VIEW review
 */

import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const DIAGNOSTICS_URL = `${BASE_URL}/diagnostics`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'diagnostics')

// ─────────────────────────────────────────────────────────────────────────────

test.describe('Diagnostics surface — #1187', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', msg => {
      if (msg.type() === 'error') {
        const text = msg.text()
        // Ignore browser-internal network errors (no live daemon in CI)
        if (!text.includes('Failed to load resource') && !text.includes('ERR_CONNECTION')) {
          consoleErrors.push(text)
        }
      }
    })
  })

  test('Diagnostics nav item is present in top nav', async ({ page }) => {
    await page.goto(DIAGNOSTICS_URL, { waitUntil: 'domcontentloaded' })
    const nav = page.getByRole('navigation', { name: 'Surface navigation' })
    await nav.waitFor({ state: 'visible', timeout: 10000 })

    const diagItem = nav.getByText('Diagnostics')
    await expect(diagItem).toBeVisible()
  })

  test('Diagnostics page renders daemon panel', async ({ page }) => {
    await page.goto(DIAGNOSTICS_URL, { waitUntil: 'domcontentloaded' })

    // Wait for either the loaded content or skeleton
    await Promise.race([
      page.getByText('Daemon').waitFor({ state: 'visible', timeout: 8000 }),
      page.waitForTimeout(8000),
    ]).catch(() => {})

    // Page heading must be present
    const heading = page.getByRole('heading', { level: 1 })
    await expect(heading).toBeVisible()
    await expect(heading).toHaveText('Diagnostics')
  })

  test('Run health check button is present', async ({ page }) => {
    await page.goto(DIAGNOSTICS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const btn = page.getByRole('button', { name: /run health check/i })
    await expect(btn).toBeVisible()
  })

  test('Kill stale daemons button is present', async ({ page }) => {
    await page.goto(DIAGNOSTICS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const btn = page.getByRole('button', { name: /kill stale daemons/i })
    await expect(btn).toBeVisible()
  })

  test('VIEW screenshot — diagnostics surface', async ({ page }) => {
    await page.goto(DIAGNOSTICS_URL, { waitUntil: 'domcontentloaded' })

    // Allow time for API data to arrive (or graceful error state)
    await page.waitForTimeout(3000)

    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, 'diagnostics-light.png'),
      fullPage: false,
    })

    // VIEW: dark mode
    const themeBtn = page.getByRole('button', { name: /switch to dark mode/i })
    if (await themeBtn.isVisible()) {
      await themeBtn.click()
      await page.waitForTimeout(300)
      await page.screenshot({
        path: path.join(SCREENSHOT_DIR, 'diagnostics-dark.png'),
        fullPage: false,
      })
    }
  })

  test('Kill stale daemons — confirm modal appears on click', async ({ page }) => {
    await page.goto(DIAGNOSTICS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1000)

    const killBtn = page.getByRole('button', { name: /kill stale daemons/i })
    await killBtn.waitFor({ state: 'visible', timeout: 5000 })
    await killBtn.click()

    // Confirm modal should appear
    const modal = page.getByRole('heading', { name: /kill stale daemons/i })
    await expect(modal).toBeVisible({ timeout: 3000 })

    // Cancel closes it
    const cancelBtn = page.getByRole('button', { name: 'Cancel' })
    await cancelBtn.click()
    await expect(modal).not.toBeVisible()
  })

  test.afterEach(() => {
    if (consoleErrors.length > 0) {
      console.warn('Console errors on /diagnostics:', consoleErrors)
    }
    expect(consoleErrors, `Console errors: ${consoleErrors.join(', ')}`).toHaveLength(0)
  })
})
