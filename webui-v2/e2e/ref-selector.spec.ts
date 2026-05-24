/**
 * Playwright E2E — ref selector (#2092 PH4)
 *
 * Tests that:
 *   1. The ref-selector trigger button is visible in the topbar.
 *   2. Clicking the trigger opens the popover dropdown.
 *   3. Selecting a ref updates the URL with ?ref=<name>.
 *   4. The trigger label reflects the selected ref name.
 *   5. Selecting the same ref again (or HEAD) clears the param.
 *
 * Resilient to daemon not running: the popover will show "No indexed refs"
 * or "Loading refs…" — both are valid states and the test only asserts UI
 * behavior, not data presence.
 *
 * NOTE: Tests that use client-fixture-a rely on a running archigraph daemon
 * with that group indexed. Tests that only need the chrome work without a daemon.
 */
import { test, expect } from "@playwright/test";

const GROUP = "client-fixture-a";

test.describe("ref selector — topbar (PH4 #2092)", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto(`/g/${GROUP}/graph`);
    // Wait for the topbar to hydrate
    await expect(
      page.getByRole("button", { name: /Switch project/i }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test("ref-selector trigger is visible in the topbar", async ({ page }) => {
    const trigger = page.getByTestId("ref-selector-trigger");
    await expect(trigger).toBeVisible();
  });

  test("clicking trigger opens the ref popover", async ({ page }) => {
    const trigger = page.getByTestId("ref-selector-trigger");
    await trigger.click();
    const popover = page.getByTestId("ref-selector-popover");
    await expect(popover).toBeVisible({ timeout: 3_000 });
  });

  test("popover closes on Escape key", async ({ page }) => {
    const trigger = page.getByTestId("ref-selector-trigger");
    await trigger.click();
    const popover = page.getByTestId("ref-selector-popover");
    await expect(popover).toBeVisible({ timeout: 3_000 });
    await page.keyboard.press("Escape");
    await expect(popover).not.toBeVisible({ timeout: 2_000 });
  });

  test("switching ref updates URL ?ref= param and trigger label", async ({ page }) => {
    // Intercept the refs endpoint to inject a deterministic fixture response.
    await page.route(`**/api/v2/groups/${GROUP}/refs`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ok: true,
          data: {
            refs: {
              "client-fixture-a-core": [
                {
                  name: "main",
                  sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                  shortSha: "aaaaaaa",
                  tier: "HOT",
                  indexedAt: Date.now() - 60_000,
                  indexerVersion: "v2.0.0",
                  source: "branch",
                },
                {
                  name: "feat/agent-X",
                  sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
                  shortSha: "bbbbbbb",
                  tier: "COLD",
                  indexedAt: Date.now() - 3_600_000,
                  indexerVersion: "v2.0.0",
                  source: "worktree",
                },
              ],
            },
          },
        }),
      }),
    );

    // Reload so the mock is used
    await page.reload();
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    const trigger = page.getByTestId("ref-selector-trigger");
    await trigger.click();
    const popover = page.getByTestId("ref-selector-popover");
    await expect(popover).toBeVisible({ timeout: 3_000 });

    // Click on "feat/agent-X" option
    const refOption = popover.getByRole("option", { name: /feat\/agent-X/i });
    await expect(refOption).toBeVisible({ timeout: 3_000 });
    await refOption.click();

    // URL should now contain ?ref=feat%2Fagent-X (slash encoded)
    await expect(page).toHaveURL(/[?&]ref=feat/);

    // Trigger label should show "feat/agent-X"
    await expect(trigger).toContainText("feat/agent-X");
  });

  test("selecting HEAD option clears ?ref= param", async ({ page }) => {
    // Start with ?ref=main
    await page.goto(`/g/${GROUP}/graph?ref=main`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // Inject mock
    await page.route(`**/api/v2/groups/${GROUP}/refs`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          ok: true,
          data: {
            refs: {
              "client-fixture-a-core": [
                {
                  name: "main",
                  sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                  shortSha: "aaaaaaa",
                  tier: "HOT",
                  indexedAt: Date.now() - 60_000,
                  indexerVersion: "v2.0.0",
                  source: "branch",
                },
              ],
            },
          },
        }),
      }),
    );

    await page.reload();
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    const trigger = page.getByTestId("ref-selector-trigger");
    await trigger.click();
    const popover = page.getByTestId("ref-selector-popover");
    await expect(popover).toBeVisible({ timeout: 3_000 });

    // Click HEAD default option
    const headOption = popover.getByRole("option", { name: /HEAD \(default\)/i });
    await expect(headOption).toBeVisible();
    await headOption.click();

    // URL should NOT contain ?ref=
    await expect(page).not.toHaveURL(/[?&]ref=/);
  });
});
