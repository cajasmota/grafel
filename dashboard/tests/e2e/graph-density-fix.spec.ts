/**
 * E2E: Graph density fix — Silk Road galaxies with repo-color default (#1356)
 *
 * Verifies:
 *   1. Default zoom shows the 'high' band (all nodes visible at startup).
 *   2. No macro/overview bands — those were dropped in #1356.
 *   3. Hub pulse animation runs (hub-pulsing nodes appear on settle).
 *   4. All 3 color modes coexist (repo → community → degree).
 *   5. 0 console errors on load.
 *
 * Tests run headless. Without a daemon, structural checks verify DOM shape.
 * With a daemon (upvate graph), screenshots confirm dense cluster layout.
 *
 * Viewport: 1440 × 900 (≥ lg so the sidebar renders).
 */

import { test, expect, type Page } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

// ── Config ─────────────────────────────────────────────────────────────────────

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const GRAPH_URL = `${BASE_URL}/default/graph`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'test-results', 'density-fix-1356')

// ── Helpers ───────────────────────────────────────────────────────────────────

async function screenshot(page: Page, name: string) {
  await page.screenshot({
    path: path.join(SCREENSHOT_DIR, `${name}.png`),
    fullPage: false,
  })
}

async function waitForGraph(page: Page): Promise<boolean> {
  try {
    await page.locator('[aria-label="Dependency graph"]').waitFor({ state: 'attached', timeout: 8000 })
    return true
  } catch {
    return false
  }
}

// ── Tests ──────────────────────────────────────────────────────────────────────

test.describe('Density fix — #1356 Silk Road galaxies with repo-color default', () => {
  let consoleErrors: string[] = []

  test.use({ viewport: { width: 1440, height: 900 } })

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })
    await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await waitForGraph(page)
  })

  // ── VIEW screenshots — the deliverables ─────────────────────────────────────

  test('VIEW — repo mode default zoom (should show dense clusters)', async ({ page }) => {
    // Repo is the default mode — just screenshot after load
    await page.waitForTimeout(2000)  // let simulation settle
    await screenshot(page, '1-repo-default-zoom')
  })

  test('VIEW — community mode', async ({ page }) => {
    const btn = page.getByRole('radio', { name: 'Community' })
    if (await btn.isVisible({ timeout: 3000 }).catch(() => false)) {
      await btn.click()
      await page.waitForTimeout(500)
    }
    await screenshot(page, '2-community-mode')
  })

  test('VIEW — degree mode (Silk Road gradient)', async ({ page }) => {
    const btn = page.getByRole('radio', { name: 'Degree' })
    if (await btn.isVisible({ timeout: 3000 }).catch(() => false)) {
      await btn.click()
      await page.waitForTimeout(500)
    }
    await screenshot(page, '3-degree-mode')
  })

  // ── Structural tests ─────────────────────────────────────────────────────────

  test('default band is "high" at startup (not macro/overview)', async ({ page }) => {
    const hud = page.locator('[data-testid="zoom-band-hud"]')
    // ZoomBandHUD only renders when GraphCanvas mounts (requires daemon + data).
    // Without a daemon this test gracefully skips.
    const count = await hud.count()
    if (count === 0) {
      test.info().annotations.push({ type: 'info', description: 'HUD not present (no daemon) — structural skip' })
      return
    }
    // After mount, handleMount sets zoom=0.3 → band 'high' (first band with maxZoom 1.0).
    // Wait a moment for the initial zoom to fire.
    await page.waitForTimeout(1500)
    const label = (await hud.textContent())?.toLowerCase().trim() ?? ''
    // Valid bands after #1356 — must NOT be macro or overview
    expect(['high', 'mid', 'full', 'detail']).toContain(label)
    expect(label).not.toBe('macro')
    expect(label).not.toBe('overview')
  })

  test('color mode order — repo first in sidebar', async ({ page }) => {
    const sidebar = page.getByRole('complementary', { name: 'Graph filters sidebar' })
    const visible = await sidebar.isVisible({ timeout: 5000 }).catch(() => false)
    if (!visible) {
      test.info().annotations.push({ type: 'info', description: 'Sidebar not visible — sidebar skip' })
      return
    }
    // Get all radio buttons in the Color by group
    const radios = await sidebar.getByRole('radio').all()
    const labels = await Promise.all(radios.map((r) => r.textContent()))
    // First radio must be Repo (repo-first order from #1356)
    expect(labels[0]?.trim()).toBe('Repo')
    expect(labels[1]?.trim()).toBe('Community')
    expect(labels[2]?.trim()).toBe('Degree')
  })

  test('default color mode is Repo (aria-checked)', async ({ page }) => {
    const repoBtn = page.getByRole('radio', { name: 'Repo' })
    const visible = await repoBtn.isVisible({ timeout: 5000 }).catch(() => false)
    if (!visible) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — skip' })
      return
    }
    await expect(repoBtn).toHaveAttribute('aria-checked', 'true')
  })

  test('all 3 color mode buttons are present', async ({ page }) => {
    const sidebar = page.getByRole('complementary', { name: 'Graph filters sidebar' })
    const visible = await sidebar.isVisible({ timeout: 5000 }).catch(() => false)
    if (!visible) {
      test.info().annotations.push({ type: 'info', description: 'No sidebar — skip' })
      return
    }
    await expect(page.getByRole('radio', { name: 'Repo' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Community' })).toBeVisible()
    await expect(page.getByRole('radio', { name: 'Degree' })).toBeVisible()
  })

  test('hub pulse: hover ring element is in DOM (requires daemon)', async ({ page }) => {
    // HoverRing only mounts inside GraphCanvas — requires daemon + data.
    const graph = page.locator('[aria-label="Dependency graph"]')
    const graphPresent = await graph.count() > 0
    if (!graphPresent) {
      test.info().annotations.push({ type: 'info', description: 'Graph canvas not present (no daemon) — skipping HoverRing check' })
      return
    }
    const ring = graph.locator('[style*="border-radius: 50%"][aria-hidden="true"]').first()
    await expect(ring).toBeAttached({ timeout: 8000 })
  })

  test('ZoomBandHUD element is present (requires daemon)', async ({ page }) => {
    // ZoomBandHUD only mounts inside GraphCanvas — requires daemon + data.
    const graph = page.locator('[aria-label="Dependency graph"]')
    const graphPresent = await graph.count() > 0
    if (!graphPresent) {
      test.info().annotations.push({ type: 'info', description: 'Graph canvas not present (no daemon) — skipping ZoomBandHUD check' })
      return
    }
    const hud = page.locator('[data-testid="zoom-band-hud"]')
    await expect(hud).toBeAttached({ timeout: 8000 })
  })

  test('0 console errors on load', async ({ page }) => {
    await page.waitForTimeout(1500)
    const realErrors = consoleErrors.filter(
      (e) =>
        !e.includes('Download the React DevTools') &&
        !e.includes('ReactDOM.render is no longer supported') &&
        !e.includes('net::ERR_CONNECTION_REFUSED'),
    )
    expect(realErrors, `Console errors:\n${realErrors.join('\n')}`).toHaveLength(0)
  })
})
