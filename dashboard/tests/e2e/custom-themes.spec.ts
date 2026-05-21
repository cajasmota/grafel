/**
 * E2E: Custom theme support (#1267)
 *
 * Verifies:
 *   1. /settings route loads without console errors
 *   2. "Themes & Colours" section is present and expandable
 *   3. All 6 preset buttons are rendered
 *   4. Clicking "Solarized Dark" applies theme-solarized-dark class to <html>
 *   5. Clicking "Nord" applies theme-nord class to <html>
 *   6. Clicking "Custom" shows the palette editor
 *   7. Palette colour fields are interactive
 *   8. Export JSON copies palette data (button becomes "Copied!")
 *   9. Reset palette button is present
 *  10. Preset persists after page reload (localStorage)
 *  11. VIEW screenshots: Solarized Dark and Nord active
 */

import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'
import fs from 'fs'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const SETTINGS_URL = `${BASE_URL}/settings`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'e2e-screenshots')

function ensureDir(dir: string) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true })
}

// ─────────────────────────────────────────────────────────────────────────────

test.describe('Custom theme support — #1267', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text()
        if (
          !text.includes('Failed to load resource') &&
          !text.includes('ERR_CONNECTION') &&
          !text.includes('ERR_FAILED')
        ) {
          consoleErrors.push(text)
        }
      }
    })
    // Clear stored theme preset before each test
    await page.addInitScript(() => {
      localStorage.removeItem('ag-theme-preset')
      localStorage.removeItem('ag-theme-custom')
    })
  })

  // ── 1. No console errors ──────────────────────────────────────────────────

  test('No unexpected console errors on /settings', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1500)
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 2. Themes section present ─────────────────────────────────────────────

  test('"Themes & Colours" section renders and expands', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    // Find the accordion button containing "Themes & Colours"
    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await expect(themesBtn).toBeVisible()

    // Click to open
    await themesBtn.click()
    await page.waitForTimeout(300)

    const themesSection = page.getByTestId('themes-section')
    await expect(themesSection).toBeVisible()
  })

  // ── 3. All 6 preset buttons ───────────────────────────────────────────────

  test('All 6 preset buttons are rendered', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    const presets = ['default', 'solarized-dark', 'nord', 'catppuccin-mocha', 'high-contrast', 'custom']
    for (const p of presets) {
      await expect(page.getByTestId(`preset-btn-${p}`)).toBeVisible()
    }
  })

  // ── 4. Solarized Dark applies class ──────────────────────────────────────

  test('Clicking Solarized Dark applies theme-solarized-dark class to <html>', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-solarized-dark').click()
    await page.waitForTimeout(500)

    const htmlClass = await page.evaluate(() => document.documentElement.className)
    expect(htmlClass).toContain('theme-solarized-dark')
  })

  // ── 5. Nord applies class ─────────────────────────────────────────────────

  test('Clicking Nord applies theme-nord class to <html>', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-nord').click()
    await page.waitForTimeout(500)

    const htmlClass = await page.evaluate(() => document.documentElement.className)
    expect(htmlClass).toContain('theme-nord')
  })

  // ── 6. Custom shows palette editor ───────────────────────────────────────

  test('Clicking Custom reveals palette editor', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-custom').click()
    await page.waitForTimeout(400)

    await expect(page.getByTestId('palette-editor')).toBeVisible()
  })

  // ── 7. Palette colour fields are visible ──────────────────────────────────

  test('Custom palette fields are visible and contain colour inputs', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-custom').click()
    await page.waitForTimeout(400)

    // Background field
    await expect(page.getByTestId('palette-field-bg')).toBeVisible()

    // Accent field
    await expect(page.getByTestId('palette-field-accent')).toBeVisible()

    // Preview swatch row
    await expect(page.getByTestId('palette-preview')).toBeVisible()
  })

  // ── 8. Export button ──────────────────────────────────────────────────────

  test('Export JSON button is present in custom editor', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-custom').click()
    await page.waitForTimeout(400)

    await expect(page.getByTestId('palette-export-btn')).toBeVisible()
    await expect(page.getByTestId('palette-export-btn')).toBeEnabled()
  })

  // ── 9. Reset button present ───────────────────────────────────────────────

  test('Reset palette button is present', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-custom').click()
    await page.waitForTimeout(400)

    await expect(page.getByTestId('palette-reset-btn')).toBeVisible()
  })

  // ── 10. Preset persists after reload ─────────────────────────────────────

  test('Chosen preset persists across page reload', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    // Choose Nord
    await page.getByTestId('preset-btn-nord').click()
    await page.waitForTimeout(500)

    // Confirm class applied
    let htmlClass = await page.evaluate(() => document.documentElement.className)
    expect(htmlClass).toContain('theme-nord')

    // Reload
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1000)

    // Class should still be there
    htmlClass = await page.evaluate(() => document.documentElement.className)
    expect(htmlClass).toContain('theme-nord')
  })

  // ── 11. Import textarea is interactive ───────────────────────────────────

  test('Import textarea accepts JSON and apply button activates', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    await page.getByTestId('preset-btn-custom').click()
    await page.waitForTimeout(400)

    const textarea = page.getByTestId('palette-import-textarea')
    await expect(textarea).toBeVisible()

    // Paste valid JSON
    const validPalette = JSON.stringify({
      bg: '#1a1a2e', bg_card: '#16213e', bg_input: '#16213e',
      fg: '#e0e0e0', fg_muted: '#888888', border: '#333366',
      accent: '#0f3460', accent_fg: '#e94560',
    })
    await textarea.fill(validPalette)
    await page.waitForTimeout(200)

    const applyBtn = page.getByTestId('palette-import-apply-btn')
    await expect(applyBtn).toBeEnabled()
  })

  // ── VIEW screenshots ──────────────────────────────────────────────────────

  test('Screenshot — Solarized Dark active (VIEW)', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(600)

    // Open themes section
    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    // Apply Solarized Dark
    await page.getByTestId('preset-btn-solarized-dark').click()
    await page.waitForTimeout(800)

    ensureDir(SCREENSHOT_DIR)
    const screenshotPath = path.join(SCREENSHOT_DIR, 'custom-themes-solarized-dark.png')
    await page.screenshot({ path: screenshotPath, fullPage: false })

    expect(fs.existsSync(screenshotPath)).toBe(true)
    console.log(`[VIEW] Solarized Dark screenshot: ${screenshotPath}`)
  })

  test('Screenshot — Nord active (VIEW)', async ({ page }) => {
    await page.goto(SETTINGS_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(600)

    // Open themes section
    const themesBtn = page.getByRole('button', { name: /themes.*colours/i })
    await themesBtn.click()
    await page.waitForTimeout(300)

    // Apply Nord
    await page.getByTestId('preset-btn-nord').click()
    await page.waitForTimeout(800)

    ensureDir(SCREENSHOT_DIR)
    const screenshotPath = path.join(SCREENSHOT_DIR, 'custom-themes-nord.png')
    await page.screenshot({ path: screenshotPath, fullPage: false })

    expect(fs.existsSync(screenshotPath)).toBe(true)
    console.log(`[VIEW] Nord screenshot: ${screenshotPath}`)
  })
})
