/**
 * Headless Playwright smoke test — Settings surface (#1206)
 *
 * Verifies:
 *   1. /settings route renders all 6 section headings
 *   2. Theme toggle updates the <html> class immediately
 *   3. MCP config tab switcher shows a non-empty code block for each tool
 *   4. Screenshot saved for visual verification
 *
 * Usage:
 *   node dashboard/smoke-settings-1206.mjs [BASE_URL]
 *   node dashboard/smoke-settings-1206.mjs http://localhost:47274
 */

import { chromium } from 'playwright'
import { writeFileSync, mkdirSync } from 'fs'
import { join } from 'path'

const BASE = process.argv[2] ?? 'http://localhost:47274'
const SCREENSHOT_DIR = 'dashboard/e2e-screenshots'
const TIMEOUT = 15_000

let browser, page
let passed = 0, failed = 0

async function check(label, fn) {
  try {
    await fn()
    console.log(`  ✓ ${label}`)
    passed++
  } catch (e) {
    console.error(`  ✗ ${label}: ${e.message}`)
    failed++
  }
}

async function main() {
  console.log(`\nSettings smoke — ${BASE}\n`)
  browser = await chromium.launch({ headless: true })
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  page = await ctx.newPage()

  // ── Navigate to /settings ──────────────────────────────────────────────────
  await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded', timeout: TIMEOUT })
  await page.waitForTimeout(800)

  // ── 1. All 6 sections are present ─────────────────────────────────────────
  const sections = [
    'General',
    'Updates',
    'MCP Configuration',
    'Telemetry',
    'Performance',
    'Logs',
  ]
  for (const title of sections) {
    await check(`Section "${title}" visible`, async () => {
      const el = page.getByText(title, { exact: true }).first()
      await el.waitFor({ state: 'visible', timeout: 5_000 })
    })
  }

  // ── 2. Theme switcher — toggle to dark, check <html> class ────────────────
  await check('Theme buttons present', async () => {
    const darkBtn = page.getByRole('button', { name: /dark/i }).first()
    await darkBtn.waitFor({ state: 'visible', timeout: 5_000 })
    await darkBtn.click()
    await page.waitForTimeout(300)
    const htmlClass = await page.evaluate(() => document.documentElement.className)
    if (!htmlClass.includes('dark')) {
      throw new Error(`Expected html.dark class after clicking Dark; got: "${htmlClass}"`)
    }
    // Restore to light
    const lightBtn = page.getByRole('button', { name: /light/i }).first()
    await lightBtn.click()
    await page.waitForTimeout(300)
  })

  // ── 3. MCP section — open and verify code block for each tool ─────────────
  // Open MCP section by clicking its header
  const mcpHeader = page.getByText('MCP Configuration', { exact: true })
  await check('MCP section opens', async () => {
    await mcpHeader.click()
    await page.waitForTimeout(300)
    const pre = page.locator('pre').first()
    await pre.waitFor({ state: 'visible', timeout: 5_000 })
    const content = await pre.textContent()
    if (!content || content.trim().length < 10) {
      throw new Error('MCP config block is empty')
    }
  })

  await check('MCP — Cursor tab shows config', async () => {
    const cursorBtn = page.getByRole('button', { name: /cursor/i }).first()
    await cursorBtn.click()
    await page.waitForTimeout(200)
    const pre = page.locator('pre').first()
    const content = await pre.textContent()
    if (!content?.includes('mcp')) throw new Error('Cursor config block missing "mcp" key')
  })

  await check('MCP — Windsurf tab shows config', async () => {
    const windsurfBtn = page.getByRole('button', { name: /windsurf/i }).first()
    await windsurfBtn.click()
    await page.waitForTimeout(200)
    const pre = page.locator('pre').first()
    const content = await pre.textContent()
    if (!content?.includes('windsurf')) throw new Error('Windsurf config block missing "windsurf" key')
  })

  // ── 4. Telemetry toggle ─────────────────────────────────────────────────────
  await check('Telemetry section opens and toggle present', async () => {
    const telHeader = page.getByText('Telemetry', { exact: true })
    await telHeader.click()
    await page.waitForTimeout(200)
    const toggle = page.getByRole('switch').first()
    await toggle.waitFor({ state: 'visible', timeout: 5_000 })
  })

  // ── Screenshot ─────────────────────────────────────────────────────────────
  mkdirSync(SCREENSHOT_DIR, { recursive: true })
  const shot = join(SCREENSHOT_DIR, 'settings-1206.png')
  await page.screenshot({ path: shot, fullPage: false })
  console.log(`\n  📸 Screenshot: ${shot}`)

  // ── Summary ────────────────────────────────────────────────────────────────
  console.log(`\n  ${passed} passed, ${failed} failed\n`)
  await browser.close()
  process.exit(failed > 0 ? 1 : 0)
}

main().catch((e) => {
  console.error('Fatal:', e)
  if (browser) browser.close()
  process.exit(1)
})
