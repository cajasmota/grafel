/**
 * Headless Playwright smoke test for the Patterns surface (#1189).
 * Run with:  node smoke-patterns-1189.mjs
 *
 * Requires a Vite dev server running on port 5173 (VITE_USE_MOCKS=true)
 * or the archigraph daemon serving the dashboard on port 47274.
 */

import { chromium } from 'playwright'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

const BASE = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const OUT = path.join(__dirname, 'e2e-screenshots')

async function main() {
  const browser = await chromium.launch({ headless: true })
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
  })

  const errors = []
  page.on('console', (msg) => {
    if (msg.type() === 'error') errors.push(msg.text())
  })

  console.log(`[smoke] Navigating to ${BASE}/patterns/fixture-a`)
  await page.goto(`${BASE}/patterns/fixture-a`, { waitUntil: 'networkidle' })

  // Wait for the Patterns header to be visible.
  await page.waitForSelector('nav[aria-label="Surface navigation"]', { timeout: 10_000 })

  // Verify "Patterns" nav item is present.
  const patternsNav = await page.locator('a', { hasText: 'Patterns' }).count()
  if (patternsNav === 0) {
    console.error('[smoke] FAIL: "Patterns" nav item not found')
    process.exitCode = 1
  } else {
    console.log('[smoke] OK: "Patterns" nav item present')
  }

  // Screenshot 1 — full Patterns page (empty state or list).
  await page.screenshot({
    path: path.join(OUT, '1189-01-patterns-page.png'),
    fullPage: false,
  })
  console.log('[smoke] Screenshot: 1189-01-patterns-page.png')

  // Check for console errors (0 tolerance).
  const jsErrors = errors.filter(
    (e) =>
      !e.includes('favicon') &&
      !e.includes('No mock') &&   // expected in mock mode
      !e.includes('404'),
  )
  if (jsErrors.length > 0) {
    console.error('[smoke] FAIL: console errors detected:')
    jsErrors.forEach((e) => console.error(' ', e))
    process.exitCode = 1
  } else {
    console.log('[smoke] OK: 0 console errors')
  }

  await browser.close()
  console.log('[smoke] Done.')
}

main().catch((err) => {
  console.error('[smoke] Fatal:', err)
  process.exitCode = 1
})
