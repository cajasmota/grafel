// webui-v2/e2e/error-boundary.spec.ts
import { test, expect } from "@playwright/test";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

test.describe("Route error boundary", () => {
  test("shows branded error page on render crash (light)", async ({ page }) => {
    await page.goto("/test/throw");

    // Must NOT show React Router's dev default
    await expect(page.getByText("Hey developer")).not.toBeVisible();

    // Must show our branded headline
    await expect(page.getByRole("heading", { name: "Something went wrong." })).toBeVisible();

    // Reload button
    await expect(page.getByRole("button", { name: "Reload page" })).toBeVisible();

    // Back to home link
    await expect(page.getByRole("link", { name: "Back to home" })).toBeVisible();

    // Technical details collapsible
    const details = page.locator("details");
    await expect(details).toBeVisible();
    await details.locator("summary").click();
    // Stack trace should mention the error message (in the stack <dd>)
    await expect(details.locator("dd").filter({ hasText: "Intentional test error" }).first()).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, "../../1539-errboundary-light.png"),
      fullPage: false,
    });
  });

  test("shows branded error page on render crash (dark)", async ({ page }) => {
    // Force dark mode via class
    await page.goto("/test/throw");
    await page.evaluate(() => document.documentElement.classList.add("dark"));
    await page.waitForTimeout(100); // allow paint

    await expect(page.getByRole("heading", { name: "Something went wrong." })).toBeVisible();

    await page.screenshot({
      path: path.join(__dirname, "../../1539-errboundary-dark.png"),
      fullPage: false,
    });
  });
});
