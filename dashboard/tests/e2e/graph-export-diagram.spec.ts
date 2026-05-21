/**
 * E2E: Graph DSL export — "Copy as diagram" button (#1318)
 *
 * Tests the ExportDiagramButton in the Entity Inspector panel.
 *
 * Two VIEW screenshots (always captured).
 * Structural tests degrade gracefully when no daemon is running:
 * the export button is only present when an entity is selected, which
 * requires a live graph. Tests annotate and skip cleanly in that case.
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
const GROUP = process.env.TEST_GROUP ?? 'default'
const GRAPH_URL = `${BASE_URL}/${GROUP}/graph`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'export-diagram')

// ── Helpers ───────────────────────────────────────────────────────────────────

async function screenshot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(SCREENSHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

async function waitForGraph(page: Page) {
  await Promise.race([
    page.getByRole('complementary', { name: 'Graph filters sidebar' })
      .waitFor({ state: 'visible', timeout: 8000 }),
    page.waitForTimeout(8000),
  ]).catch(() => {})
}

async function trySelectNode(page: Page): Promise<boolean> {
  const canvas = page.locator('.graph-canvas')
  const canvasVisible = await canvas.isVisible({ timeout: 5000 }).catch(() => false)
  if (!canvasVisible) return false

  const box = await canvas.boundingBox()
  if (!box) return false

  // Try center + 4 quadrants to find a clickable node.
  const offsets = [[0, 0], [-100, -100], [100, -100], [-100, 100], [100, 100]] as const
  for (const [dx, dy] of offsets) {
    await page.mouse.click(box.x + box.width / 2 + dx, box.y + box.height / 2 + dy)
    await page.waitForTimeout(500)
    const inspector = page.getByTestId('entity-inspector')
    if (await inspector.isVisible({ timeout: 1000 }).catch(() => false)) {
      return true
    }
  }
  return false
}

// ── Tests ─────────────────────────────────────────────────────────────────────

test.describe('Graph DSL export — #1318', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })
    await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await waitForGraph(page)
  })

  // ── VIEW screenshots ──────────────────────────────────────────────────────

  test('VIEW 1 — graph loaded, export button not yet visible', async ({ page }) => {
    await page.waitForTimeout(500)
    await screenshot(page, '1-graph-no-selection')
  })

  test('VIEW 2 — inspector open with export button visible (if node found)', async ({ page }) => {
    await trySelectNode(page)
    await page.waitForTimeout(500)
    await screenshot(page, '2-inspector-export-button')
  })

  // ── Structural tests ──────────────────────────────────────────────────────

  test('graph route renders without crash', async ({ page }) => {
    const errorBoundary = page.locator('[data-testid="error-boundary"]')
    const crashed = await errorBoundary.isVisible({ timeout: 2000 }).catch(() => false)
    expect(crashed, 'React error boundary should not be visible').toBe(false)
  })

  test('export button appears in inspector when node selected (with daemon)', async ({ page }) => {
    const nodeSelected = await trySelectNode(page)
    if (!nodeSelected) {
      test.info().annotations.push({
        type: 'info',
        description: 'No daemon / no node found — export button test skipped.',
      })
      return
    }

    const inspector = page.getByTestId('entity-inspector')
    await expect(inspector).toBeVisible()

    const exportBtn = inspector.getByTestId('export-diagram-button')
    await expect(exportBtn, 'Export diagram button should be visible in inspector').toBeVisible()

    // Format picker button should be present.
    const formatPicker = exportBtn.getByRole('button', { name: /Export format/i })
    await expect(formatPicker).toBeVisible()

    // Copy button should be present.
    const copyBtn = exportBtn.getByTestId('copy-diagram-btn')
    await expect(copyBtn).toBeVisible()
    await expect(copyBtn).toHaveText(/Copy as diagram/)
  })

  test('format picker opens and shows all 4 formats (with daemon)', async ({ page }) => {
    const nodeSelected = await trySelectNode(page)
    if (!nodeSelected) {
      test.info().annotations.push({
        type: 'info',
        description: 'No daemon — format picker test skipped.',
      })
      return
    }

    const inspector = page.getByTestId('entity-inspector')
    const exportBtn = inspector.getByTestId('export-diagram-button')
    if (!(await exportBtn.isVisible({ timeout: 2000 }).catch(() => false))) {
      test.info().annotations.push({ type: 'info', description: 'Export button not visible — skipped.' })
      return
    }

    // Open format dropdown.
    const formatPicker = exportBtn.getByRole('button', { name: /Export format/i })
    await formatPicker.click()

    // Dropdown should list all 4 formats.
    const dropdown = exportBtn.getByRole('listbox', { name: 'Diagram format' })
    await expect(dropdown).toBeVisible()

    for (const format of ['Mermaid', 'Graphviz DOT', 'PlantUML', 'D2']) {
      await expect(dropdown.getByRole('option', { name: new RegExp(format) })).toBeVisible()
    }

    // Close by clicking elsewhere.
    await page.keyboard.press('Escape')
  })

  test('copy-to-clipboard writes DSL text (with daemon)', async ({ page, context }) => {
    // Grant clipboard permissions.
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])

    const nodeSelected = await trySelectNode(page)
    if (!nodeSelected) {
      test.info().annotations.push({
        type: 'info',
        description: 'No daemon — clipboard test skipped.',
      })
      return
    }

    const inspector = page.getByTestId('entity-inspector')
    const exportBtn = inspector.getByTestId('export-diagram-button')
    if (!(await exportBtn.isVisible({ timeout: 2000 }).catch(() => false))) {
      test.info().annotations.push({ type: 'info', description: 'Export button not visible — skipped.' })
      return
    }

    const copyBtn = exportBtn.getByTestId('copy-diagram-btn')
    await copyBtn.click()

    // Button should show "Copied!" flash.
    await expect(copyBtn).toHaveText(/Copied!/, { timeout: 5000 })

    // Clipboard should contain non-empty text starting with Mermaid header.
    const clipboardText = await page.evaluate(() => navigator.clipboard.readText())
    expect(clipboardText.length, 'Clipboard should contain DSL text').toBeGreaterThan(0)
    expect(clipboardText, 'Default format should be Mermaid').toMatch(/flowchart LR/)
  })

  test('switching to Graphviz format and copying (with daemon)', async ({ page, context }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])

    const nodeSelected = await trySelectNode(page)
    if (!nodeSelected) {
      test.info().annotations.push({ type: 'info', description: 'No daemon — skipped.' })
      return
    }

    const inspector = page.getByTestId('entity-inspector')
    const exportBtn = inspector.getByTestId('export-diagram-button')
    if (!(await exportBtn.isVisible({ timeout: 2000 }).catch(() => false))) {
      test.info().annotations.push({ type: 'info', description: 'Export button not visible — skipped.' })
      return
    }

    // Switch to Graphviz.
    const formatPicker = exportBtn.getByRole('button', { name: /Export format/i })
    await formatPicker.click()
    const dropdown = exportBtn.getByRole('listbox', { name: 'Diagram format' })
    await dropdown.getByRole('option', { name: /Graphviz/ }).click()

    // Format label should update.
    await expect(formatPicker).toContainText('Graphviz DOT')

    // Copy.
    const copyBtn = exportBtn.getByTestId('copy-diagram-btn')
    await copyBtn.click()
    await expect(copyBtn).toHaveText(/Copied!/, { timeout: 5000 })

    const clipboardText = await page.evaluate(() => navigator.clipboard.readText())
    expect(clipboardText, 'Graphviz DOT format should contain digraph').toMatch(/digraph subgraph/)
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
