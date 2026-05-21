/**
 * Headless Playwright smoke test for the Security & Quality surface (#1330).
 * Run with:  node smoke-security-1330.mjs
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

  // ── 1. Navigate to /security/fixture-a ─────────────────────────────────────
  console.log(`[smoke] Navigating to ${BASE}/security/fixture-a`)
  await page.goto(`${BASE}/security/fixture-a`, { waitUntil: 'networkidle' })

  // Verify page loaded (nav bar present)
  await page.waitForSelector('nav[aria-label="Surface navigation"]', { timeout: 15_000 })

  // Verify Security nav entry in Operate menu
  const operateBtn = page.getByTestId('nav-operate')
  await operateBtn.click()
  await page.waitForSelector('[data-testid="nav-operate-content"]', { timeout: 5_000 })
  const securityItem = page.getByText('Security')
  const securityCount = await securityItem.count()
  if (securityCount === 0) {
    console.error('[smoke] FAIL: "Security" nav item not found in Operate menu')
    process.exitCode = 1
  } else {
    console.log('[smoke] OK: "Security" nav item present in Operate menu')
  }
  // Close the menu
  await page.keyboard.press('Escape')

  // ── 2. Page header ──────────────────────────────────────────────────────────
  const header = await page.locator('h1', { hasText: 'Security' }).count()
  if (header === 0) {
    console.error('[smoke] FAIL: Security page header not found')
    process.exitCode = 1
  } else {
    console.log('[smoke] OK: Security page header visible')
  }

  // ── 3. Four tabs present ────────────────────────────────────────────────────
  const TABS = ['auth', 'secrets', 'nplus1', 'cycles']
  for (const tab of TABS) {
    const btn = page.getByTestId(`security-tab-${tab}`)
    const count = await btn.count()
    if (count === 0) {
      console.error(`[smoke] FAIL: tab "${tab}" not found`)
      process.exitCode = 1
    } else {
      console.log(`[smoke] OK: tab "${tab}" present`)
    }
  }

  // ── 4. Screenshot: Auth Coverage tab (default) ──────────────────────────────
  await page.screenshot({
    path: path.join(OUT, '1330-01-security-auth.png'),
    fullPage: false,
  })
  console.log('[smoke] Screenshot: 1330-01-security-auth.png')

  // ── 5. Click Secrets tab ────────────────────────────────────────────────────
  await page.getByTestId('security-tab-secrets').click()
  await page.waitForTimeout(500)
  await page.screenshot({
    path: path.join(OUT, '1330-02-security-secrets.png'),
    fullPage: false,
  })
  console.log('[smoke] Screenshot: 1330-02-security-secrets.png')

  // ── 6. Click N+1 Queries tab ────────────────────────────────────────────────
  await page.getByTestId('security-tab-nplus1').click()
  await page.waitForTimeout(500)
  await page.screenshot({
    path: path.join(OUT, '1330-03-security-nplus1.png'),
    fullPage: false,
  })
  console.log('[smoke] Screenshot: 1330-03-security-nplus1.png')

  // ── 7. Click Import Cycles tab ──────────────────────────────────────────────
  await page.getByTestId('security-tab-cycles').click()
  await page.waitForTimeout(500)
  await page.screenshot({
    path: path.join(OUT, '1330-04-security-cycles.png'),
    fullPage: false,
  })
  console.log('[smoke] Screenshot: 1330-04-security-cycles.png')

  // ── 8. Console error check ──────────────────────────────────────────────────
  const jsErrors = errors.filter(
    (e) =>
      !e.includes('favicon') &&
      !e.includes('No mock') &&
      !e.includes('404'),
  )
  if (jsErrors.length > 0) {
    console.error('[smoke] FAIL: console errors detected:')
    jsErrors.forEach((e) => console.error('  ', e))
    process.exitCode = 1
  } else {
    console.log('[smoke] OK: no console errors')
  }

  await browser.close()

  if (process.exitCode === 1) {
    console.error('[smoke] OVERALL: FAILED')
  } else {
    console.log('[smoke] OVERALL: PASSED')
  }
}

main().catch((err) => {
  console.error('[smoke] FATAL:', err)
  process.exit(1)
})
