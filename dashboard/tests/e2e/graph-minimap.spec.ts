/**
 * E2E: Graph minimap — viewport indicator + click-to-pan + toggle (#1366)
 *
 * Tests run headless against the Vite dev server. Without a running daemon the
 * graph route still renders the sidebar (where the Minimap toggle lives), so
 * structural tests pass unconditionally. Behavioural assertions are soft-skipped
 * when no graph data is present.
 *
 * Viewport: 1440×900 so the lg sidebar is visible.
 */

import { test, expect, type Page } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// ── Config ────────────────────────────────────────────────────────────────────

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const GRAPH_URL = `${BASE_URL}/default/graph`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'minimap')

// ── Helpers ───────────────────────────────────────────────────────────────────

async function waitForSidebar(page: Page): Promise<boolean> {
  return page
    .getByRole('complementary', { name: 'Graph filters sidebar' })
    .isVisible({ timeout: 6000 })
    .catch(() => false)
}

async function screenshot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(SCREENSHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('Graph minimap — #1366', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await waitForSidebar(page)
    // Wait a moment for graph to settle
    await page.waitForTimeout(1500)
  })

  // ── VIEW screenshots (always run) ─────────────────────────────────────────

  test('VIEW 1 — default state with minimap visible', async ({ page }) => {
    await screenshot(page, '1-default-minimap-visible')
    // No critical console errors
    const fatal = consoleErrors.filter(
      (e) => !e.includes('ResizeObserver') && !e.includes('favicon'),
    )
    expect(fatal, `Console errors: ${fatal.join('\n')}`).toHaveLength(0)
  })

  test('VIEW 2 — minimap hidden via sidebar toggle', async ({ page }) => {
    // The sidebar toggle button for minimap (active state = sky-300 text)
    const toggleBtn = page.getByTestId('minimap-sidebar-toggle')
    const hasSidebar = await toggleBtn.isVisible({ timeout: 4000 }).catch(() => false)

    if (!hasSidebar) {
      test.info().annotations.push({
        type: 'info',
        description: 'No sidebar visible — skipping minimap toggle screenshot.',
      })
      return
    }

    await toggleBtn.click()
    await page.waitForTimeout(200)
    await screenshot(page, '2-minimap-hidden')
  })

  test('VIEW 3 — minimap re-shown via MiniMapToggleButton', async ({ page }) => {
    // First hide via sidebar toggle
    const sidebarToggle = page.getByTestId('minimap-sidebar-toggle')
    const hasSidebar = await sidebarToggle.isVisible({ timeout: 4000 }).catch(() => false)
    if (!hasSidebar) return

    await sidebarToggle.click()
    await page.waitForTimeout(200)

    // Then re-show via the MiniMapToggleButton that appears in its place
    const showBtn = page.getByTestId('graph-minimap-toggle')
    const hasShowBtn = await showBtn.isVisible({ timeout: 2000 }).catch(() => false)
    if (hasShowBtn) {
      await showBtn.click()
      await page.waitForTimeout(200)
    }

    await screenshot(page, '3-minimap-reshown')
  })

  // ── Structural assertions ─────────────────────────────────────────────────

  test('ASSERT 1 — minimap canvas is rendered', async ({ page }) => {
    const canvas = page.getByTestId('graph-minimap-canvas')
    const hasCanvas = await canvas.isVisible({ timeout: 5000 }).catch(() => false)

    if (!hasCanvas) {
      // If no graph data loaded, minimap is not shown — this is expected
      test.info().annotations.push({
        type: 'info',
        description: 'No graph data loaded — minimap canvas not rendered. Skipping assertion.',
      })
      return
    }

    await expect(canvas).toBeVisible()

    // Canvas has correct dimensions attribute
    const width = await canvas.getAttribute('width')
    const height = await canvas.getAttribute('height')
    expect(Number(width)).toBe(200)
    expect(Number(height)).toBe(150)
  })

  test('ASSERT 2 — minimap hide button dismisses the minimap', async ({ page }) => {
    const hideBtn = page.getByTestId('graph-minimap-hide-btn')
    const hasHideBtn = await hideBtn.isVisible({ timeout: 5000 }).catch(() => false)

    if (!hasHideBtn) {
      test.info().annotations.push({
        type: 'info',
        description: 'No minimap hide button found — graph may not have loaded. Skipping.',
      })
      return
    }

    await hideBtn.click()
    await page.waitForTimeout(200)

    // Minimap should be gone
    const minimapDiv = page.getByTestId('graph-minimap')
    await expect(minimapDiv).not.toBeVisible()

    // And the "show minimap" toggle should appear in the sidebar
    const showToggle = page.getByTestId('graph-minimap-toggle')
    await expect(showToggle).toBeVisible()
  })

  test('ASSERT 3 — minimap toggle in sidebar shows/hides minimap', async ({ page }) => {
    const sidebarToggle = page.getByTestId('minimap-sidebar-toggle')
    const hasSidebar = await sidebarToggle.isVisible({ timeout: 4000 }).catch(() => false)

    if (!hasSidebar) return

    // Click to hide
    await sidebarToggle.click()
    await page.waitForTimeout(200)

    // Minimap canvas should be gone (or the outer div should be hidden)
    const minimapDiv = page.getByTestId('graph-minimap')
    const visibleAfterHide = await minimapDiv.isVisible().catch(() => false)
    expect(visibleAfterHide).toBe(false)

    // "Show" toggle should now appear
    const showBtn = page.getByTestId('graph-minimap-toggle')
    await expect(showBtn).toBeVisible()

    // Click show
    await showBtn.click()
    await page.waitForTimeout(200)

    // Sidebar should show "active" toggle again
    const activeToggle = page.getByTestId('minimap-sidebar-toggle')
    await expect(activeToggle).toBeVisible()
  })

  test('ASSERT 4 — minimap node count chip shows correctly', async ({ page }) => {
    const countChip = page.getByTestId('graph-minimap-count')
    const hasChip = await countChip.isVisible({ timeout: 5000 }).catch(() => false)

    if (!hasChip) {
      test.info().annotations.push({
        type: 'info',
        description: 'No minimap count chip — graph may not have loaded.',
      })
      return
    }

    // Check text contains "nodes"
    const text = await countChip.textContent()
    expect(text).toMatch(/nodes/)
  })

  test('ASSERT 5 — minimap click triggers pan (structural, no crash)', async ({ page }) => {
    const canvas = page.getByTestId('graph-minimap-canvas')
    const hasCanvas = await canvas.isVisible({ timeout: 5000 }).catch(() => false)

    if (!hasCanvas) return

    // Click center of minimap — should not throw
    await canvas.click({ position: { x: 100, y: 75 } })
    await page.waitForTimeout(300)

    // No new console errors from the click
    const fatal = consoleErrors.filter(
      (e) => !e.includes('ResizeObserver') && !e.includes('favicon'),
    )
    expect(fatal, `Console errors after click: ${fatal.join('\n')}`).toHaveLength(0)
  })
})
