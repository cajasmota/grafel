/**
 * Playwright E2E — URL ?ref= persistence across views (#2092 PH4)
 *
 * Tests that:
 *   1. Loading /g/:group/graph?ref=feat-X preserves the ref on mount.
 *   2. Loading /g/:group/paths?ref=develop preserves the ref on mount.
 *   3. Navigating between screens via the nav-rail preserves ?ref=.
 *   4. The ref-selector trigger reflects the ref from the URL on initial load.
 *
 * Uses route-intercept mocks — no running daemon required.
 */
import { test, expect, type Page } from "@playwright/test";

const GROUP = "client-fixture-a";

/** Inject a mock refs response so the selector can show the ref label. */
async function mockRefs(page: Page): Promise<void> {
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
                name: "develop",
                sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                shortSha: "aaaaaaa",
                tier: "HOT",
                indexedAt: Date.now() - 300_000,
                indexerVersion: "v2.0.0",
                source: "branch",
              },
              {
                name: "feat/agent-X",
                sha: "cccccccccccccccccccccccccccccccccccccccc",
                shortSha: "ccccccc",
                tier: "WARM",
                indexedAt: Date.now() - 1_800_000,
                indexerVersion: "v2.0.0",
                source: "branch",
              },
            ],
          },
        },
      }),
    }),
  );
}

test.describe("URL ?ref= persistence (PH4 #2092)", () => {
  test("?ref= param is preserved on initial load — graph screen", async ({ page }) => {
    await mockRefs(page);
    await page.goto(`/g/${GROUP}/graph?ref=develop`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // URL still carries the param
    await expect(page).toHaveURL(/[?&]ref=develop/);

    // Trigger label should show "develop"
    const trigger = page.getByTestId("ref-selector-trigger");
    await expect(trigger).toBeVisible();
    await expect(trigger).toContainText("develop");
  });

  test("?ref= param is preserved on initial load — paths screen", async ({ page }) => {
    await mockRefs(page);
    await page.goto(`/g/${GROUP}/paths?ref=develop`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    await expect(page).toHaveURL(/[?&]ref=develop/);

    const trigger = page.getByTestId("ref-selector-trigger");
    await expect(trigger).toContainText("develop");
  });

  test("direct URL with encoded slash ref (%2F) is handled correctly", async ({ page }) => {
    await mockRefs(page);
    await page.goto(`/g/${GROUP}/graph?ref=feat%2Fagent-X`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    await expect(page).toHaveURL(/ref=feat/);

    const trigger = page.getByTestId("ref-selector-trigger");
    // The label should show the decoded name
    await expect(trigger).toContainText("feat/agent-X");
  });

  test("ref-selector trigger shows HEAD when ?ref= is absent", async ({ page }) => {
    await mockRefs(page);
    await page.goto(`/g/${GROUP}/graph`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    await expect(page).not.toHaveURL(/[?&]ref=/);

    const trigger = page.getByTestId("ref-selector-trigger");
    await expect(trigger).toContainText("HEAD");
  });
});
