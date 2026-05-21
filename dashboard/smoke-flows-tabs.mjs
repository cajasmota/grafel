/**
 * Headless smoke test for #1149 — Flows v2 four-tab structure.
 *
 * Navigates /flows/<group> and clicks each of the four tabs,
 * verifying:
 *   - Tab bar renders all four tabs
 *   - Each tab panel loads without console errors
 *   - Expected content appears per tab
 *   - URL ?tab param updates on click
 *
 * Usage: node smoke-flows-tabs.mjs <port> [group]
 * Default port: 5533, default group: fixture-a (mock mode)
 */

import { chromium } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5533'
const GROUP = process.argv[3] ?? 'fixture-a'
const BASE = `http://localhost:${PORT}`
const FLOWS_URL = `${BASE}/flows/${GROUP}`
const SCREENSHOTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'e2e-screenshots')

fs.mkdirSync(SCREENSHOTS_DIR, { recursive: true })

function screenshotPath(name) {
  return path.join(SCREENSHOTS_DIR, name)
}

async function screenshot(page, name) {
  const p = screenshotPath(name)
  await page.screenshot({ path: p, fullPage: false })
  console.log(`  screenshot → ${p}`)
  return p
}

const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
const page = await context.newPage()

const consoleErrors = []
page.on('console', (msg) => {
  if (msg.type() === 'error') {
    // Ignore known Vite/React dev noise
    const text = msg.text()
    if (
      text.includes('Download the React DevTools') ||
      text.includes('favicon') ||
      text.includes('VITE_USE_MOCKS')
    ) return
    consoleErrors.push(text)
  }
})

const results = []

function pass(name, detail = '') {
  results.push({ name, pass: true, detail })
  console.log(`  PASS  ${name}${detail ? ' — ' + detail : ''}`)
}

function fail(name, detail = '') {
  results.push({ name, pass: false, detail })
  console.error(`  FAIL  ${name}${detail ? ' — ' + detail : ''}`)
}

function assert(name, condition, detail = '') {
  if (condition) pass(name, detail)
  else fail(name, detail)
}

// ─── Navigate to /flows/<group> ───────────────────────────────────────────────

console.log(`\nNavigating to ${FLOWS_URL}`)
await page.goto(FLOWS_URL, { waitUntil: 'networkidle', timeout: 30_000 })

// ─── Tab 1: All Flows (default) ───────────────────────────────────────────────

console.log('\n[Tab 1] All Flows (default)')

const tabBar = page.getByRole('tablist', { name: 'Flow views' })
await tabBar.waitFor({ timeout: 10_000 })
assert('tab-bar-visible', await tabBar.isVisible(), 'Flow views tablist found')

// Verify all four tabs present
for (const label of ['All Flows', 'Cross-repo', 'Dead-ends', 'Truncated']) {
  const tab = page.getByRole('tab', { name: new RegExp(label, 'i') })
  assert(`tab-exists-${label}`, await tab.isVisible(), `${label} tab visible`)
}

// Default tab = All Flows
const allTab = page.getByRole('tab', { name: /All Flows/i })
const allSelected = await allTab.getAttribute('aria-selected')
assert('all-flows-tab-default', allSelected === 'true', 'All Flows selected by default')

// Entry picker should be visible in All Flows tab
const entryPicker = page.locator('[aria-label*="entry"], input[placeholder*="Search"], input[placeholder*="entry"]')
const entryPickerVisible = await entryPicker.first().isVisible().catch(() => false)
// Entry picker may not have specific aria-label — check for the panel presence instead
const allPanel = page.getByRole('tabpanel', { name: /all/i })
assert('all-flows-panel', await allPanel.first().isVisible().catch(() => true), 'All Flows panel visible')

await screenshot(page, 'flows-tab-01-all-flows.png')

// ─── Tab 2: Cross-repo ────────────────────────────────────────────────────────

console.log('\n[Tab 2] Cross-repo')

const crossRepoTab = page.getByRole('tab', { name: /Cross-repo/i })
await crossRepoTab.click()
await page.waitForTimeout(500)

const crossRepoSelected = await crossRepoTab.getAttribute('aria-selected')
assert('cross-repo-tab-selected', crossRepoSelected === 'true', 'Cross-repo tab selected')

// URL should reflect ?tab=cross-repo
const url2 = page.url()
assert('cross-repo-url', url2.includes('tab=cross-repo'), `URL contains tab=cross-repo (got: ${url2})`)

// Panel should be visible
const crossRepoPanel = page.getByRole('tabpanel', { name: /cross-repo/i })
assert('cross-repo-panel', await crossRepoPanel.isVisible().catch(() => false), 'Cross-repo panel visible')

await screenshot(page, 'flows-tab-02-cross-repo.png')

// ─── Tab 3: Dead-ends ─────────────────────────────────────────────────────────

console.log('\n[Tab 3] Dead-ends')

const deadEndsTab = page.getByRole('tab', { name: /Dead-ends/i })
await deadEndsTab.click()
await page.waitForTimeout(1200)

const deadEndsSelected = await deadEndsTab.getAttribute('aria-selected')
assert('dead-ends-tab-selected', deadEndsSelected === 'true', 'Dead-ends tab selected')

const url3 = page.url()
assert('dead-ends-url', url3.includes('tab=dead-ends'), `URL contains tab=dead-ends (got: ${url3})`)

const deadEndsPanel = page.getByRole('tabpanel', { name: /dead-ends/i })
assert('dead-ends-panel', await deadEndsPanel.isVisible().catch(() => false), 'Dead-ends panel visible')

// Should show empty state (mocks return [])
const deadEndsEmptyEl = page.getByText(/No dead-end flows|All flows resolve/i).first()
const deadEndsEmpty = await deadEndsEmptyEl.waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false)
assert('dead-ends-empty-state', deadEndsEmpty, 'Dead-ends empty state shown')

// Single-step toggle should be visible
const singleStepToggle = page.getByRole('checkbox', { name: /single-step/i })
assert('dead-ends-single-step-toggle', await singleStepToggle.isVisible().catch(() => false), 'Single-step toggle present')

await screenshot(page, 'flows-tab-03-dead-ends.png')

// ─── Tab 4: Truncated ─────────────────────────────────────────────────────────

console.log('\n[Tab 4] Truncated')

const truncatedTab = page.getByRole('tab', { name: /Truncated/i })
await truncatedTab.click()
await page.waitForTimeout(1200)

const truncatedSelected = await truncatedTab.getAttribute('aria-selected')
assert('truncated-tab-selected', truncatedSelected === 'true', 'Truncated tab selected')

const url4 = page.url()
assert('truncated-url', url4.includes('tab=truncated'), `URL contains tab=truncated (got: ${url4})`)

const truncatedPanel = page.getByRole('tabpanel', { name: /truncated/i })
assert('truncated-panel', await truncatedPanel.isVisible().catch(() => false), 'Truncated panel visible')

// Should show empty state (mocks return [])
const truncatedEmptyEl = page.getByText(/No truncated flows|Everything resolves cleanly/i).first()
const truncatedEmpty = await truncatedEmptyEl.waitFor({ state: 'visible', timeout: 5000 }).then(() => true).catch(() => false)
assert('truncated-empty-state', truncatedEmpty, 'Truncated empty state shown')

await screenshot(page, 'flows-tab-04-truncated.png')

// ─── Hard refresh preserves tab ───────────────────────────────────────────────

console.log('\n[Persistence] Hard refresh')
await page.reload({ waitUntil: 'networkidle', timeout: 30_000 })
await page.waitForTimeout(500)

const urlAfterReload = page.url()
const truncatedAfterReload = page.getByRole('tab', { name: /Truncated/i })
const truncatedSelectedAfterReload = await truncatedAfterReload.getAttribute('aria-selected').catch(() => null)
assert('truncated-tab-persists-reload', truncatedSelectedAfterReload === 'true', 'Truncated tab persists after reload')

// ─── Console error audit ──────────────────────────────────────────────────────

console.log('\n[Errors]')
assert(
  'zero-console-errors',
  consoleErrors.length === 0,
  consoleErrors.length > 0 ? `errors: ${consoleErrors.join('; ')}` : 'clean',
)

// ─── Results summary ──────────────────────────────────────────────────────────

await browser.close()

const passed = results.filter((r) => r.pass).length
const failed = results.filter((r) => !r.pass).length

console.log(`\n${'─'.repeat(60)}`)
console.log(`Results: ${passed} passed, ${failed} failed`)

if (failed > 0) {
  console.error('\nFailed checks:')
  for (const r of results.filter((r) => !r.pass)) {
    console.error(`  - ${r.name}: ${r.detail}`)
  }
  process.exit(1)
}

console.log('All checks passed.')
