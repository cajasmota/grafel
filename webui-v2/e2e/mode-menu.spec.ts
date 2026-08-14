import { expect, test } from "@playwright/test";

const modeReply = {
  ok: true,
  data: {
    mode: "workstation",
    effective_mode: "workstation",
    description: "Production defaults.",
    env_defaults: {},
    all_modes: [
      { name: "background", description: "Low-footprint mode.", env_defaults: {} },
      { name: "workstation", description: "Production defaults.", env_defaults: {} },
      { name: "readonly", description: "Query-only mode.", env_defaults: {} },
    ],
  },
};

test("mode badge opens the switcher and reaches the confirmation dialog", async ({ page }) => {
  await page.route("**/api/v2/daemon/mode*", async (route) => {
    if (route.request().method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify(modeReply),
      });
      return;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ok: true,
        data: { mode: "background", config_path: "test", restart_initiated: false },
      }),
    });
  });

  await page.goto("/g/demo/operations");

  const badge = page.getByRole("button", {
    name: "Daemon mode: workstation. Click to switch.",
  });
  await expect(badge).toBeVisible({ timeout: 10_000 });
  await badge.click();

  await expect(page.getByText("Daemon Mode")).toBeVisible();

  await page.getByRole("button", { name: "Switch", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "Switch to background mode?" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Confirm restart" })).toBeVisible();
});
