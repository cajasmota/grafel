/**
 * E2E: Cmd+K command palette (#1234)
 *
 * Verifies:
 *   1. Cmd+K opens the palette from anywhere
 *   2. Type 'top' → 'Topology' item appears in results
 *   3. Type 'rebuild' → 'Rebuild this group' action appears
 *   4. Enter on 'Graph' item navigates to /graph/*
 *   5. Esc closes the palette
 *   6. Chip button in header also opens palette
 *   7. 0 console errors
 *   8. Screenshots: palette open + filtered state
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

test.describe('Command palette (#1234)', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text()
        // Ignore network errors from missing daemon
        if (!text.includes('Failed to load resource') && !text.includes('ERR_CONNECTION')) {
          consoleErrors.push(text)
        }
      }
    })
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)
  })

  // ── 1. Cmd+K opens palette ────────────────────────────────────────────────

  test('Cmd+K opens the command palette', async ({ page }) => {
    // Palette should be hidden initially
    await expect(page.getByTestId('cmd-palette')).not.toBeVisible()

    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()
    await expect(page.getByTestId('cmd-palette-input')).toBeFocused()
  })

  // ── 2. Type 'top' → Topology appears ─────────────────────────────────────

  test("Type 'top' shows Topology in results", async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()

    await page.getByTestId('cmd-palette-input').fill('top')
    await expect(page.getByTestId('cmd-item-surface-topology')).toBeVisible()
  })

  // ── 3. Type 'rebuild' → Rebuild action appears ────────────────────────────

  test("Type 'rebuild' shows 'Rebuild this group' action", async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()

    await page.getByTestId('cmd-palette-input').fill('rebuild')
    await expect(page.getByTestId('cmd-item-action-rebuild')).toBeVisible()
  })

  // ── 4. Enter on Graph navigates to /graph/* ───────────────────────────────

  test('Enter on Graph item navigates to /graph route', async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()

    await page.getByTestId('cmd-palette-input').fill('graph')
    // Arrow Down to surface-graph (it should already be first / selected)
    // Press Enter
    await page.keyboard.press('Enter')

    // Palette should close
    await expect(page.getByTestId('cmd-palette')).not.toBeVisible()
    // URL should contain /graph/
    await expect(page).toHaveURL(/\/graph\//)
  })

  // ── 5. Esc closes the palette ─────────────────────────────────────────────

  test('Esc closes the command palette', async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByTestId('cmd-palette')).not.toBeVisible()
  })

  // ── 6. Header chip button opens palette ──────────────────────────────────

  test('Chip button in header opens command palette', async ({ page }) => {
    await expect(page.getByTestId('cmd-palette')).not.toBeVisible()

    const chip = page.getByTestId('cmd-palette-chip')
    await expect(chip).toBeVisible()
    await chip.click()

    await expect(page.getByTestId('cmd-palette')).toBeVisible()
  })

  // ── 7. No console errors ──────────────────────────────────────────────────

  test('No unexpected console errors during palette interaction', async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await page.waitForTimeout(300)

    await page.getByTestId('cmd-palette-input').fill('graph')
    await page.waitForTimeout(200)

    await page.keyboard.press('Escape')
    await page.waitForTimeout(300)

    expect(consoleErrors).toHaveLength(0)
  })

  // ── 8. Screenshots (VIEW) ─────────────────────────────────────────────────

  test('Screenshot — palette open empty state (VIEW)', async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()
    await page.waitForTimeout(400)

    ensureDir(SCREENSHOT_DIR)
    const p = path.join(SCREENSHOT_DIR, 'command-palette-open.png')
    await page.screenshot({ path: p, fullPage: false })
    expect(fs.existsSync(p)).toBe(true)
    console.log(`[VIEW] Command palette open: ${p}`)
  })

  test("Screenshot — palette filtered by 'flow' (VIEW)", async ({ page }) => {
    await page.keyboard.press('Meta+k')
    await expect(page.getByTestId('cmd-palette')).toBeVisible()
    await page.getByTestId('cmd-palette-input').fill('flow')
    await page.waitForTimeout(300)

    ensureDir(SCREENSHOT_DIR)
    const p = path.join(SCREENSHOT_DIR, 'command-palette-filtered.png')
    await page.screenshot({ path: p, fullPage: false })
    expect(fs.existsSync(p)).toBe(true)
    console.log(`[VIEW] Command palette filtered: ${p}`)
  })
})
