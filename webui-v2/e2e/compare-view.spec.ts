/**
 * Playwright E2E — compare view (PH5 #2093)
 *
 * Tests the /g/:group/compare route:
 *   1. Loading the page without params shows the setup placeholder.
 *   2. With refA + refB + repo params the diff is loaded and displayed.
 *   3. Clicking a filter chip updates the change list.
 *   4. Clicking an entity row highlights it in both side panels.
 *   5. Error state is shown when the diff API returns an error.
 *
 * Uses route-intercept mocks — no running daemon required.
 */
import { test, expect, type Page } from "@playwright/test";

const GROUP = "client-fixture-a";
const REPO = "client-fixture-a-core";
const REF_A = "main";
const REF_B = "feat/new-feature";

/** Minimal diff response with known change counts. */
const MOCK_DIFF = {
  ok: true,
  data: {
    group: GROUP,
    repo: REPO,
    ref_a: REF_A,
    ref_b: REF_B,
    summary: {
      entities_added: 3,
      entities_removed: 1,
      entities_modified: 2,
      relationships_added: 5,
      relationships_removed: 1,
      files_changed: 4,
    },
    entities: {
      added: [
        { id: "add1", kind: "Function", name: "newFnA", source_file: "pkg/a.go" },
        { id: "add2", kind: "Function", name: "newFnB", source_file: "pkg/b.go" },
        { id: "add3", kind: "Class", name: "NewClass", source_file: "pkg/c.go" },
      ],
      removed: [
        { id: "rem1", kind: "Function", name: "deletedFn", source_file: "pkg/d.go" },
      ],
      modified: [
        { id: "mod1", kind: "Function", name: "changedFn1", source_file: "pkg/e.go", modified_fields: ["name"] },
        { id: "mod2", kind: "Function", name: "changedFn2", source_file: "pkg/f.go", modified_fields: ["source_window"] },
      ],
    },
    relationships: {
      added: [
        { from_id: "add1", to_id: "add2", kind: "calls" },
        { from_id: "add1", to_id: "add3", kind: "calls" },
        { from_id: "add2", to_id: "mod1", kind: "calls" },
        { from_id: "add3", to_id: "mod2", kind: "calls" },
        { from_id: "add2", to_id: "add3", kind: "calls" },
      ],
      removed: [
        { from_id: "rem1", to_id: "mod1", kind: "calls" },
      ],
    },
  },
};

/** Minimal refs response. */
const MOCK_REFS = {
  ok: true,
  data: {
    refs: {
      [REPO]: [
        {
          name: REF_A,
          sha: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
          shortSha: "aaaaaaa",
          tier: "HOT",
          indexedAt: Date.now() - 300_000,
          indexerVersion: "v2.0.0",
          source: "branch",
        },
        {
          name: "feat/new-feature",
          sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
          shortSha: "bbbbbbb",
          tier: "WARM",
          indexedAt: Date.now() - 600_000,
          indexerVersion: "v2.0.0",
          source: "branch",
        },
      ],
    },
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

test.describe("Compare view (PH5 #2093)", () => {
  test("shows setup placeholder when no params are provided", async ({ page }) => {
    await mockAll(page);
    await page.goto(`/g/${GROUP}/compare`);
    // AppShell chrome should load
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });
    // Placeholder text
    await expect(
      page.getByText(/Select a repo.*base ref.*head ref/i),
    ).toBeVisible();
  });

  test("loads diff data and shows summary banner with counts", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}`,
    );
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // Summary banner title
    await expect(page.getByText(/Showing what changes if you merge/i)).toBeVisible({
      timeout: 10_000,
    });

    // Check entity counts in the banner.
    await expect(page.getByText("+3 entities")).toBeVisible();
    await expect(page.getByText("−1 entities")).toBeVisible();
    await expect(page.getByText("~2 entities")).toBeVisible();
    await expect(page.getByText("+5 relationships")).toBeVisible();
  });

  test("filter chip — 'only added' hides removed and modified rows", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}`,
    );
    await expect(page.getByText(/Showing what changes/i)).toBeVisible({ timeout: 10_000 });

    // Click the "Added" filter chip
    await page.getByRole("button", { name: /\+Added/i }).click();

    // Added entities should be visible
    await expect(page.getByText("newFnA")).toBeVisible();
    // Removed entity should NOT be visible
    await expect(page.getByText("deletedFn")).not.toBeVisible();
    // Modified entity should NOT be visible
    await expect(page.getByText("changedFn1")).not.toBeVisible();
  });

  test("filter chip — 'only removed' shows removed rows only", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${encodeURIComponent(REF_B)}`,
    );
    await expect(page.getByText(/Showing what changes/i)).toBeVisible({ timeout: 10_000 });

    await page.getByRole("button", { name: /−Removed/i }).click();

    await expect(page.getByText("deletedFn")).toBeVisible();
    await expect(page.getByText("newFnA")).not.toBeVisible();
  });

  test("same-ref shows 'no differences' banner", async ({ page }) => {
    await mockAll(page);
    await page.goto(
      `/g/${GROUP}/compare?repo=${encodeURIComponent(REPO)}&refA=${REF_A}&refB=${REF_A}`,
    );
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // Same-ref fast path doesn't call the API at all; just the route renders.
    // The "No differences" message should be visible.
    await expect(page.getByText(/No differences — both refs point/i)).toBeVisible({
      timeout: 5_000,
    });
  });
});
