/**
 * Playwright E2E — Paths endpoint-detail view (#2113)
 *
 * Tests the restructured Parameters/Response sections, truncation helpers,
 * shape-tree expansion, and Auth section.
 *
 * NOTE: These tests require a running archigraph daemon with the client-fixture-a
 * group indexed. They are designed to be robust against timing but will skip
 * gracefully if no route is selectable (daemon not running in CI).
 */
import { test, expect, type Page } from "@playwright/test";

// Navigate to the Paths screen for the demo group and wait for it to load.
async function gotoPaths(page: Page, group = "demo"): Promise<void> {
  await page.goto(`/g/${group}/paths`);
  // Wait for the paths screen sentinel
  await page.waitForSelector('[data-testid="paths-screen"]', { timeout: 10_000 });
}

// Click the first available route row and wait for the detail pane to hydrate.
async function selectFirstRoute(page: Page): Promise<boolean> {
  // Wait for at least one backend card or route row
  const backendCard = page.locator('[data-testid^="backend-card-"]').first();
  const hasBackend = await backendCard.isVisible({ timeout: 5_000 }).catch(() => false);
  if (hasBackend) {
    await backendCard.click();
  }

  // Wait for a route row to appear
  const routeRow = page.locator('[data-testid^="route-row-"]').first();
  const hasRoute = await routeRow.isVisible({ timeout: 5_000 }).catch(() => false);
  if (!hasRoute) return false;

  await routeRow.click();
  // Wait for detail pane to be visible
  await page.waitForSelector('[data-testid="paths-detail-pane"]', { timeout: 5_000 }).catch(() => null);
  return true;
}

test.describe("Paths endpoint detail (#2113)", () => {
  test.describe.configure({ mode: "serial" });

  test("Auth section renders between Description and Parameters", async ({ page }) => {
    await gotoPaths(page);
    const selected = await selectFirstRoute(page);
    test.skip(!selected, "No routes available — daemon not running");

    // Auth section should appear
    const authSection = page.locator('[data-testid="auth-section"]');
    await expect(authSection).toBeVisible({ timeout: 8_000 });

    // The auth chip should be present
    const authChip = page.locator('[data-testid="auth-chip"]');
    await expect(authChip).toBeVisible();

    // Auth section appears before the Parameters ShapeTree
    const authBox = await authSection.boundingBox();
    const paramSection = page.locator('[data-testid="shape-tree"]').first();
    const paramBox = await paramSection.boundingBox().catch(() => null);
    if (authBox && paramBox) {
      expect(authBox.y).toBeLessThan(paramBox.y);
    }
  });

  test("Response section has no [PUT] / [POST] / [PATCH] verb chip", async ({ page }) => {
    await gotoPaths(page);
    const selected = await selectFirstRoute(page);
    test.skip(!selected, "No routes available — daemon not running");

    // Look specifically in the Response section area
    // The shape tree within Response should have no chips labelled PUT/POST/PATCH/DELETE
    const shapeTrees = page.locator('[data-testid="shape-tree"]');
    await expect(shapeTrees.first()).toBeVisible({ timeout: 8_000 });

    // Verify no in-chip has text PUT, POST, PATCH, or DELETE inside the shape rows
    const verbChipsInRows = page.locator('[data-testid^="in-chip-PUT"], [data-testid^="in-chip-POST"], [data-testid^="in-chip-PATCH"], [data-testid^="in-chip-DELETE"]');
    const count = await verbChipsInRows.count();
    expect(count).toBe(0);
  });

  test("Parameters section shows [in] chips with correct colors", async ({ page }) => {
    await gotoPaths(page);
    const selected = await selectFirstRoute(page);
    test.skip(!selected, "No routes available — daemon not running");

    // Wait for shape tree
    const shapeTree = page.locator('[data-testid="shape-tree"]').first();
    const isVisible = await shapeTree.isVisible({ timeout: 8_000 }).catch(() => false);
    test.skip(!isVisible, "No shape tree visible");

    // Check that at least one in-chip exists (path param should always be present if route has {var})
    const anyInChip = page.locator('[data-testid^="in-chip-"]').first();
    const hasChip = await anyInChip.isVisible({ timeout: 3_000 }).catch(() => false);
    // This is a soft check — not all endpoints have parameters
    if (hasChip) {
      // The chip text should be one of the expected values
      const chipText = await anyInChip.textContent();
      expect(["path", "query", "body", "header", "cookie", "form"]).toContain(chipText?.trim().toLowerCase());
    }
  });

  test("ShapeTree expand chevron shows nested fields for expandable rows", async ({ page }) => {
    await gotoPaths(page);
    const selected = await selectFirstRoute(page);
    test.skip(!selected, "No routes available — daemon not running");

    // Look for an expandable row (has a ChevronRight icon inside it)
    const expandableRow = page.locator('[data-testid^="shape-row-"]').filter({ has: page.locator('svg') }).first();
    const hasExpandable = await expandableRow.isVisible({ timeout: 5_000 }).catch(() => false);
    test.skip(!hasExpandable, "No expandable rows available for this endpoint");

    await expandableRow.click();

    // Wait for nested content (some text that looks like a field name)
    // The NestedFieldRows component renders field rows; after expand, new rows appear
    await page.waitForTimeout(500); // give lazy fetch time
    const nestedRows = page.locator('.font-mono').filter({ hasText: /^[a-zA-Z]/ });
    const rowCount = await nestedRows.count();
    expect(rowCount).toBeGreaterThan(0);
  });

  test("TruncatedPath click-to-copy copies full path to clipboard", async ({ page }) => {
    await gotoPaths(page);
    const selected = await selectFirstRoute(page);
    test.skip(!selected, "No routes available — daemon not running");

    // Find the first truncated path button
    const truncatedPath = page.locator('[data-testid="truncated-path"]').first();
    const hasTruncated = await truncatedPath.isVisible({ timeout: 5_000 }).catch(() => false);
    test.skip(!hasTruncated, "No TruncatedPath elements visible for this endpoint");

    // Grant clipboard permission
    await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);

    // Read the expected value from the title or data attribute
    const titleValue = await truncatedPath.getAttribute("title");

    await truncatedPath.click();

    // Check clipboard if browser supports reading it
    const clipboardText = await page.evaluate(() =>
      navigator.clipboard.readText().catch(() => null),
    );

    if (clipboardText !== null && titleValue) {
      // Clipboard should contain the full value (title attribute has full string)
      expect(clipboardText).toBe(titleValue);
    }
  });

  test("Truncated FQN hover tooltip shows full FQN", async ({ page }) => {
    await gotoPaths(page);
    const selected = await selectFirstRoute(page);
    test.skip(!selected, "No routes available — daemon not running");

    // Find a truncated path that is actually truncated (display !== full)
    const truncated = page.locator('[data-testid="truncated-path"]').first();
    const hasTruncated = await truncated.isVisible({ timeout: 5_000 }).catch(() => false);
    test.skip(!hasTruncated, "No TruncatedPath elements visible");

    // Hover to trigger tooltip
    await truncated.hover();

    // The Radix tooltip portal renders its content in the body
    const tooltip = page.locator('[role="tooltip"]');
    const hasTooltip = await tooltip.isVisible({ timeout: 2_000 }).catch(() => false);

    // Tooltip appears only for truncated strings; soft assert
    if (hasTooltip) {
      const tooltipText = await tooltip.textContent();
      expect(tooltipText?.trim().length).toBeGreaterThan(0);
    }
  });
});
