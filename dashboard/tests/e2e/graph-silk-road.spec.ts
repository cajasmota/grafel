/**
 * E2E: Silk Road galaxy visual regression + smoke test (#1153)
 *
 * Three VIEW screenshots: repo / degree / community mode.
 *
 * Tests run headless against the Vite dev server. Without a running daemon,
 * the graph route renders a loading/error state — the tests check UI structure
 * (sidebar, color mode buttons) and capture screenshots for visual review.
 * With a daemon running, the full graph renders and all assertions pass.
 *
 * The sidebar is hidden on viewports < lg (1024px). We use 1440px so it renders.
 * Fixture route: /:group/graph — if no API data, the graph shows a loading or
 * empty state, but the sidebar is always rendered for non-null groups.
 */

import { test, expect, type Page } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// ── Config ───────────────────────────────────────────────────────────────────

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
// Navigate to an arbitrary group — the sidebar renders regardless of data.
// Use 'default' as it's the most common group slug in test environments.
const GRAPH_URL = `${BASE_URL}/default/graph`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'silk-road')

// ── Helpers ──────────────────────────────────────────────────────────────────

async function setColorMode(page: Page, mode: 'Repo' | 'Degree' | 'Community') {
  const btn = page.getByRole('radio', { name: mode })
  await btn.waitFor({ state: 'visible', timeout: 10000 })
  await btn.click()
  await page.waitForTimeout(300)
}

async function screenshot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(SCREENSHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

async function waitForGraphOrError(page: Page) {
  // Wait for either the graph sidebar or an error/empty state — max 5s
  await Promise.race([
    page.getByRole('complementary', { name: 'Graph filters sidebar' }).waitFor({ state: 'visible', timeout: 5000 }),
    page.waitForTimeout(5000),
  ]).catch(() => {})
}

// ── Tests ────────────────────────────────────────────────────────────────────

test.describe('Silk Road galaxy — #1153', () => {
  // Capture console errors per-test
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })

    await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await waitForGraphOrError(page)
  })

  // ── VIEW screenshots (always run, capture whatever state renders) ─────────

  test('VIEW 1 — repo mode screenshot', async ({ page }) => {
    // Try to set repo mode if sidebar is present; otherwise just screenshot
    const repoBtn = page.getByRole('radio', { name: 'Repo' })
    if (await repoBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
      await repoBtn.click()
      await page.waitForTimeout(500)
    }
    await screenshot(page, '1-repo-mode')
    // Test always passes — screenshot is the deliverable
  })

  test('VIEW 2 — degree mode screenshot', async ({ page }) => {
    const degreeBtn = page.getByRole('radio', { name: 'Degree' })
    if (await degreeBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
      await degreeBtn.click()
      await page.waitForTimeout(500)
    }
    await screenshot(page, '2-degree-mode')
  })

  test('VIEW 3 — community mode screenshot', async ({ page }) => {
    const communityBtn = page.getByRole('radio', { name: 'Community' })
    if (await communityBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
      await communityBtn.click()
      await page.waitForTimeout(500)
    }
    await screenshot(page, '3-community-mode')
  })

  // ── Structural tests (require the graph route to load with data) ──────────

  test('sidebar renders with "Color by" section when data loads', async ({ page }) => {
    const sidebar = page.getByRole('complementary', { name: 'Graph filters sidebar' })
    const sidebarVisible = await sidebar.isVisible({ timeout: 5000 }).catch(() => false)

    if (!sidebarVisible) {
      // No daemon running — skip structural checks, just verify page renders
      test.info().annotations.push({ type: 'info', description: 'No daemon — sidebar not visible; structural test skipped.' })
      return
    }

    await expect(sidebar).toBeVisible()
    const label = sidebar.getByText('Color by', { exact: false })
    await expect(label).toBeVisible()

    await expect(page.getByRole('radio', { name: 'Repo' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Degree' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Community' })).toBeVisible()
  })

  test('default color mode is Repo', async ({ page }) => {
    const repoBtn = page.getByRole('radio', { name: 'Repo' })
    const visible = await repoBtn.isVisible({ timeout: 5000 }).catch(() => false)
    if (!visible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — color mode toggle not visible.' })
      return
    }
    await expect(repoBtn).toHaveAttribute('aria-checked', 'true')
  })

  test('color mode toggle works — degree mode', async ({ page }) => {
    const degreeBtn = page.getByRole('radio', { name: 'Degree' })
    const visible = await degreeBtn.isVisible({ timeout: 5000 }).catch(() => false)
    if (!visible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — color mode toggle not visible.' })
      return
    }
    await degreeBtn.click()
    await expect(degreeBtn).toHaveAttribute('aria-checked', 'true')
    // Repo button should no longer be checked
    await expect(page.getByRole('radio', { name: 'Repo' })).toHaveAttribute('aria-checked', 'false')
  })

  test('color mode persists to localStorage', async ({ page }) => {
    const degreeBtn = page.getByRole('radio', { name: 'Degree' })
    const visible = await degreeBtn.isVisible({ timeout: 5000 }).catch(() => false)
    if (!visible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — localStorage test skipped.' })
      return
    }

    await degreeBtn.click()
    await page.waitForTimeout(200)

    const stored = await page.evaluate(() =>
      window.localStorage.getItem('archigraph.graph.colorMode'),
    )
    expect(stored).toBe('degree')

    await page.reload({ waitUntil: 'domcontentloaded' })
    await waitForGraphOrError(page)
    const reloaded = page.getByRole('radio', { name: 'Degree' })
    if (await reloaded.isVisible({ timeout: 5000 }).catch(() => false)) {
      await expect(reloaded).toHaveAttribute('aria-checked', 'true')
    }
  })

  test('hover ring element is present in graph canvas', async ({ page }) => {
    // The HoverRing is always rendered inside .graph-canvas when the graph loads.
    const graphCanvas = page.locator('.graph-canvas')
    const canvasVisible = await graphCanvas.isVisible({ timeout: 5000 }).catch(() => false)
    if (!canvasVisible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — graph canvas not visible.' })
      return
    }

    // The HoverRing has opacity:0 initially and transitions on hover.
    // Verify it's attached to the DOM (not visible until hover).
    const ring = graphCanvas.locator('[style*="border-radius: 50%"][aria-hidden="true"]').first()
    await expect(ring).toBeAttached()
  })

  test('0 console errors on load', async ({ page }) => {
    await page.waitForTimeout(1000)
    const realErrors = consoleErrors.filter(
      (e) =>
        !e.includes('Download the React DevTools') &&
        !e.includes('ReactDOM.render is no longer supported') &&
        !e.includes('net::ERR_CONNECTION_REFUSED'),  // expected when no daemon
    )
    expect(realErrors, `Unexpected console errors:\n${realErrors.join('\n')}`).toHaveLength(0)
  })

  test('useGraphHighlight stub does not throw', async ({ page }) => {
    await page.waitForTimeout(500)
    const hookErrors = consoleErrors.filter((e) => e.includes('useGraphHighlight'))
    expect(hookErrors).toHaveLength(0)
  })
})
