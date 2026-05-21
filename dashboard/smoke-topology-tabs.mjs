/**
 * Headless smoke test for #1140 — Topology v2 four-tab structure.
 *   1. Navigate to /topology/upvate
 *   2. Verify "All" tab is active by default
 *   3. Click each tab, verify content loads without console errors
 *   4. Verify tab selection persists to URL (?tab=...)
 *   5. 3 screenshots: all, orphan-publishers, orphan-subscribers
 *
 * Usage: node smoke-topology-tabs.mjs <port>
 */

import { chromium } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5544'
const BASE = `http://localhost:${PORT}`
const TOPO_URL = `${BASE}/topology/upvate`
const SCREENSHOTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'e2e-screenshots')

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

// Track console errors
const consoleErrors = []
page.on('console', (msg) => {
  if (msg.type() === 'error') consoleErrors.push(msg.text())
})

const results = []

function assert(name, pass, detail = '') {
  const status = pass ? 'PASS' : 'FAIL'
  const line = `[${status}] ${name}${detail ? ' — ' + detail : ''}`
  console.log(line)
  results.push({ name, pass, detail })
}

// ── Navigate to topology route ────────────────────────────────────────────────
console.log('\n── Navigate to /topology/upvate ──')
await page.goto(TOPO_URL, { waitUntil: 'networkidle', timeout: 30_000 })
await page.waitForTimeout(1000)

// Screenshot 1: All tab (default)
await screenshot(page, '1140-01-all-tab.png')

// Verify the tab bar is present
const tabList = await page.locator('[role="tablist"]').first()
assert('Tab bar rendered', await tabList.isVisible())

// Verify All tab exists and is selected
const allTab = page.locator('[role="tab"][id="topology-tab-all"]')
assert('All tab exists', await allTab.isVisible())
const allTabSelected = await allTab.getAttribute('aria-selected')
assert('All tab is active by default', allTabSelected === 'true', `aria-selected="${allTabSelected}"`)

// Verify other tabs exist
const pubTab = page.locator('[role="tab"][id="topology-tab-orphan-publishers"]')
const subTab = page.locator('[role="tab"][id="topology-tab-orphan-subscribers"]')
const schedTab = page.locator('[role="tab"][id="topology-tab-scheduled"]')
assert('Orphan Publishers tab exists', await pubTab.isVisible())
assert('Orphan Subscribers tab exists', await subTab.isVisible())
assert('Scheduled Jobs tab exists', await schedTab.isVisible())

// Verify All tab panel is visible
const allPanel = page.locator('[role="tabpanel"][id="topology-panel-all"]')
assert('All tab panel rendered', await allPanel.isVisible())

// URL should not have ?tab= on default tab
const urlBefore = page.url()
assert('Default tab URL has no tab param', !urlBefore.includes('tab='), urlBefore)

// ── Click Orphan Publishers tab ───────────────────────────────────────────────
console.log('\n── Click Orphan Publishers tab ──')
await pubTab.click()
await page.waitForTimeout(600)

// Screenshot 2: Orphan Publishers tab
await screenshot(page, '1140-02-orphan-publishers.png')

const pubTabSelected = await pubTab.getAttribute('aria-selected')
assert('Orphan Publishers tab becomes active', pubTabSelected === 'true', `aria-selected="${pubTabSelected}"`)

const pubPanel = page.locator('[role="tabpanel"][id="topology-panel-orphan-publishers"]')
assert('Orphan Publishers panel rendered', await pubPanel.isVisible())

// URL should reflect tab selection
const urlPub = page.url()
assert('Orphan Publishers tab persisted to URL', urlPub.includes('tab=orphan-publishers'), urlPub)

// ── Click Orphan Subscribers tab ──────────────────────────────────────────────
console.log('\n── Click Orphan Subscribers tab ──')
await subTab.click()
await page.waitForTimeout(600)

// Screenshot 3: Orphan Subscribers tab
await screenshot(page, '1140-03-orphan-subscribers.png')

const subTabSelected = await subTab.getAttribute('aria-selected')
assert('Orphan Subscribers tab becomes active', subTabSelected === 'true', `aria-selected="${subTabSelected}"`)

const subPanel = page.locator('[role="tabpanel"][id="topology-panel-orphan-subscribers"]')
assert('Orphan Subscribers panel rendered', await subPanel.isVisible())

const urlSub = page.url()
assert('Orphan Subscribers tab persisted to URL', urlSub.includes('tab=orphan-subscribers'), urlSub)

// ── Click Scheduled Jobs tab ──────────────────────────────────────────────────
console.log('\n── Click Scheduled Jobs tab ──')
await schedTab.click()
await page.waitForTimeout(600)

const schedTabSelected = await schedTab.getAttribute('aria-selected')
assert('Scheduled Jobs tab becomes active', schedTabSelected === 'true', `aria-selected="${schedTabSelected}"`)

const schedPanel = page.locator('[role="tabpanel"][id="topology-panel-scheduled"]')
assert('Scheduled Jobs panel rendered', await schedPanel.isVisible())

const urlSched = page.url()
assert('Scheduled Jobs tab persisted to URL', urlSched.includes('tab=scheduled'), urlSched)

// ── Navigate back to All tab ──────────────────────────────────────────────────
console.log('\n── Navigate back to All tab ──')
await allTab.click()
await page.waitForTimeout(600)

const allTabSelected2 = await allTab.getAttribute('aria-selected')
assert('All tab re-selectable', allTabSelected2 === 'true')

// URL should remove tab param when returning to All
const urlAfter = page.url()
assert('All tab removes tab param from URL', !urlAfter.includes('tab='), urlAfter)

// ── Hard refresh to test tab persistence ─────────────────────────────────────
console.log('\n── Hard refresh on orphan-publishers URL ──')
await page.goto(`${TOPO_URL}?tab=orphan-publishers`, { waitUntil: 'networkidle', timeout: 20_000 })
await page.waitForTimeout(800)

const pubTab2 = page.locator('[role="tab"][id="topology-tab-orphan-publishers"]')
const pubTabSelectedAfterRefresh = await pubTab2.getAttribute('aria-selected')
assert('Tab selection survives hard refresh', pubTabSelectedAfterRefresh === 'true', `aria-selected="${pubTabSelectedAfterRefresh}"`)

// ── Console error check ───────────────────────────────────────────────────────
// Filter out known non-fatal Vite HMR messages and React strict mode noise
const realErrors = consoleErrors.filter(
  (e) => !e.includes('HMR') && !e.includes('Warning:') && !e.includes('DevTools')
)
assert('Zero console errors', realErrors.length === 0, realErrors.length > 0 ? realErrors.join(' | ') : '')

// ── Summary ───────────────────────────────────────────────────────────────────
await browser.close()

const passed = results.filter((r) => r.pass).length
const failed = results.filter((r) => !r.pass).length

console.log(`\n── Results: ${passed} passed, ${failed} failed ──`)

if (failed > 0) {
  process.exit(1)
}
