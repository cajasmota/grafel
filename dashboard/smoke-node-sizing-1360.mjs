/**
 * Headless smoke test for #1360 — Tunable node sizing.
 *
 *   - Sidebar shows "Node sizing" collapsible section
 *   - Expanding it shows base size input + 6 tier multiplier rows
 *   - Editing base size persists to localStorage
 *   - Editing tier multiplier persists to localStorage
 *   - Reset to defaults button appears when values are custom
 *   - Reset restores original values
 *   - 0 relevant console errors
 *
 * Usage: node smoke-node-sizing-1360.mjs <port>
 */

import { chromium } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'
import { fileURLToPath } from 'url'

const PORT = process.argv[2] ?? '5533'
const BASE = `http://127.0.0.1:${PORT}`
const GRAPH_URL = `${BASE}/graph/upvate`
const SCREENSHOTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), 'smoke-screenshots-node-sizing-1360')

fs.mkdirSync(SCREENSHOTS_DIR, { recursive: true })

function screenshot(page, name) {
  const p = path.join(SCREENSHOTS_DIR, name)
  return page.screenshot({ path: p, fullPage: false }).then(() => {
    console.log(`  screenshot → ${p}`)
    return p
  })
}

const results = []
function assert(name, condition, detail = '') {
  results.push({ name, pass: !!condition })
  if (condition) {
    console.log(`  ✓ ${name}`)
  } else {
    console.log(`  ✗ ${name}${detail ? ` — ${detail}` : ''}`)
  }
}

const browser = await chromium.launch({ headless: true })
const page = await browser.newPage()

const consoleErrors = []
page.on('console', (msg) => {
  if (msg.type() === 'error') consoleErrors.push(msg.text())
})

// ── Step 1: Navigate to /graph/upvate ────────────────────────────────────────
console.log('\n── Step 1: Navigate ─────────────────────────────────────────────────')
await page.goto(GRAPH_URL, { waitUntil: 'domcontentloaded', timeout: 30000 })
await page.waitForTimeout(5000)
await screenshot(page, 'ss-01-initial.png')

// ── Step 2: Sidebar visible and Node sizing section present ───────────────────
console.log('\n── Step 2: Node sizing section in sidebar ───────────────────────────')

const sidebar = page.locator('[aria-label="Graph filters sidebar"]')
const sidebarVisible = await sidebar.isVisible()
assert('sidebar is visible', sidebarVisible)

const nodeSizingToggle = page.locator('[aria-controls="node-sizing-body"]')
const sizingToggleVisible = await nodeSizingToggle.isVisible()
assert('Node sizing collapsible button is visible', sizingToggleVisible)

await screenshot(page, 'ss-02-sidebar-collapsed.png')

// ── Step 3: Expand the Node sizing section ────────────────────────────────────
console.log('\n── Step 3: Expand Node sizing ────────────────────────────────────────')
await nodeSizingToggle.click()
await page.waitForTimeout(300)

const sizingBody = page.locator('[data-testid="node-sizing-body"]')
const bodyVisible = await sizingBody.isVisible()
assert('Node sizing body expands on click', bodyVisible)

await screenshot(page, 'ss-03-expanded.png')

// ── Step 4: Verify base size input ────────────────────────────────────────────
console.log('\n── Step 4: Base size input ───────────────────────────────────────────')
const baseSizeInput = page.locator('[aria-label="Base node size in pixels"]')
const baseSizeVisible = await baseSizeInput.isVisible()
assert('Base size input is visible', baseSizeVisible)

const baseSizeValue = await baseSizeInput.inputValue()
assert(`Base size default is 10 (got ${baseSizeValue})`, baseSizeValue === '10')

// ── Step 5: Verify 6 tier multiplier inputs ───────────────────────────────────
console.log('\n── Step 5: Tier multiplier inputs ────────────────────────────────────')
const tierInputs = page.locator('[aria-label^="Tier "][aria-label$="size multiplier"]')
const tierCount = await tierInputs.count()
assert(`6 tier multiplier inputs visible (found ${tierCount})`, tierCount === 6)

// Check defaults
const expectedDefaults = ['1', '1.5', '2', '3', '5', '10']
for (let i = 0; i < 6; i++) {
  const val = await tierInputs.nth(i).inputValue()
  assert(`Tier ${i + 1} default multiplier = ${expectedDefaults[i]} (got ${val})`, val === expectedDefaults[i])
}

// ── Step 6: Edit base size (10 → 5) ──────────────────────────────────────────
console.log('\n── Step 6: Edit base size 10 → 5 ────────────────────────────────────')
await baseSizeInput.click({ clickCount: 3 })
await baseSizeInput.fill('5')
await baseSizeInput.press('Enter')
await page.waitForTimeout(500)

// Verify localStorage persisted
const storedAfterBaseEdit = await page.evaluate(() => {
  try { return localStorage.getItem('archigraph:graph:sizing') } catch { return null }
})
let parsed = null
try { parsed = JSON.parse(storedAfterBaseEdit ?? 'null') } catch { /* empty */ }
assert('localStorage updated after base size edit', !!parsed)
assert(`localStorage.baseSize = 5 (got ${parsed?.baseSize})`, parsed?.baseSize === 5)

await screenshot(page, 'ss-04-base-edited.png')

// Reset-to-defaults button should now be active
const resetBtn = page.locator('[aria-label="Reset node sizing to defaults"]')
const resetBtnEnabled = !(await resetBtn.isDisabled())
assert('Reset button enabled when values differ from defaults', resetBtnEnabled)

// ── Step 7: Edit tier 6 multiplier (10 → 20) ─────────────────────────────────
console.log('\n── Step 7: Edit tier 6 multiplier 10 → 20 ───────────────────────────')
const tier6Input = tierInputs.nth(5)
await tier6Input.click({ clickCount: 3 })
await tier6Input.fill('20')
await tier6Input.press('Enter')
await page.waitForTimeout(500)

const storedAfterTierEdit = await page.evaluate(() => {
  try { return localStorage.getItem('archigraph:graph:sizing') } catch { return null }
})
let parsed2 = null
try { parsed2 = JSON.parse(storedAfterTierEdit ?? 'null') } catch { /* empty */ }
assert('localStorage updated after tier 6 edit', !!parsed2)
assert(`localStorage.multipliers[5] = 20 (got ${parsed2?.multipliers?.[5]})`, parsed2?.multipliers?.[5] === 20)

await screenshot(page, 'ss-05-tier-edited.png')

// ── Step 8: Reset to defaults ─────────────────────────────────────────────────
console.log('\n── Step 8: Reset to defaults ────────────────────────────────────────')
await resetBtn.click()
await page.waitForTimeout(500)

const baseSizeAfterReset = await baseSizeInput.inputValue()
assert(`Base size restored to 10 after reset (got ${baseSizeAfterReset})`, baseSizeAfterReset === '10')

const tier6AfterReset = await tier6Input.inputValue()
assert(`Tier 6 multiplier restored to 10 after reset (got ${tier6AfterReset})`, tier6AfterReset === '10')

const storedAfterReset = await page.evaluate(() => {
  try { return localStorage.getItem('archigraph:graph:sizing') } catch { return null }
})
let parsed3 = null
try { parsed3 = JSON.parse(storedAfterReset ?? 'null') } catch { /* empty */ }
assert('localStorage updated to defaults after reset', parsed3?.baseSize === 10 && parsed3?.multipliers?.[5] === 10)

await screenshot(page, 'ss-06-after-reset.png')

// ── Step 9: Reload — values persist ───────────────────────────────────────────
console.log('\n── Step 9: Persistence across reload ────────────────────────────────')
// Set a custom value, reload, verify it survives
await baseSizeInput.click({ clickCount: 3 })
await baseSizeInput.fill('15')
await baseSizeInput.press('Enter')
await page.waitForTimeout(400)

await page.reload({ waitUntil: 'domcontentloaded', timeout: 30000 })
await page.waitForTimeout(2000)

// Re-expand sidebar section after reload
const nodeSizingToggleAfterReload = page.locator('[aria-controls="node-sizing-body"]')
await nodeSizingToggleAfterReload.click()
await page.waitForTimeout(300)

const baseSizeAfterReload = await page.locator('[aria-label="Base node size in pixels"]').inputValue()
assert(`Base size survives reload (got ${baseSizeAfterReload})`, baseSizeAfterReload === '15')

await screenshot(page, 'ss-07-after-reload.png')

// ── Step 10: Console errors ───────────────────────────────────────────────────
console.log('\n── Step 10: Console error check ─────────────────────────────────────')
const relevantErrors = consoleErrors.filter(e =>
  !e.includes('GPU stall') &&
  !e.includes('WebGL') &&
  !e.includes('ReadPixels') &&
  !e.includes('net::ERR') &&
  !e.includes('DuckDB') &&
  !e.includes('Abort') &&
  !e.includes('503') &&
  !e.includes('Failed to load resource') &&
  !e.includes('Service Unavailable')
)
assert('no relevant console errors', relevantErrors.length === 0,
  relevantErrors.length > 0 ? relevantErrors.slice(0, 3).join(' | ') : 'clean')

// ── Summary ───────────────────────────────────────────────────────────────────
console.log('\n══ SMOKE TEST SUMMARY ══════════════════════════════════════════════')
const passed = results.filter(r => r.pass).length
const failed = results.filter(r => !r.pass).length
results.forEach(r => console.log(`  ${r.pass ? '✓' : '✗'} ${r.name}`))
console.log(`\n  ${passed}/${results.length} passed — Screenshots in: ${SCREENSHOTS_DIR}`)

await browser.close()
process.exit(failed === 0 ? 0 : 1)
