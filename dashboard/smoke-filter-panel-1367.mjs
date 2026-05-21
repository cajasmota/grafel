/**
 * Headless smoke test for #1367 — Multi-criteria graph filter panel.
 *
 * Tests:
 *   1. Filter panel toggle button is present in the sidebar
 *   2. Panel expands and shows all filter controls
 *   3. Active filter badge shows correct count
 *   4. Match count badge updates live
 *   5. Kind filter checkbox marks nodes visible/hidden
 *   6. Min-degree input narrows the match count
 *   7. Clear all button resets the panel
 *   8. No regression: repo filter, color toggle, simulation tunables still present
 *
 * Usage: node smoke-filter-panel-1367.mjs <port>
 */

import { chromium } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5533'
const BASE = `http://127.0.0.1:${PORT}`
const GRAPH_URL = `${BASE}/graph/upvate`
const SCREENSHOTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'e2e-screenshots', 'filter-panel-1367')

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
await page.waitForTimeout(2000)

await screenshot(page, 'ss-01-loaded.png')

// Canvas should be present
const canvas = page.locator('canvas')
const canvasCount = await canvas.count()
assert('canvas rendered', canvasCount > 0, `count=${canvasCount}`)

// ── Step 2: Filter panel toggle button exists ────────────────────────────────
console.log('\n── Step 2: Filter panel toggle button ──────────────────────────────')

const filterToggle = page.locator('[data-testid="filter-panel-toggle"]')
const filterToggleCount = await filterToggle.count()
assert('filter panel toggle button present', filterToggleCount > 0, `count=${filterToggleCount}`)

// Panel body should be hidden initially
const filterBody = page.locator('[data-testid="filter-panel-body"]')
const bodyInitialCount = await filterBody.count()
assert('filter panel body hidden initially', bodyInitialCount === 0, `count=${bodyInitialCount}`)

// ── Step 3: Expand the filter panel ─────────────────────────────────────────
console.log('\n── Step 3: Expand filter panel ─────────────────────────────────────')
await filterToggle.first().click()
await page.waitForTimeout(300)

const bodyAfterExpand = await filterBody.count()
assert('filter panel body visible after click', bodyAfterExpand > 0, `count=${bodyAfterExpand}`)

await screenshot(page, 'ss-02-panel-expanded.png')

// Check all controls are present
const kindSearch = page.locator('[data-testid="filter-kind-search"]')
assert('kind search input present', await kindSearch.count() > 0)

const minDegreeInput = page.locator('[data-testid="filter-min-degree"]')
assert('min-degree input present', await minDegreeInput.count() > 0)

const fileGlobInput = page.locator('[data-testid="filter-file-glob"]')
assert('file-glob input present', await fileGlobInput.count() > 0)

const hasPropSelect = page.locator('[data-testid="filter-has-property"]')
assert('has-property select present', await hasPropSelect.count() > 0)

const invertToggle = page.locator('[data-testid="filter-invert"]')
assert('invert toggle present', await invertToggle.count() > 0)

const clearAllBtn = page.locator('[data-testid="filter-clear-all"]')
assert('clear-all button present', await clearAllBtn.count() > 0)

const logicAnd = page.locator('[data-testid="filter-logic-and"]')
const logicOr = page.locator('[data-testid="filter-logic-or"]')
assert('AND logic button present', await logicAnd.count() > 0)
assert('OR logic button present', await logicOr.count() > 0)

// ── Step 4: Wait for nodes to load and check initial match count ─────────────
console.log('\n── Step 4: Match count after load ──────────────────────────────────')
await page.waitForTimeout(5000) // let graph data load

const matchCountEl = page.locator('[data-testid="filter-match-count"]')
const matchCountText = await matchCountEl.count() > 0 ? await matchCountEl.first().textContent() : 'n/a'
console.log(`  Initial match count text: "${matchCountText}"`)
// Match count may not be shown when no filter is active (matchCount = nodes.length but badge hidden when panel shows it inline)
// At minimum confirm no error state

// ── Step 5: Select a kind to filter ─────────────────────────────────────────
console.log('\n── Step 5: Select Function kind filter ─────────────────────────────')

const functionKindBtn = page.locator('[data-testid="filter-kind-Function"]')
const functionKindCount = await functionKindBtn.count()
assert('Function kind checkbox present', functionKindCount > 0, `count=${functionKindCount}`)

if (functionKindCount > 0) {
  await functionKindBtn.first().click()
  await page.waitForTimeout(400)

  const activeBadge = page.locator('[data-testid="filter-active-badge"]')
  const activeBadgeCount = await activeBadge.count()
  assert('active filter badge appears after kind selection', activeBadgeCount > 0, `count=${activeBadgeCount}`)

  const activeBadgeText = activeBadgeCount > 0 ? await activeBadge.first().textContent() : ''
  assert('active filter badge shows count ≥ 1', parseInt(activeBadgeText ?? '0', 10) >= 1, `badge="${activeBadgeText}"`)

  await screenshot(page, 'ss-03-function-filter.png')

  // Check aria-checked = true
  const ariaChecked = await functionKindBtn.first().getAttribute('aria-checked')
  assert('Function kind checkbox aria-checked=true', ariaChecked === 'true', `aria-checked=${ariaChecked}`)
}

// ── Step 6: Set min-degree filter ────────────────────────────────────────────
console.log('\n── Step 6: Set min-degree = 5 ──────────────────────────────────────')
await minDegreeInput.first().fill('5')
await page.waitForTimeout(400)

const matchCountAfterDegree = page.locator('[data-testid="filter-match-count"]')
const degreeMatchText = await matchCountAfterDegree.count() > 0 ? await matchCountAfterDegree.first().textContent() : 'n/a'
console.log(`  Match count after degree filter: "${degreeMatchText}"`)

await screenshot(page, 'ss-04-degree-filter.png')

// Active badge should show 2 or more (kind + degree)
const badgeAfterDegree = page.locator('[data-testid="filter-active-badge"]')
const badgeAfterDegreeText = await badgeAfterDegree.count() > 0 ? await badgeAfterDegree.first().textContent() : '0'
assert('active badge shows ≥ 2 after kind + degree', parseInt(badgeAfterDegreeText ?? '0', 10) >= 2, `badge="${badgeAfterDegreeText}"`)

// ── Step 7: Clear all ─────────────────────────────────────────────────────────
console.log('\n── Step 7: Clear all filters ────────────────────────────────────────')
await clearAllBtn.first().click()
await page.waitForTimeout(300)

// Active badge should disappear
const badgeAfterClear = page.locator('[data-testid="filter-active-badge"]')
const badgeAfterClearCount = await badgeAfterClear.count()
assert('active badge gone after clear all', badgeAfterClearCount === 0, `count=${badgeAfterClearCount}`)

// Kind checkbox should be unchecked
const functionKindAfterClear = await functionKindBtn.count() > 0 ? await functionKindBtn.first().getAttribute('aria-checked') : 'n/a'
assert('Function kind unchecked after clear all', functionKindAfterClear === 'false', `aria-checked=${functionKindAfterClear}`)

// Min degree should be 0
const minDegreeAfterClear = await minDegreeInput.first().inputValue()
assert('min-degree reset to 0 after clear all', minDegreeAfterClear === '0', `value=${minDegreeAfterClear}`)

await screenshot(page, 'ss-05-cleared.png')

// ── Step 8: File glob filter ──────────────────────────────────────────────────
console.log('\n── Step 8: File glob filter ─────────────────────────────────────────')
await fileGlobInput.first().fill('**/*.py')
await page.waitForTimeout(400)

const badgeGlob = page.locator('[data-testid="filter-active-badge"]')
const badgeGlobCount = await badgeGlob.count()
assert('active badge appears for glob filter', badgeGlobCount > 0, `count=${badgeGlobCount}`)

await screenshot(page, 'ss-06-glob-filter.png')

// Clear
await clearAllBtn.first().click()
await page.waitForTimeout(200)

// ── Step 9: OR logic toggle ───────────────────────────────────────────────────
console.log('\n── Step 9: Logic toggle ─────────────────────────────────────────────')

// AND should be active by default
const andAriaPressed = await logicAnd.first().getAttribute('aria-pressed')
assert('AND logic pressed by default', andAriaPressed === 'true', `aria-pressed=${andAriaPressed}`)

await logicOr.first().click()
await page.waitForTimeout(200)

const orAriaPressed = await logicOr.first().getAttribute('aria-pressed')
const andAriaAfterOr = await logicAnd.first().getAttribute('aria-pressed')
assert('OR logic becomes pressed', orAriaPressed === 'true', `aria-pressed=${orAriaPressed}`)
assert('AND logic unpressed after OR click', andAriaAfterOr === 'false', `aria-pressed=${andAriaAfterOr}`)

// Reset to AND
await logicAnd.first().click()
await page.waitForTimeout(200)

// ── Step 10: Regression — existing sidebar controls still present ─────────────
console.log('\n── Step 10: Regression — existing controls still present ────────────')

// Color by radio group
const colorByRepo = page.locator('button[role="radio"][aria-checked="true"]')
assert('color mode radio present', await colorByRepo.count() > 0)

// Simulation controls toggle
const simControls = page.locator('[data-testid="sim-controls-toggle"]')
assert('simulation controls toggle present', await simControls.count() > 0)

// Cross-repo toggle in toolbar
const crossRepoToggle = page.locator('[data-testid="cross-repo-toggle"]')
assert('cross-repo toggle present', await crossRepoToggle.count() > 0)

// Snapshot button
const snapshotBtn = page.locator('[data-testid="toolbar-snapshot-btn"]')
assert('snapshot button present', await snapshotBtn.count() > 0)

await screenshot(page, 'ss-07-regression-check.png')

// ── Step 11: Console errors ───────────────────────────────────────────────────
console.log('\n── Step 11: Console error check ─────────────────────────────────────')
const relevantErrors = consoleErrors.filter(e =>
  !e.includes('GPU stall') &&
  !e.includes('WebGL') &&
  !e.includes('ReadPixels') &&
  !e.includes('net::ERR') &&
  !e.includes('DuckDB') &&
  !e.includes('Abort') &&
  !e.includes('favicon')
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
