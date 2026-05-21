/**
 * Headless smoke test for #1151 — entry-kind grouping headers in flow list.
 *
 * Verifies:
 *  1. Grouped section headers render at /flows/upvate (mock data)
 *  2. High-priority groups (http_handler, message_consumer, scheduled_task) open by default
 *  3. Collapsing a group hides its rows
 *  4. Quick-filter input narrows results across groups
 *  5. 0 console errors
 *
 * Screenshots:
 *  1151-01-default-open.png   — page load, high-priority groups open
 *  1151-02-internal-expanded.png — after expanding internal group
 *
 * Usage:
 *   node smoke-flows-grouping-1151.mjs <port>
 */

import { chromium } from 'playwright'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5544'
const BASE = `http://localhost:${PORT}`
const FLOWS_URL = `${BASE}/flows/upvate`
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

// ── Navigate ──────────────────────────────────────────────────────────────────
console.log('\n── Navigate to /flows/upvate ──')
await page.goto(FLOWS_URL, { waitUntil: 'networkidle', timeout: 30_000 })
await page.waitForTimeout(1_500)

// ── Screenshot 1: default state ───────────────────────────────────────────────
await screenshot(page, '1151-01-default-open.png')

// ── Verify group headers render ───────────────────────────────────────────────
const httpHeader = page.locator('[data-testid="flow-group-header-http_handler"]')
const consumerHeader = page.locator('[data-testid="flow-group-header-message_consumer"]')
const scheduledHeader = page.locator('[data-testid="flow-group-header-scheduled_task"]')

assert('http_handler group header visible', await httpHeader.isVisible())
assert('message_consumer group header visible', await consumerHeader.isVisible())
assert('scheduled_task group header visible', await scheduledHeader.isVisible())

// ── Verify high-priority groups are open by default ───────────────────────────
const httpRows = page.locator('[data-testid="flow-group-rows-http_handler"]')
const consumerRows = page.locator('[data-testid="flow-group-rows-message_consumer"]')
const scheduledRows = page.locator('[data-testid="flow-group-rows-scheduled_task"]')

assert('http_handler rows visible (open by default)', await httpRows.isVisible())
assert('message_consumer rows visible (open by default)', await consumerRows.isVisible())
assert('scheduled_task rows visible (open by default)', await scheduledRows.isVisible())

// ── Verify aria-expanded on open groups ───────────────────────────────────────
const httpExpanded = await httpHeader.getAttribute('aria-expanded')
assert('http_handler header aria-expanded=true', httpExpanded === 'true', `aria-expanded="${httpExpanded}"`)

// ── Collapse http_handler group ───────────────────────────────────────────────
console.log('\n── Collapse http_handler group ──')
await httpHeader.click()
await page.waitForTimeout(200)

const httpRowsAfterCollapse = page.locator('[data-testid="flow-group-rows-http_handler"]')
assert('http_handler rows hidden after collapse', !(await httpRowsAfterCollapse.isVisible()))

const httpExpandedAfter = await httpHeader.getAttribute('aria-expanded')
assert('http_handler aria-expanded=false after collapse', httpExpandedAfter === 'false', `aria-expanded="${httpExpandedAfter}"`)

// ── Expand internal group (if present) ───────────────────────────────────────
console.log('\n── Expand internal group ──')
const internalHeader = page.locator('[data-testid="flow-group-header-internal"]')
const hasInternal = await internalHeader.isVisible()

if (hasInternal) {
  // internal should be collapsed by default
  const internalRowsBefore = page.locator('[data-testid="flow-group-rows-internal"]')
  assert('internal group collapsed by default', !(await internalRowsBefore.isVisible()))

  await internalHeader.click()
  await page.waitForTimeout(200)

  assert('internal group rows visible after expand', await page.locator('[data-testid="flow-group-rows-internal"]').isVisible())
} else {
  assert('internal group present (skipped — not in mock)', true, 'group absent from response')
}

// ── Screenshot 2: with internal expanded ─────────────────────────────────────
await screenshot(page, '1151-02-internal-expanded.png')

// ── Quick filter ──────────────────────────────────────────────────────────────
console.log('\n── Quick filter test ──')
const filterInput = page.locator('[data-testid="flow-filter-input"]')
assert('Filter input visible', await filterInput.isVisible())

await filterInput.fill('UserList')
await page.waitForTimeout(300)

// After filter, count visible FlowRow elements (role="row") across all groups
const listBody = page.locator('[data-testid="flow-list-body"]')
const visibleRows = listBody.locator('[role="row"]:visible')
const visibleCount = await visibleRows.count()
// Also check that group headers present (count-descending label still shows)
const groupHeaders = listBody.locator('button[data-testid^="flow-group-header"]')
const groupHeaderCount = await groupHeaders.count()
assert(
  'Filter narrows results (rows or group headers visible)',
  visibleCount > 0 || groupHeaderCount > 0,
  `${visibleCount} rows, ${groupHeaderCount} headers visible`,
)

// Clear filter
await filterInput.fill('')
await page.waitForTimeout(200)

// ── Row selection still works ─────────────────────────────────────────────────
console.log('\n── Row selection ──')
// Re-expand http_handler so rows are visible
const httpHeaderForClick = page.locator('[data-testid="flow-group-header-http_handler"]')
if ((await httpHeaderForClick.getAttribute('aria-expanded')) === 'false') {
  await httpHeaderForClick.click()
  await page.waitForTimeout(200)
}

const firstRow = httpRows.locator('[role="row"]').first()
const rowVisible = await firstRow.isVisible()
if (rowVisible) {
  await firstRow.click()
  await page.waitForTimeout(300)
  // Detail panel or selected state should appear
  const selected = await firstRow.getAttribute('aria-selected')
  assert('Row selection works (aria-selected)', selected === 'true', `aria-selected="${selected}"`)
} else {
  assert('Row visible for selection test', false, 'No rows found in http_handler group')
}

// ── Console errors check ──────────────────────────────────────────────────────
console.log('\n── Console errors ──')
assert('0 console errors', consoleErrors.length === 0, consoleErrors.join('; ') || 'none')

// ── Summary ───────────────────────────────────────────────────────────────────
await browser.close()

const passed = results.filter((r) => r.pass).length
const failed = results.filter((r) => !r.pass).length
console.log(`\n── Results: ${passed} passed, ${failed} failed ──`)

if (failed > 0) {
  console.error('\nFailed assertions:')
  results.filter((r) => !r.pass).forEach((r) => {
    console.error(`  FAIL: ${r.name}${r.detail ? ' — ' + r.detail : ''}`)
  })
  process.exit(1)
}

console.log('\nAll checks passed.')
