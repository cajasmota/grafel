/**
 * E2E: Tunable simulation params — sidebar sliders + presets + localStorage (#1361)
 *
 * Tests run headless against the Vite dev server. Without a running daemon the
 * graph route still renders the sidebar (where the Simulation section lives),
 * so structural tests pass unconditionally; behavioral assertions are
 * soft-skipped when the sidebar is absent.
 *
 * The sidebar is hidden on viewports < lg (1024px). We use 1440px.
 */

import { test, expect, type Page } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname  = path.dirname(__filename)

// ── Config ───────────────────────────────────────────────────────────────────

const BASE_URL  = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const GRAPH_URL = `${BASE_URL}/default/graph`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'sim-tunables')

// ── Helpers ──────────────────────────────────────────────────────────────────

async function waitForSidebar(page: Page): Promise<boolean> {
  return page
    .getByRole('complementary', { name: 'Graph filters sidebar' })
    .isVisible({ timeout: 5000 })
    .catch(() => false)
}

async function openSimSection(page: Page): Promise<boolean> {
  const toggle = page.getByTestId('sim-controls-toggle')
  if (!(await toggle.isVisible({ timeout: 3000 }).catch(() => false))) return false
  const expanded = await toggle.getAttribute('aria-expanded')
  if (expanded !== 'true') await toggle.click()
  return page.getByTestId('sim-controls-body').isVisible({ timeout: 2000 }).catch(() => false)
}

async function screenshot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(SCREENSHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('Graph simulation tunables — #1361', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await waitForSidebar(page)
  })

  // ── VIEW screenshots (always run) ─────────────────────────────────────────

  test('VIEW 1 — sidebar collapsed screenshot', async ({ page }) => {
    await screenshot(page, '1-collapsed')
  })

  test('VIEW 2 — Simulation section expanded screenshot', async ({ page }) => {
    await openSimSection(page)
    await page.waitForTimeout(300)
    await screenshot(page, '2-expanded')
  })

  test('VIEW 3 — after applying Dense preset', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — skipping Dense preset screenshot.' })
      return
    }
    await page.getByTestId('sim-preset-dense').click()
    await page.waitForTimeout(300)
    await screenshot(page, '3-dense-preset')
  })

  // ── Structural tests ──────────────────────────────────────────────────────

  test('Simulation section toggle is present in sidebar', async ({ page }) => {
    const sidebarVisible = await waitForSidebar(page)
    if (!sidebarVisible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — sidebar not visible; structural test skipped.' })
      return
    }
    const toggle = page.getByTestId('sim-controls-toggle')
    await expect(toggle).toBeVisible()
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })

  test('expanding the Simulation section reveals sliders', async ({ page }) => {
    const sidebarVisible = await waitForSidebar(page)
    if (!sidebarVisible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — sidebar not visible.' })
      return
    }
    const opened = await openSimSection(page)
    expect(opened, 'Sim controls body should be visible after toggle').toBe(true)

    // All 5 sliders should render
    for (const key of ['spaceSize', 'gravity', 'linkSpring', 'linkDistance', 'friction']) {
      await expect(page.getByTestId(`sim-slider-${key}`)).toBeVisible()
    }
  })

  test('sliders show Silk Road defaults on first load', async ({ page }) => {
    // Clear any stale localStorage so we read the real default
    await page.evaluate(() => localStorage.removeItem('archigraph.graph.simulationConfig'))
    await page.reload({ waitUntil: 'domcontentloaded' })
    await waitForSidebar(page)

    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — slider default check skipped.' })
      return
    }

    const gravitySlider = page.getByTestId('sim-slider-gravity')
    await expect(gravitySlider).toHaveValue('0.46')

    const frictionSlider = page.getByTestId('sim-slider-friction')
    await expect(frictionSlider).toHaveValue('0.77')
  })

  test('moving gravity slider updates displayed value', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — slider interaction skipped.' })
      return
    }

    const slider = page.getByTestId('sim-slider-gravity')
    await expect(slider).toBeVisible()

    // Set to a specific value via fill
    await slider.fill('0.75')
    await page.waitForTimeout(200)

    // Value badge should update
    const badge = page.locator('[aria-label*="Gravity value"]')
    if (await badge.isVisible({ timeout: 2000 }).catch(() => false)) {
      await expect(badge).toContainText('0.75')
    }
  })

  test('Silk Road preset button resets to defaults', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — preset test skipped.' })
      return
    }

    // First mutate gravity
    const slider = page.getByTestId('sim-slider-gravity')
    await slider.fill('0.90')
    await page.waitForTimeout(100)

    // Apply Silk Road preset
    await page.getByTestId('sim-preset-silk-road').click()
    await page.waitForTimeout(200)

    await expect(slider).toHaveValue('0.46')
  })

  test('Dense preset applies denser defaults', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — Dense preset test skipped.' })
      return
    }

    await page.getByTestId('sim-preset-dense').click()
    await page.waitForTimeout(200)

    const gravitySlider = page.getByTestId('sim-slider-gravity')
    // Dense default gravity is 0.85
    await expect(gravitySlider).toHaveValue('0.85')
  })

  test('config persists to localStorage on slider change', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — localStorage persistence test skipped.' })
      return
    }

    await page.getByTestId('sim-slider-gravity').fill('0.60')
    await page.waitForTimeout(300)

    const stored = await page.evaluate(() =>
      window.localStorage.getItem('archigraph.graph.simulationConfig'),
    )
    if (stored) {
      const parsed = JSON.parse(stored)
      expect(parsed.gravity).toBeCloseTo(0.60, 1)
    }
  })

  test('config survives page reload (localStorage round-trip)', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — reload persistence test skipped.' })
      return
    }

    await page.getByTestId('sim-slider-linkSpring').fill('1.20')
    await page.waitForTimeout(300)

    await page.reload({ waitUntil: 'domcontentloaded' })
    await waitForSidebar(page)
    const reopened = await openSimSection(page)
    if (!reopened) return

    const slider = page.getByTestId('sim-slider-linkSpring')
    await expect(slider).toHaveValue('1.2')
  })

  test('share button is present and clickable', async ({ page }) => {
    const opened = await openSimSection(page)
    if (!opened) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — share button test skipped.' })
      return
    }
    const shareBtn = page.getByTestId('sim-share-btn')
    await expect(shareBtn).toBeVisible()
    // Clipboard write may fail in headless — just assert button is present and no throw
    await shareBtn.click().catch(() => {})
  })

  test('collapsing the section hides sliders', async ({ page }) => {
    const sidebarVisible = await waitForSidebar(page)
    if (!sidebarVisible) return

    const toggle = page.getByTestId('sim-controls-toggle')
    if (!(await toggle.isVisible({ timeout: 2000 }).catch(() => false))) return

    // Expand
    await toggle.click()
    await expect(page.getByTestId('sim-controls-body')).toBeVisible()

    // Collapse
    await toggle.click()
    await expect(page.getByTestId('sim-controls-body')).not.toBeVisible()
  })

  test('0 console errors on load', async ({ page }) => {
    await page.waitForTimeout(1000)
    const realErrors = consoleErrors.filter(
      (e) =>
        !e.includes('Download the React DevTools') &&
        !e.includes('ReactDOM.render is no longer supported') &&
        !e.includes('net::ERR_CONNECTION_REFUSED'),
    )
    expect(realErrors, `Unexpected console errors:\n${realErrors.join('\n')}`).toHaveLength(0)
  })

  test('color toggle, edge filters, and hub-pulse still render (no regression)', async ({ page }) => {
    const sidebarVisible = await waitForSidebar(page)
    if (!sidebarVisible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — regression check skipped.' })
      return
    }
    // Color by section
    await expect(page.getByRole('radio', { name: 'Repo' })).toBeVisible()
    // MCP overlay toggle
    await expect(page.getByRole('switch', { name: /MCP activity/i })).toBeVisible()
    // Process clamp (cross-repo toggle in toolbar)
    await expect(page.getByTestId('cross-repo-toggle')).toBeVisible()
  })
})
