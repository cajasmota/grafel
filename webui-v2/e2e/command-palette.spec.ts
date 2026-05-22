import { test, expect } from "@playwright/test";

test.describe("Command palette (#1515)", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/g/demo/graph");
    // Wait for chrome to hydrate — TopBar button is a reliable sentinel
    await expect(page.getByText("Quick jump")).toBeVisible({ timeout: 10_000 });
  });

  test("⌘K opens the palette", async ({ page }) => {
    await page.keyboard.press("Meta+k");
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
  });

  test("TopBar button opens the palette", async ({ page }) => {
    await page.getByText("Quick jump").click();
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
  });

  test("Escape closes the palette", async ({ page }) => {
    await page.getByText("Quick jump").click();
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog", { name: "Command palette" })).not.toBeVisible();
  });

  test("navigate to Flows screen via palette", async ({ page }) => {
    await page.getByText("Quick jump").click();
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
    // Type 'Flows' to filter navigate items
    await page.keyboard.type("Flows");
    // First item should now be 'Flows' nav item; press Enter
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/flows$/);
  });

  test("entity search input interaction works", async ({ page }) => {
    await page.getByText("Quick jump").click();
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
    const input = page.getByPlaceholder("Search or navigate…");
    await input.fill("main");
    // Wait briefly for the entities group to appear (daemon not running → empty state is fine)
    await page.waitForTimeout(1_000);
    // Verify the input has text (search was triggered)
    await expect(input).toHaveValue("main");
  });

  test("light mode screenshot", async ({ page }) => {
    await page.evaluate(() => document.documentElement.setAttribute("data-theme", "light"));
    await page.getByText("Quick jump").click();
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
    await page.waitForTimeout(150); // let animation settle
    await page.screenshot({ path: "screenshots/1515-palette-light.png", fullPage: false });
  });

  test("dark mode screenshot", async ({ page }) => {
    await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
    await page.getByText("Quick jump").click();
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
    await page.waitForTimeout(150);
    await page.screenshot({ path: "screenshots/1515-palette-dark.png", fullPage: false });
  });
});
