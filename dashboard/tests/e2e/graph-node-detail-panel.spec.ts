/**
 * E2E: Graph node detail panel — rich entity inspector (#1240)
 *
 * Two VIEW screenshots; structural tests degrade gracefully when no daemon
 * is running (inspector only renders with live graph data, so we test the
 * empty-state and sidebar structure in daemon-absent mode).
 *
 * Headless only. 0 console errors expected.
 */

import { test, expect, type Page } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// ── Config ────────────────────────────────────────────────────────────────────

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
// Use the group name that is most likely to have data — falls back gracefully.
const GROUP = process.env.TEST_GROUP ?? 'default'
const GRAPH_URL = `${BASE_URL}/${GROUP}/graph`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'node-detail-panel')

// ── Helpers ───────────────────────────────────────────────────────────────────

async function screenshot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(SCREENSHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

async function waitForGraphOrError(page: Page) {
  await Promise.race([
    page.getByRole('complementary', { name: 'Graph filters sidebar' })
      .waitFor({ state: 'visible', timeout: 8000 }),
    page.waitForTimeout(8000),
  ]).catch(() => {})
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('Graph node detail panel — #1240', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })

    await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await waitForGraphOrError(page)
  })

  // ── VIEW screenshots (always capture, regardless of data) ─────────────────

  test('VIEW 1 — graph loaded, inspector closed', async ({ page }) => {
    await page.waitForTimeout(500)
    await screenshot(page, '1-graph-inspector-closed')
    // Screenshot is the deliverable; test always passes.
  })

  test('VIEW 2 — inspector panel visible (if node can be clicked)', async ({ page }) => {
    const canvas = page.locator('.graph-canvas')
    const canvasVisible = await canvas.isVisible({ timeout: 5000 }).catch(() => false)

    if (canvasVisible) {
      // Try clicking the canvas center to see if a node gets selected.
      const box = await canvas.boundingBox()
      if (box) {
        await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2)
        await page.waitForTimeout(800)
      }
    }

    await screenshot(page, '2-inspector-panel-state')
    // Screenshot is the deliverable; test always passes.
  })

  // ── Structural tests ──────────────────────────────────────────────────────

  test('graph route renders without crash', async ({ page }) => {
    // At minimum the page must not show a React error boundary.
    const errorBoundary = page.locator('[data-testid="error-boundary"]')
    const crashed = await errorBoundary.isVisible({ timeout: 2000 }).catch(() => false)
    expect(crashed, 'React error boundary should not be visible').toBe(false)
  })

  test('inspector panel renders when a node is selected (with data)', async ({ page }) => {
    const canvas = page.locator('.graph-canvas')
    const canvasVisible = await canvas.isVisible({ timeout: 5000 }).catch(() => false)

    if (!canvasVisible) {
      test.info().annotations.push({
        type: 'info',
        description: 'No daemon — graph canvas not visible; inspector test skipped.',
      })
      return
    }

    // Click the canvas center — if a node is there it will be selected.
    const box = await canvas.boundingBox()
    if (!box) return

    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2)
    await page.waitForTimeout(1000)

    const inspector = page.getByTestId('entity-inspector')
    const inspectorVisible = await inspector.isVisible({ timeout: 3000 }).catch(() => false)

    if (!inspectorVisible) {
      // No node was at the click point — try a few more spots.
      for (const [dx, dy] of [[-100, -100], [100, -100], [-100, 100], [100, 100]]) {
        await page.mouse.click(box.x + box.width / 2 + dx, box.y + box.height / 2 + dy)
        await page.waitForTimeout(500)
        const visible = await inspector.isVisible({ timeout: 1000 }).catch(() => false)
        if (visible) break
      }
    }

    const isOpen = await inspector.isVisible({ timeout: 2000 }).catch(() => false)
    if (!isOpen) {
      test.info().annotations.push({
        type: 'info',
        description: 'Could not click a node on the canvas — inspector test skipped.',
      })
      return
    }

    // Verify inspector has the expected structure.
    await expect(inspector).toBeVisible()

    // Should have a header with entity name.
    const heading = inspector.locator('h2')
    await expect(heading).toBeVisible()
    await expect(heading).not.toHaveText('Inspector')

    // Should have at least one collapsible section.
    const sections = inspector.locator('button[aria-expanded]')
    const sectionCount = await sections.count()
    expect(sectionCount, 'Inspector should have at least 1 collapsible section').toBeGreaterThan(0)

    // Should have copy-ID button.
    const copyBtn = inspector.getByRole('button', { name: 'Copy entity ID' })
    await expect(copyBtn).toBeVisible()

    // Should have pin button.
    const pinBtn = inspector.getByRole('button', { name: /[Pp]in/ })
    await expect(pinBtn).toBeVisible()

    // Should have close button.
    const closeBtn = inspector.getByRole('button', { name: 'Close inspector' })
    await expect(closeBtn).toBeVisible()
  })

  test('inspector closes on close button click', async ({ page }) => {
    const canvas = page.locator('.graph-canvas')
    const canvasVisible = await canvas.isVisible({ timeout: 5000 }).catch(() => false)

    if (!canvasVisible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — test skipped.' })
      return
    }

    const box = await canvas.boundingBox()
    if (!box) return

    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2)
    await page.waitForTimeout(800)

    const inspector = page.getByTestId('entity-inspector')
    const open = await inspector.isVisible({ timeout: 2000 }).catch(() => false)
    if (!open) {
      test.info().annotations.push({ type: 'info', description: 'No node selected — test skipped.' })
      return
    }

    const closeBtn = inspector.getByRole('button', { name: 'Close inspector' })
    await closeBtn.click()
    await page.waitForTimeout(300)

    await expect(inspector).not.toBeVisible()
  })

  test('edge filter input is accessible (with data)', async ({ page }) => {
    const canvas = page.locator('.graph-canvas')
    const canvasVisible = await canvas.isVisible({ timeout: 5000 }).catch(() => false)

    if (!canvasVisible) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — test skipped.' })
      return
    }

    const box = await canvas.boundingBox()
    if (!box) return

    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2)
    await page.waitForTimeout(800)

    const inspector = page.getByTestId('entity-inspector')
    const open = await inspector.isVisible({ timeout: 2000 }).catch(() => false)
    if (!open) {
      test.info().annotations.push({ type: 'info', description: 'No node selected — test skipped.' })
      return
    }

    // Edge filter is only rendered when there are edges — it may or may not appear.
    const filterInput = inspector.getByLabel('Filter edges by name')
    const filterVisible = await filterInput.isVisible({ timeout: 1000 }).catch(() => false)

    if (filterVisible) {
      // Verify it accepts input.
      await filterInput.fill('test')
      await expect(filterInput).toHaveValue('test')
      await filterInput.clear()
    }
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
})
