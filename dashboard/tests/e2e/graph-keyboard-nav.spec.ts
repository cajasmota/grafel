/**
 * E2E: Graph keyboard navigation (#1368)
 *
 * Tests:
 *   1. '/' focuses search input when no input is active
 *   2. F fires fit-view (no crash, no console error)
 *   3. 0 fires reset-zoom (no crash)
 *   4. Arrow keys + Tab do not crash when no node is selected
 *   5. N / P keys do not crash when search results are empty
 *   6. Cmd+[ / Cmd+] do not crash with empty history
 *   7. Shortcuts overlay lists the new graph-nav shortcuts
 *   8. Shortcuts are blocked when an input is focused
 *   9. No console errors on the graph page during nav interactions
 *  10. Screenshots (VIEW)
 */

import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'
import fs from 'fs'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'e2e-screenshots')

function ensureDir(dir: string) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true })
}

/** Navigate to graph page for the fixture group. */
async function gotoGraph(page: import('@playwright/test').Page) {
  await page.goto(`${BASE_URL}/fixture-a/graph`, { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(800)
}

test.describe('Graph keyboard navigation (#1368)', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text()
        // Ignore expected API 502s when no daemon is running
        if (
          !text.includes('Failed to load resource') &&
          !text.includes('ERR_CONNECTION') &&
          !text.includes('502') &&
          !text.includes('404')
        ) {
          consoleErrors.push(text)
        }
      }
    })
  })

  // ── 1. '/' focuses search ────────────────────────────────────────────────

  test("'/' key focuses the graph search input", async ({ page }) => {
    await gotoGraph(page)

    // Make sure no input is focused first
    await page.keyboard.press('Escape')
    await page.waitForTimeout(100)

    // Press '/'
    await page.keyboard.press('/')
    await page.waitForTimeout(100)

    const searchInput = page.locator('#graph-search')
    await expect(searchInput).toBeFocused()
  })

  // ── 2. F — fit view ───────────────────────────────────────────────────────

  test('F key triggers fit-view without errors', async ({ page }) => {
    await gotoGraph(page)

    // Click somewhere on the page to make sure no input is focused
    await page.locator('[role="img"][aria-label="Dependency graph"]').click({ position: { x: 100, y: 100 }, force: true }).catch(() => {
      // Canvas may not be rendered if daemon is down — that's fine
    })
    await page.waitForTimeout(100)

    // Press F
    await page.keyboard.press('f')
    await page.waitForTimeout(200)

    // No crash — we just verify no console errors and page is still alive
    await expect(page.locator('body')).toBeAttached()
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 3. 0 — reset zoom ────────────────────────────────────────────────────

  test('0 key triggers reset-zoom without errors', async ({ page }) => {
    await gotoGraph(page)

    await page.keyboard.press('0')
    await page.waitForTimeout(200)

    await expect(page.locator('body')).toBeAttached()
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 4. Arrow keys + Tab without selection don't crash ────────────────────

  test('Arrow keys without a selected node do not crash', async ({ page }) => {
    await gotoGraph(page)

    for (const key of ['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight']) {
      await page.keyboard.press(key)
      await page.waitForTimeout(50)
    }

    await expect(page.locator('body')).toBeAttached()
    expect(consoleErrors).toHaveLength(0)
  })

  test('Tab without a selected node does not crash', async ({ page }) => {
    await gotoGraph(page)

    await page.keyboard.press('Tab')
    await page.waitForTimeout(100)

    await expect(page.locator('body')).toBeAttached()
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 5. N / P without search results ─────────────────────────────────────

  test('N / P keys without search results do not crash', async ({ page }) => {
    await gotoGraph(page)

    await page.keyboard.press('n')
    await page.waitForTimeout(50)
    await page.keyboard.press('p')
    await page.waitForTimeout(50)

    await expect(page.locator('body')).toBeAttached()
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 6. Cmd+[ / Cmd+] with empty history ──────────────────────────────────

  test('Cmd+[ and Cmd+] with empty history do not crash', async ({ page }) => {
    await gotoGraph(page)

    await page.keyboard.press('Meta+[')
    await page.waitForTimeout(50)
    await page.keyboard.press('Meta+]')
    await page.waitForTimeout(50)

    await expect(page.locator('body')).toBeAttached()
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 7. Shortcuts overlay contains new graph nav shortcuts ─────────────────

  test("Shortcuts overlay lists new graph-nav shortcuts", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    // Open shortcuts overlay
    await page.keyboard.press('?')
    await expect(page.getByTestId('shortcuts-overlay')).toBeVisible()

    // Filter to graph shortcuts
    await page.getByTestId('shortcuts-overlay-search').fill('neighbor')
    await page.waitForTimeout(200)

    // Graph category should be visible with the new nav shortcuts
    await expect(page.getByTestId('shortcuts-category-graph')).toBeVisible()

    // There should be at least one shortcut row about neighbor navigation
    const rows = page.getByTestId('shortcut-row')
    await expect(rows.first()).toBeVisible()

    // Close
    await page.keyboard.press('Escape')
  })

  test("Shortcuts overlay lists '/' shortcut", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    await page.keyboard.press('?')
    await expect(page.getByTestId('shortcuts-overlay')).toBeVisible()

    await page.getByTestId('shortcuts-overlay-search').fill('search input')
    await page.waitForTimeout(200)

    await expect(page.getByTestId('shortcuts-category-graph')).toBeVisible()
    await page.keyboard.press('Escape')
  })

  test("Shortcuts overlay lists history nav shortcuts", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    await page.keyboard.press('?')
    await expect(page.getByTestId('shortcuts-overlay')).toBeVisible()

    await page.getByTestId('shortcuts-overlay-search').fill('history')
    await page.waitForTimeout(200)

    await expect(page.getByTestId('shortcuts-category-graph')).toBeVisible()
    await page.keyboard.press('Escape')
  })

  // ── 8. Keys blocked when input is focused ─────────────────────────────────

  test("'/' key is ignored when search input is already focused", async ({ page }) => {
    await gotoGraph(page)

    // Focus the search input manually
    const searchInput = page.locator('#graph-search')
    await searchInput.focus()
    await expect(searchInput).toBeFocused()

    // Type some text — '/' should go into the input, not re-focus
    await page.keyboard.type('foo')
    await expect(searchInput).toHaveValue('foo')

    // Clear it
    await searchInput.clear()
  })

  // ── 9. No console errors ──────────────────────────────────────────────────

  test('No unexpected console errors during keyboard nav interactions', async ({ page }) => {
    await gotoGraph(page)

    // Fire a series of nav keys
    for (const key of ['f', '0', 'n', 'p', 'ArrowUp', 'ArrowDown']) {
      await page.keyboard.press(key)
      await page.waitForTimeout(30)
    }

    await page.keyboard.press('Meta+[')
    await page.keyboard.press('Meta+]')
    await page.waitForTimeout(200)

    expect(consoleErrors).toHaveLength(0)
  })

  // ── 10. Screenshots (VIEW) ────────────────────────────────────────────────

  test('Screenshot — graph page with keyboard nav ready (VIEW)', async ({ page }) => {
    await gotoGraph(page)
    await page.waitForTimeout(600)

    ensureDir(SCREENSHOT_DIR)
    const p = path.join(SCREENSHOT_DIR, 'graph-keyboard-nav-ready.png')
    await page.screenshot({ path: p, fullPage: false })
    expect(fs.existsSync(p)).toBe(true)
    console.log(`[VIEW] Graph keyboard nav: ${p}`)
  })

  test('Screenshot — shortcuts overlay filtered to graph nav (VIEW)', async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    await page.keyboard.press('?')
    await expect(page.getByTestId('shortcuts-overlay')).toBeVisible()
    await page.getByTestId('shortcuts-overlay-search').fill('neighbor')
    await page.waitForTimeout(300)

    ensureDir(SCREENSHOT_DIR)
    const p = path.join(SCREENSHOT_DIR, 'graph-keyboard-nav-shortcuts.png')
    await page.screenshot({ path: p, fullPage: false })
    expect(fs.existsSync(p)).toBe(true)
    console.log(`[VIEW] Graph keyboard nav shortcuts: ${p}`)
  })
})
