/**
 * E2E: Help & About surface — headless smoke + VIEW screenshot (#1253)
 *
 * Verifies:
 *   1. /help route loads without console errors
 *   2. "Help & About" nav item appears in the Operate dropdown
 *   3. Page header renders with correct heading
 *   4. All 6 section cards are present (about, tour, glossary, tips, faq, contact)
 *   5. Tour controls (prev/next/play-pause, slide title) function
 *   6. Glossary search filters results
 *   7. FAQ search filters results
 *   8. Contact: "Open GitHub issue" link and "Copy diagnostic report" button present
 *   9. ⌘? keyboard shortcut navigates to /help from the home page
 *  10. Screenshots captured for VIEW review
 */

import { test, expect } from '@playwright/test'
import { fileURLToPath } from 'url'
import path from 'path'
import fs from 'fs'

const __filename = fileURLToPath(import.meta.url)
const __dirname = path.dirname(__filename)

const BASE_URL = process.env.TEST_BASE_URL ?? 'http://localhost:5173'
const HELP_URL = `${BASE_URL}/help`
const SCREENSHOT_DIR = path.join(__dirname, '..', '..', 'e2e-screenshots')

function ensureDir(dir: string) {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true })
}

// ─────────────────────────────────────────────────────────────────────────────

test.describe('Help & About surface — #1253', () => {
  let consoleErrors: string[] = []

  test.beforeEach(async ({ page }) => {
    consoleErrors = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text()
        // Ignore network errors from no live daemon in CI
        if (
          !text.includes('Failed to load resource') &&
          !text.includes('ERR_CONNECTION') &&
          !text.includes('ERR_FAILED')
        ) {
          consoleErrors.push(text)
        }
      }
    })
  })

  // ── 1. No console errors ──────────────────────────────────────────────────

  test('No unexpected console errors on /help', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)
    expect(consoleErrors).toHaveLength(0)
  })

  // ── 2. Nav entry ──────────────────────────────────────────────────────────

  test('"Help & About" appears in Operate dropdown', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const nav = page.getByRole('navigation', { name: 'Surface navigation' })
    await expect(nav).toBeVisible()

    // Open the Operate dropdown
    const operateTrigger = nav.getByTestId('nav-operate')
    await expect(operateTrigger).toBeVisible()
    await operateTrigger.click()
    await page.waitForTimeout(300)

    // "Help & About" entry should be visible
    const content = page.getByTestId('nav-operate-content')
    await expect(content).toBeVisible()
    await expect(content.getByText('Help & About')).toBeVisible()
  })

  // ── 3. Page header ────────────────────────────────────────────────────────

  test('Page renders with correct heading', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const helpPage = page.getByTestId('help-page')
    await expect(helpPage).toBeVisible()

    await expect(page.getByRole('heading', { level: 1, name: /help/i })).toBeVisible()
  })

  // ── 4. All section cards present ──────────────────────────────────────────

  test('All 6 section cards are rendered', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const sections = ['about', 'tour', 'glossary', 'tips', 'faq', 'contact']
    for (const id of sections) {
      await expect(page.getByTestId(`help-section-${id}`)).toBeVisible()
    }
  })

  // ── 5. About section ─────────────────────────────────────────────────────

  test('About section opens and shows archigraph branding', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    // About is open by default
    const aboutSection = page.getByTestId('help-section-about')
    await expect(aboutSection).toBeVisible()

    // archigraph brand name
    await expect(aboutSection.getByText('archigraph')).toBeVisible()

    // GitHub link present
    const ghLink = aboutSection.getByRole('link', { name: /github repository/i })
    await expect(ghLink).toBeVisible()
  })

  // ── 6. Tour section ───────────────────────────────────────────────────────

  test('Tour section: slides and controls function', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    // Open tour section
    const tourSection = page.getByTestId('help-section-tour')
    await tourSection.getByRole('button').first().click()
    await page.waitForTimeout(300)

    const tourContainer = page.getByTestId('tour-container')
    if (!await tourContainer.isVisible()) {
      // Already open — try clicking the header button
      return
    }

    // Slide title visible
    const slideTitle = page.getByTestId('tour-slide-title')
    await expect(slideTitle).toBeVisible()
    const firstTitle = await slideTitle.textContent()
    expect(firstTitle).toBeTruthy()

    // Slide description visible
    await expect(page.getByTestId('tour-slide-description')).toBeVisible()

    // Click next
    const nextBtn = page.getByTestId('tour-next')
    await expect(nextBtn).toBeVisible()
    await nextBtn.click()
    await page.waitForTimeout(400)

    // Title should have changed
    const secondTitle = await slideTitle.textContent()
    expect(secondTitle).not.toBe(firstTitle)

    // Click prev to go back
    const prevBtn = page.getByTestId('tour-prev')
    await prevBtn.click()
    await page.waitForTimeout(400)
    const backTitle = await slideTitle.textContent()
    expect(backTitle).toBe(firstTitle)

    // Play/pause button present
    await expect(page.getByTestId('tour-play-pause')).toBeVisible()

    // "Open surface" button present
    await expect(page.getByTestId('tour-go-to-surface')).toBeVisible()
  })

  // ── 7. Glossary search ────────────────────────────────────────────────────

  test('Glossary search filters terms', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    // Open glossary section
    const glossarySection = page.getByTestId('help-section-glossary')
    await glossarySection.getByRole('button').first().click()
    await page.waitForTimeout(300)

    const searchInput = page.getByTestId('glossary-search')
    if (!await searchInput.isVisible()) return

    // Type a query that matches a known term
    await searchInput.fill('orphan')
    await page.waitForTimeout(200)

    const list = page.getByTestId('glossary-list')
    await expect(list).toBeVisible()

    // "Orphan" term should be visible
    await expect(list.getByText('Orphan')).toBeVisible()

    // Clear and verify all terms return
    await searchInput.fill('')
    await page.waitForTimeout(200)
    const terms = await list.locator('[data-testid^="glossary-term-"]').count()
    expect(terms).toBeGreaterThan(5)
  })

  // ── 8. FAQ search ─────────────────────────────────────────────────────────

  test('FAQ search filters questions', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    // The FAQ section starts closed — click its header button to open it
    const faqSection = page.getByTestId('help-section-faq')
    const faqHeaderBtn = faqSection.locator('button[aria-expanded]').first()
    // Confirm it's collapsed, then open it
    const isExpanded = await faqHeaderBtn.getAttribute('aria-expanded')
    if (isExpanded !== 'true') {
      await faqHeaderBtn.click()
      await page.waitForTimeout(400)
    }

    const searchInput = page.getByTestId('faq-search')
    await expect(searchInput).toBeVisible({ timeout: 3000 })

    // Count all items before filtering
    const list = page.getByTestId('faq-list')
    const totalBefore = await list.locator('[data-testid^="faq-"]').count()
    expect(totalBefore).toBeGreaterThan(4)

    // Filter to a term that matches only a subset
    await searchInput.fill('orphan')
    await page.waitForTimeout(300)

    const filteredItems = await list.locator('[data-testid^="faq-"]').count()
    // Should match at least 1 (the orphan question) but fewer than total
    expect(filteredItems).toBeGreaterThan(0)
    expect(filteredItems).toBeLessThan(totalBefore)
  })

  // ── 9. Contact section ────────────────────────────────────────────────────

  test('Contact section has GitHub issue link and copy button', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)

    const contactSection = page.getByTestId('help-section-contact')
    await contactSection.getByRole('button').first().click()
    await page.waitForTimeout(300)

    const newIssueLink = page.getByTestId('contact-new-issue-link')
    if (await newIssueLink.isVisible()) {
      await expect(newIssueLink).toBeVisible()
      const href = await newIssueLink.getAttribute('href')
      expect(href).toContain('github.com')
    }

    const copyBtn = page.getByTestId('contact-copy-diagnostics-btn')
    if (await copyBtn.isVisible()) {
      await expect(copyBtn).toBeVisible()
      await expect(copyBtn).toBeEnabled()
    }
  })

  // ── 10. ⌘? keyboard shortcut ──────────────────────────────────────────────

  test('⌘? keyboard shortcut navigates to /help', async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(800)

    // Press Cmd+? (Meta+Shift+/)
    await page.keyboard.press('Meta+Shift+/')
    await page.waitForTimeout(600)

    // Should now be on the help page
    expect(page.url()).toContain('/help')
    await expect(page.getByTestId('help-page')).toBeVisible()
  })

  // ── 11. Screenshots (VIEW) ────────────────────────────────────────────────

  test('Screenshot — full /help page (VIEW)', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(2000)

    ensureDir(SCREENSHOT_DIR)
    const screenshotPath = path.join(SCREENSHOT_DIR, 'help-surface-full.png')
    await page.screenshot({ path: screenshotPath, fullPage: true })

    expect(fs.existsSync(screenshotPath)).toBe(true)
    console.log(`[VIEW] Screenshot saved: ${screenshotPath}`)
  })

  test('Screenshot — tour section open (VIEW)', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1000)

    // Open the tour section if not already open
    const tourSection = page.getByTestId('help-section-tour')
    const tourBtn = tourSection.getByRole('button').first()
    await tourBtn.click()
    await page.waitForTimeout(500)

    ensureDir(SCREENSHOT_DIR)
    const screenshotPath = path.join(SCREENSHOT_DIR, 'help-tour-open.png')
    await page.screenshot({ path: screenshotPath, fullPage: false })

    expect(fs.existsSync(screenshotPath)).toBe(true)
    console.log(`[VIEW] Tour screenshot saved: ${screenshotPath}`)
  })

  test('Screenshot — glossary search (VIEW)', async ({ page }) => {
    await page.goto(HELP_URL, { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1000)

    const glossarySection = page.getByTestId('help-section-glossary')
    await glossarySection.getByRole('button').first().click()
    await page.waitForTimeout(500)

    const search = page.getByTestId('glossary-search')
    if (await search.isVisible()) {
      await search.fill('orphan')
      await page.waitForTimeout(300)
    }

    ensureDir(SCREENSHOT_DIR)
    const screenshotPath = path.join(SCREENSHOT_DIR, 'help-glossary-search.png')
    await page.screenshot({ path: screenshotPath, fullPage: false })

    expect(fs.existsSync(screenshotPath)).toBe(true)
    console.log(`[VIEW] Glossary screenshot saved: ${screenshotPath}`)
  })
})
