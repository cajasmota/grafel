/**
 * Playwright E2E — worktree subtree in the nav-rail sidebar (#2092 PH4)
 *
 * Tests that:
 *   1. When no worktree refs exist the WorktreeList section is absent.
 *   2. When worktree refs exist they appear as indented rows.
 *   3. Clicking a worktree row updates the URL ?ref= param.
 *
 * Uses route-intercept mocks so the daemon doesn't need to be running.
 */
import { test, expect, type Page } from "@playwright/test";

const GROUP = "client-fixture-a";

/** Inject a mock refs response with worktrees. */
async function mockRefsWithWorktrees(page: Page): Promise<void> {
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
                indexedAt: Date.now() - 120_000,
                indexerVersion: "v2.0.0",
                source: "branch",
              },
              {
                name: "feat/agent-X",
                sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
                shortSha: "bbbbbbb",
                tier: "COLD",
                indexedAt: Date.now() - 7_200_000,
                indexerVersion: "v2.0.0",
                source: "worktree",
              },
            ],
          },
        },
      }),
    }),
  );
}

/** Inject a mock refs response with only branch refs (no worktrees). */
async function mockRefsNoBranch(page: Page): Promise<void> {
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
}

test.describe("worktree subtree in nav-rail sidebar (PH4 #2092)", () => {
  test("worktree section absent when no worktree refs exist", async ({ page }) => {
    await mockRefsNoBranch(page);
    await page.goto(`/g/${GROUP}/graph`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // Wait briefly for refs to load
    await page.waitForTimeout(500);

    // Worktree list should NOT be present
    const worktreeList = page.getByTestId("worktree-list");
    await expect(worktreeList).not.toBeVisible();
  });

  test("worktree rows appear when refs include worktrees", async ({ page }) => {
    await mockRefsWithWorktrees(page);
    await page.goto(`/g/${GROUP}/graph`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // Hover the sidebar to expand it
    const rail = page.locator("aside");
    await rail.hover();

    // Worktree list should be visible
    const worktreeList = page.getByTestId("worktree-list");
    await expect(worktreeList).toBeVisible({ timeout: 3_000 });

    // The worktree row for feat/agent-X should be present
    const worktreeRow = page.getByTestId("worktree-row-feat/agent-X");
    await expect(worktreeRow).toBeVisible({ timeout: 2_000 });
  });

  test("clicking a worktree row switches the ref in the URL", async ({ page }) => {
    await mockRefsWithWorktrees(page);
    await page.goto(`/g/${GROUP}/graph`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    // Hover sidebar to expand
    const rail = page.locator("aside");
    await rail.hover();

    const worktreeRow = page.getByTestId("worktree-row-feat/agent-X");
    await expect(worktreeRow).toBeVisible({ timeout: 3_000 });
    await worktreeRow.click();

    // URL should now include ?ref=feat%2Fagent-X
    await expect(page).toHaveURL(/[?&]ref=feat/);
  });

  test("clicking an active worktree row clears the ref param", async ({ page }) => {
    await mockRefsWithWorktrees(page);
    await page.goto(`/g/${GROUP}/graph?ref=feat%2Fagent-X`);
    await expect(page.getByRole("button", { name: /Switch project/i })).toBeVisible({
      timeout: 10_000,
    });

    const rail = page.locator("aside");
    await rail.hover();

    const worktreeRow = page.getByTestId("worktree-row-feat/agent-X");
    await expect(worktreeRow).toBeVisible({ timeout: 3_000 });
    // Clicking the currently-selected row should toggle it off
    await worktreeRow.click();

    await expect(page).not.toHaveURL(/[?&]ref=/);
  });
});
