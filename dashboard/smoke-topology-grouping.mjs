/**
 * Headless smoke test for #1142 — Topology v2 broker + service grouping headers.
 *   1. Navigate to /topology/upvate in list view
 *   2. Verify "By broker" mode is default and broker sections render
 *   3. Screenshot 1: default grouping (by broker, all sections expanded)
 *   4. Collapse the first broker section
 *   5. Screenshot 2: first broker section collapsed
 *   6. Verify collapse state persists after switching grouping and back
 *   7. Verify search still works across all groups
 *   8. Zero console errors
 *
 * Usage: node smoke-topology-grouping.mjs <port>
 */

import { chromium } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5173'
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

// ── Step 1: Navigate to topology route in list view ───────────────────────────
console.log('\n── Navigate to /topology/upvate ──')
// Force list view by setting localStorage before navigation
await page.addInitScript(() => {
  localStorage.setItem('archigraph:topology-view-mode', 'list')
  localStorage.setItem('archigraph:topology-list-grouping', 'broker')
})

await page.goto(TOPO_URL, { waitUntil: 'networkidle', timeout: 30_000 })
await page.waitForTimeout(1500)

// ── Step 2: Verify broker grouping view renders ───────────────────────────────
console.log('\n── Verify broker grouping ──')

const brokerGroupsContainer = page.locator('[data-testid="topology-broker-groups"]')
const brokerGroupsVisible = await brokerGroupsContainer.isVisible().catch(() => false)
assert('Broker groups container rendered', brokerGroupsVisible)

// Verify "By broker" button is active (aria-pressed)
const byBrokerBtn = page.locator('button[aria-pressed="true"]').filter({ hasText: 'By broker' })
const byBrokerActive = await byBrokerBtn.isVisible().catch(() => false)
assert('By broker grouping is default / active', byBrokerActive)

// Verify at least one broker section header exists
const brokerHeaders = page.locator('[data-broker]')
const brokerCount = await brokerHeaders.count()
assert('At least one broker section rendered', brokerCount > 0, `found ${brokerCount} broker sections`)

// Verify first broker header has aria-expanded
if (brokerCount > 0) {
  const firstHeader = brokerHeaders.first()
  const firstBrokerSlug = await firstHeader.getAttribute('data-broker')
  assert('First broker section has data-broker attribute', !!firstBrokerSlug, firstBrokerSlug ?? 'none')

  const firstExpandBtn = firstHeader.locator('button[aria-expanded]').first()
  const firstExpanded = await firstExpandBtn.getAttribute('aria-expanded').catch(() => null)
  assert('First broker section is expanded by default', firstExpanded === 'true', `aria-expanded="${firstExpanded}"`)
}

// Screenshot 1: Default grouping — all broker sections visible
await screenshot(page, '1142-01-broker-grouping-default.png')

// ── Step 3: Collapse first broker section ─────────────────────────────────────
console.log('\n── Collapse first broker section ──')
if (brokerCount > 0) {
  const firstBrokerBtn = brokerHeaders.first().locator('button[aria-expanded]').first()
  await firstBrokerBtn.click()
  await page.waitForTimeout(400)

  const collapsedAttr = await firstBrokerBtn.getAttribute('aria-expanded')
  assert('First broker section collapses on click', collapsedAttr === 'false', `aria-expanded="${collapsedAttr}"`)
}

// Screenshot 2: First broker section collapsed
await screenshot(page, '1142-02-first-broker-collapsed.png')

// ── Step 4: Collapse persists after switching grouping tabs ───────────────────
console.log('\n── Test collapse persistence across grouping switches ──')
if (brokerCount > 0) {
  // Switch to "By repo" grouping
  const byRepoBtn = page.locator('button').filter({ hasText: 'By repo' })
  await byRepoBtn.click()
  await page.waitForTimeout(400)

  // Switch back to "By broker"
  const byBrokerBtnSwitch = page.locator('button').filter({ hasText: 'By broker' })
  await byBrokerBtnSwitch.click()
  await page.waitForTimeout(400)

  // Re-query after switch
  const brokerHeadersAfter = page.locator('[data-broker]')
  const firstBrokerBtnAfter = brokerHeadersAfter.first().locator('button[aria-expanded]').first()
  const stillCollapsed = await firstBrokerBtnAfter.getAttribute('aria-expanded').catch(() => null)
  assert('Collapse state persists after grouping switch', stillCollapsed === 'false', `aria-expanded="${stillCollapsed}"`)
}

// ── Step 5: Search filters across all broker groups ───────────────────────────
console.log('\n── Test search across broker groups ──')

// Re-expand broker sections for search testing
const byBrokerBtnSearch = page.locator('button').filter({ hasText: 'By broker' })
await byBrokerBtnSearch.click()
await page.waitForTimeout(400)

const searchInput = page.locator('input[type="search"]')
const searchInputVisible = await searchInput.isVisible().catch(() => false)
assert('Search input is visible', searchInputVisible)

if (searchInputVisible) {
  // Type a query that matches real entities (celery tasks are present in upvate group)
  await searchInput.fill('async')
  await page.waitForTimeout(500)

  // Entity count should update to filtered subset
  const entityCountEl = page.locator('text=/\\d+ enti/').first()
  const entityCountVisible = await entityCountEl.isVisible().catch(() => false)
  assert('Entity count label visible during search', entityCountVisible)

  // When searching, broker groups container should still be visible (rows match filter)
  const brokerGroupsSearchContainer = page.locator('[data-testid="topology-broker-groups"]')
  const stillVisible = await brokerGroupsSearchContainer.isVisible().catch(() => false)
  assert('Broker groups container still visible during search', stillVisible)

  // Clear search
  await searchInput.fill('')
  await page.waitForTimeout(400)
}

// ── Step 6: Console error check ───────────────────────────────────────────────
const realErrors = consoleErrors.filter(
  (e) =>
    !e.includes('HMR') &&
    !e.includes('Warning:') &&
    !e.includes('DevTools') &&
    !e.includes('[vite]') &&
    !e.includes('favicon')
)
assert('Zero console errors', realErrors.length === 0, realErrors.length > 0 ? realErrors.join(' | ') : '')

// ── Summary ───────────────────────────────────────────────────────────────────
await browser.close()

const passed = results.filter((r) => r.pass).length
const failed = results.filter((r) => !r.pass).length

console.log(`\n── Results: ${passed} passed, ${failed} failed ──`)
if (failed > 0) {
  console.log('\nFailed assertions:')
  for (const r of results.filter((r) => !r.pass)) {
    console.log(`  ✗ ${r.name}${r.detail ? ' — ' + r.detail : ''}`)
  }
  process.exit(1)
}
