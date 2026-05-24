/**
 * Playwright E2E — compare URL persistence (PH5 #2093)
 *
 * Tests that ?refA, ?refB, ?repo, ?filter, and ?kind params:
 *   1. Are preserved on reload.
 *   2. Update in the URL when the user changes filters.
 *   3. Deep-link to a specific filter view on initial load.
 *
 * Uses route-intercept mocks — no running daemon required.
 */
import { test, expect, type Page } from "@playwright/test";

const GROUP = "client-fixture-b";
const REPO = "client-fixture-b-svc";
const REF_A = "main";
const REF_B = "pr-99";

const MOCK_REFS = {
  ok: true,
  data: {
    refs: {
      [REPO]: [
        {
          name: REF_A,
          sha: "aaaa",
          shortSha: "aaaaaaa",
          tier: "HOT",
          indexedAt: Date.now(),
          indexerVersion: "v2.0.0",
          source: "branch",
        },
        {
          name: REF_B,
          sha: "bbbb",
          shortSha: "bbbbbbb",
          tier: "WARM",
          indexedAt: Date.now(),
          indexerVersion: "v2.0.0",
          source: "branch",
        },
      ],
    },
  },
};

const MOCK_DIFF = {
  ok: true,
  data: {
    group: GROUP,
    repo: REPO,
    ref_a: REF_A,
    ref_b: REF_B,
    summary: {
      entities_added: 1,
      entities_removed: 1,
      entities_modified: 1,
      relationships_added: 0,
      relationships_removed: 0,
      files_changed: 2,
    },
    entities: {
      added: [{ id: "a1", kind: "Function", name: "newFn", source_file: "a.go" }],
      removed: [{ id: "r1", kind: "Function", name: "oldFn", source_file: "b.go" }],
      modified: [
        { id: "m1", kind: "Function", name: "movedFn", source_file: "c.go", modified_fields: ["source_file"] },
      ],
    },
    relationships: { added: [], removed: [] },
  },
};

async function mockAll(page: Page): Promise<void> {
  await page.route(`**/api/v2/groups/${GROUP}/refs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(MOCK_REFS),
    }),
  );
  await page.route(
    `**/api/v2/groups/${GROUP}/repos/${REPO}/diff**`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(MOCK_DIFF),
      }),
  );
}

test.describe("Compare URL persistence (PH5 #2093)", () => {
  test("all params are present in the URL after initial load", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}`,
    );
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    await expect(page).toHaveURL(new RegExp(`repo=${encodeURIComponent(REPO)}`));
    await expect(page).toHaveURL(new RegExp(`refA=${REF_A}`));
    await expect(page).toHaveURL(new RegExp(`refB=`));
  });

  test("filter param is added to URL when filter chip is clicked", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}`,
    );
    await expect(page.getByText(/Showing what changes/i)).toBeVisible({ timeout: 10_000 });

    // Click added filter
    await page.getByRole("button", { name: /\+Added/i }).click();
    await expect(page).toHaveURL(/filter=added/);

    // Click all filter — param should be removed
    await page.getByRole("button", { name: /^All/i }).click();
    await expect(page).not.toHaveURL(/filter=/);
  });

  test("deep-link to filter=removed loads the removed-only view", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}&filter=removed`,
    );
    await expect(page.getByText(/Showing what changes/i)).toBeVisible({ timeout: 10_000 });

    // Only removed entities visible
    await expect(page.getByText("oldFn")).toBeVisible();
    await expect(page.getByText("newFn")).not.toBeVisible();

    // URL still carries the param
    await expect(page).toHaveURL(/filter=removed/);
  });

  test("params survive a page reload", async ({ page }) => {
    const url = `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}&filter=added`;
    await mockAll(page);
    await page.goto(url);
    await expect(page.getByText(/Showing what changes/i)).toBeVisible({ timeout: 10_000 });

    // Re-mock before reload so the daemon calls still work
    await mockAll(page);
    await page.reload();
    await expect(page.getByText(/Showing what changes/i)).toBeVisible({ timeout: 10_000 });

    await expect(page).toHaveURL(/filter=added/);
    await expect(page).toHaveURL(new RegExp(`refA=${REF_A}`));
  });
});
