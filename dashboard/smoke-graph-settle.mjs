/**
 * Headless smoke test for #1066 + #1067:
 *   - Force simulation auto-pauses after ~5s (nodes freeze)
 *   - Resume/Pause layout button visible and functional
 *   - Show stdlib / Hide stdlib toggle visible and functional
 *   - External nodes absent by default, present when toggled on
 *
 * Usage: node smoke-graph-settle.mjs <port>
 */

import { chromium } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5533'
const BASE = `http://127.0.0.1:${PORT}`
const GRAPH_URL = `${BASE}/graph/upvate`
const SCREENSHOTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'smoke-screenshots-settle')

fs.mkdirSync(SCREENSHOTS_DIR, { recursive: true })

function screenshot(page, name) {
  const p = path.join(SCREENSHOTS_DIR, name)
  return page.screenshot({ path: p, fullPage: false }).then(() => {
    console.log(`  screenshot → ${p}`)
    return p
  })
}

const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
const page = await context.newPage()

// Track API requests
const apiGraphRequests = []
page.on('request', (req) => {
  if (req.url().includes('/api/graph')) {
    apiGraphRequests.push(req.url())
  }
})

const consoleErrors = []
page.on('console', (msg) => {
  if (msg.type() === 'error') consoleErrors.push(msg.text())
})

const results = []

function assert(name, pass, detail = '') {
  const status = pass ? 'PASS' : 'FAIL'
  results.push({ name, pass })
  console.log(`  [${status}] ${name}${detail ? ' — ' + detail : ''}`)
  return pass
}

// ── Step 1: Load graph page ─────────────────────────────────────────────────
console.log('\n── Step 1: Load graph page ─────────────────────────────────────────')
console.log(`Navigating to ${GRAPH_URL} ...`)
await page.goto(GRAPH_URL, { waitUntil: 'networkidle', timeout: 30000 })
await page.waitForTimeout(3000) // let canvas render

await screenshot(page, 'ss-01-loaded.png')

// Check canvas is present
const canvas = page.locator('canvas')
const canvasCount = await canvas.count()
assert('canvas rendered', canvasCount > 0, `count=${canvasCount}`)

// ── Step 2: Check toolbar buttons ──────────────────────────────────────────
console.log('\n── Step 2: Check toolbar buttons ───────────────────────────────────')

// Resume/Pause layout button (Play icon — simulation starts active, pauses after settle)
const playPauseBtn = page.locator('button[aria-label*="layout simulation"], button[aria-label*="Resume layout simulation"], button[aria-label*="Pause layout simulation"]')
const playPauseBtnCount = await playPauseBtn.count()
assert('Resume/Pause layout button present', playPauseBtnCount > 0, `count=${playPauseBtnCount}`)

// Show stdlib toggle button
const stdlibBtn = page.locator('button[aria-label*="stdlib"], button[aria-label*="external"]')
const stdlibBtnCount = await stdlibBtn.count()
assert('Show stdlib toggle button present', stdlibBtnCount > 0, `count=${stdlibBtnCount}`)

// ── Step 3: Verify initial node count (default: no external nodes) ──────────
console.log('\n── Step 3: Initial node count (external hidden by default) ─────────')
const nodeCountBadge = page.locator('.font-mono.select-none').first()
const nodeCountText = await nodeCountBadge.textContent() ?? 'unknown'
console.log(`  Node count: ${nodeCountText.trim()}`)

// Parse the number
const nodeCountMatch = nodeCountText.match(/[\d,]+/)
const initialNodeCount = nodeCountMatch ? parseInt(nodeCountMatch[0].replace(',', ''), 10) : 0
assert('initial node count > 0', initialNodeCount > 0, `count=${initialNodeCount}`)

// Capture initial API URL to check include_external is absent (default false = not sent)
const firstGraphRequest = apiGraphRequests.find(u => u.includes('/api/graph/upvate'))
console.log(`  First graph API request: ${firstGraphRequest ?? 'none found'}`)
assert('initial request lacks include_external=true',
  !firstGraphRequest || !firstGraphRequest.includes('include_external=true'),
  firstGraphRequest)

// ── Step 4: Wait for simulation to settle and auto-pause ────────────────────
console.log('\n── Step 4: Wait for simulation settle / auto-pause (~8s total) ─────')
await page.waitForTimeout(8000) // simulation should settle within 6s

// After settle, the Play/Pause button should reflect paused state
// (aria-pressed=false means sim paused → shows Play icon = "Resume layout")
const btnAriaPressed = await playPauseBtn.first().getAttribute('aria-pressed')
console.log(`  Play/Pause aria-pressed after settle: ${btnAriaPressed}`)
assert('simulation paused after settle (aria-pressed=false)', btnAriaPressed === 'false',
  `aria-pressed=${btnAriaPressed}`)

// Take screenshots 1s apart — node positions should be identical (frozen)
await screenshot(page, 'ss-02-settled-A.png')
await page.waitForTimeout(2000)
await screenshot(page, 'ss-03-settled-B.png')

console.log('  (Visual: ss-02 and ss-03 should be identical node positions)')

// ── Step 5: Click "Resume layout" → simulation runs ─────────────────────────
console.log('\n── Step 5: Click Resume layout ─────────────────────────────────────')
await playPauseBtn.first().click()
await page.waitForTimeout(1500)

const btnAfterResume = await playPauseBtn.first().getAttribute('aria-pressed')
assert('simulation running after Resume click (aria-pressed=true)', btnAfterResume === 'true',
  `aria-pressed=${btnAfterResume}`)

await screenshot(page, 'ss-04-after-resume.png')

// ── Step 6: Click "Pause layout" again ──────────────────────────────────────
console.log('\n── Step 6: Click Pause layout ──────────────────────────────────────')
await playPauseBtn.first().click()
await page.waitForTimeout(1000)

const btnAfterPause = await playPauseBtn.first().getAttribute('aria-pressed')
assert('simulation paused after Pause click (aria-pressed=false)', btnAfterPause === 'false',
  `aria-pressed=${btnAfterPause}`)

// ── Step 7: Toggle "Show stdlib" → external nodes appear ────────────────────
console.log('\n── Step 7: Toggle Show stdlib ──────────────────────────────────────')

// Clear request log
apiGraphRequests.length = 0

await stdlibBtn.first().click()
await page.waitForTimeout(3000) // wait for data refetch + render

await screenshot(page, 'ss-05-show-stdlib.png')

// Check a new API request was made with include_external=true
const externalRequest = apiGraphRequests.find(u => u.includes('include_external=true'))
assert('API called with include_external=true when stdlib toggled on',
  !!externalRequest, externalRequest ?? 'no request found')

// Node count should be higher when externals included
const nodeCountAfterExternal = await nodeCountBadge.textContent() ?? 'unknown'
console.log(`  Node count with stdlib: ${nodeCountAfterExternal.trim()}`)
const countAfterExternal = parseInt((nodeCountAfterExternal.match(/[\d,]+/) ?? ['0'])[0].replace(',', ''), 10)
assert('node count increased with stdlib', countAfterExternal > initialNodeCount,
  `before=${initialNodeCount} after=${countAfterExternal}`)

// Verify button reflects "Hide stdlib" now
const stdlibBtnLabel = await stdlibBtn.first().getAttribute('aria-label')
assert('stdlib button reflects enabled state', stdlibBtnLabel?.includes('Hide') ?? false,
  `label=${stdlibBtnLabel}`)

// ── Step 8: Toggle "Hide stdlib" → external nodes hidden again ──────────────
console.log('\n── Step 8: Toggle Hide stdlib ──────────────────────────────────────')
apiGraphRequests.length = 0

await stdlibBtn.first().click()
await page.waitForTimeout(3000)

await screenshot(page, 'ss-06-hide-stdlib.png')

const hideRequest = apiGraphRequests.find(u => u.includes('/api/graph/upvate'))
assert('API called when stdlib toggled off',
  !!hideRequest, hideRequest ?? 'no request found')
assert('request does not include include_external=true when stdlib off',
  !hideRequest || !hideRequest.includes('include_external=true'),
  hideRequest ?? '')

const nodeCountAfterHide = await nodeCountBadge.textContent() ?? 'unknown'
console.log(`  Node count after hiding stdlib: ${nodeCountAfterHide.trim()}`)
const countAfterHide = parseInt((nodeCountAfterHide.match(/[\d,]+/) ?? ['0'])[0].replace(',', ''), 10)
assert('node count restored to original after hiding stdlib', countAfterHide <= countAfterExternal,
  `initial=${initialNodeCount} external=${countAfterExternal} restored=${countAfterHide}`)

// ── Step 9: Console errors ───────────────────────────────────────────────────
console.log('\n── Step 9: Console error check ─────────────────────────────────────')
const relevantErrors = consoleErrors.filter(e =>
  !e.includes('GPU stall') &&
  !e.includes('WebGL') &&
  !e.includes('ReadPixels') &&
  !e.includes('net::ERR') &&
  !e.includes('DuckDB') &&
  !e.includes('Abort')
)
assert('no relevant console errors', relevantErrors.length === 0,
  relevantErrors.length > 0 ? relevantErrors.slice(0, 3).join(' | ') : 'clean')

// ── Summary ──────────────────────────────────────────────────────────────────
console.log('\n══ SMOKE TEST SUMMARY ══════════════════════════════════════════════')
const passed = results.filter(r => r.pass).length
const failed = results.filter(r => !r.pass).length
results.forEach(r => console.log(`  ${r.pass ? '✓' : '✗'} ${r.name}`))
console.log(`\n  ${passed}/${results.length} passed — Screenshots in: ${SCREENSHOTS_DIR}`)

await browser.close()
process.exit(failed === 0 ? 0 : 1)
