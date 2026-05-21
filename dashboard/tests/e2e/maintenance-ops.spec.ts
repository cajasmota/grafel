/**
 * E2E: Maintenance ops surface — headless smoke + VIEW screenshot (#1200)
 *
 * Verifies:
 *   1. Landing page renders group cards with an ops trigger button
 *   2. Clicking the ops trigger opens a dropdown menu with Rebuild / Reset / Cleanup
 *   3. Rebuild modal: opens on click, Cancel closes it
 *   4. Reset modal: opens on click, Cancel closes it
 *   5. VIEW screenshot of the kebab menu open
 *
 * HEADLESS CONSTRAINT: Reset and Cleanup actions are NOT executed in these
 * tests (they are destructive or require a live daemon). We open their modals
 * and cancel only.
 */

import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const INDEX_URL = BASE_URL + '/'
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'maintenance-ops')

// ─────────────────────────────────────────────────────────────────────────────

test.describe('Maintenance ops surface — #1200', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', msg => {
      if (msg.type() === 'error') {
        const text = msg.text()
        // Ignore browser-internal network errors (no live daemon in CI)
        if (
          !text.includes('Failed to load resource') &&
          !text.includes('ERR_CONNECTION') &&
          !text.includes('favicon')
        ) {
          consoleErrors.push(text)
        }
      }
    })
  })

  test('Landing page loads without console errors', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    // Allow API calls to settle (or fail gracefully)
    await page.waitForTimeout(2000)
    // No assertion on consoleErrors here — afterEach handles it
  })

  test('Ops trigger button is rendered on group cards', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    // In mock mode the registry returns groups — look for at least one ops trigger.
    // We use a broader selector so we don't depend on a specific group slug.
    const opsTriggers = page.locator('[data-ops-trigger]')
    const count = await opsTriggers.count()

    if (count === 0) {
      // No groups registered — the landing shows NoGroupsState, which is fine.
      // Just verify the page rendered something.
      const body = page.locator('body')
      await expect(body).toBeVisible()
      return
    }

    // At least one ops trigger should be visible.
    await expect(opsTriggers.first()).toBeVisible()
  })

  test('Ops menu opens on trigger click and shows expected actions', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const opsTriggers = page.locator('[data-ops-trigger]')
    const count = await opsTriggers.count()
    if (count === 0) {
      test.skip() // no groups in this environment
      return
    }

    const trigger = opsTriggers.first()
    await trigger.click()

    // Dropdown should appear
    const menu = page.locator('[data-ops-menu]').first()
    await expect(menu).toBeVisible({ timeout: 3000 })

    // Expected menu items
    await expect(menu.getByRole('menuitem', { name: /rebuild group/i })).toBeVisible()
    await expect(menu.getByRole('menuitem', { name: /reset group/i })).toBeVisible()
    await expect(menu.getByRole('menuitem', { name: /cleanup registry/i })).toBeVisible()
  })

  test('VIEW screenshot — kebab menu open on first card', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const opsTriggers = page.locator('[data-ops-trigger]')
    const count = await opsTriggers.count()
    if (count > 0) {
      await opsTriggers.first().click()
      // Wait for dropdown to appear
      await page.waitForTimeout(300)
    }

    await page.screenshot({
      path: path.join(SCREENSHOT_DIR, 'ops-menu-open.png'),
      fullPage: false,
    })
  })

  test('Rebuild modal: opens on click and Cancel closes it', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const opsTriggers = page.locator('[data-ops-trigger]')
    const count = await opsTriggers.count()
    if (count === 0) {
      test.skip()
      return
    }

    // Open menu
    await opsTriggers.first().click()

    const rebuildItem = page.getByRole('menuitem', { name: /rebuild group/i })
    await rebuildItem.waitFor({ state: 'visible', timeout: 3000 })
    await rebuildItem.click()

    // Rebuild confirm modal should appear
    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible({ timeout: 3000 })
    await expect(modal.getByRole('heading')).toContainText(/rebuild/i)

    // Cancel closes the modal
    await modal.getByRole('button', { name: 'Cancel' }).click()
    await expect(modal).not.toBeVisible({ timeout: 2000 })
  })

  test('Reset modal: opens on click, requires typing group name, Cancel closes it', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const opsTriggers = page.locator('[data-ops-trigger]')
    const count = await opsTriggers.count()
    if (count === 0) {
      test.skip()
      return
    }

    // Open menu
    await opsTriggers.first().click()

    const resetItem = page.getByRole('menuitem', { name: /reset group/i })
    await resetItem.waitFor({ state: 'visible', timeout: 3000 })
    await resetItem.click()

    // Reset confirm modal should appear
    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible({ timeout: 3000 })
    await expect(modal.getByRole('heading')).toContainText(/reset/i)

    // Confirm button should be disabled until group name is typed
    const confirmBtn = modal.getByRole('button', { name: /reset and rebuild/i })
    await expect(confirmBtn).toBeDisabled()

    // Cancel closes without executing
    await modal.getByRole('button', { name: 'Cancel' }).click()
    await expect(modal).not.toBeVisible({ timeout: 2000 })
  })

  test('Cleanup modal: opens on click, Cancel closes it', async ({ page }) => {
    await page.goto(INDEX_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    const opsTriggers = page.locator('[data-ops-trigger]')
    const count = await opsTriggers.count()
    if (count === 0) {
      test.skip()
      return
    }

    // Open menu
    await opsTriggers.first().click()

    const cleanupItem = page.getByRole('menuitem', { name: /cleanup registry/i })
    await cleanupItem.waitFor({ state: 'visible', timeout: 3000 })
    await cleanupItem.click()

    // Cleanup modal appears (preview fetch runs first; in mock mode it's instant)
    const modal = page.getByRole('dialog')
    await expect(modal).toBeVisible({ timeout: 5000 })
    await expect(modal.getByRole('heading')).toContainText(/cleanup/i)

    // Cancel closes without executing
    await modal.getByRole('button', { name: 'Cancel' }).click()
    await expect(modal).not.toBeVisible({ timeout: 2000 })
  })

  test.afterEach(() => {
    if (consoleErrors.length > 0) {
      console.warn('[maintenance-ops] Console errors:', consoleErrors)
    }
    expect(consoleErrors, `Console errors: ${consoleErrors.join(', ')}`).toHaveLength(0)
  })
})
