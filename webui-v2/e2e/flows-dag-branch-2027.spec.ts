// webui-v2/e2e/flows-dag-branch-2027.spec.ts
//
// Headless Playwright screenshots for #2027 — JARVIS branch-aware rendering.
//
// Three scenarios:
//   1. Linear flow — DAG canvas with single-comet layout, no branch edges.
//   2. Branched flow — DAG chip + fork×N badges + amber branch arm edges visible.
//   3. Branched flow paused mid-sweep — frozen comets at fan-out positions.
//
// All API responses are intercepted so these run standalone without a daemon.
import { test, type Page } from "@playwright/test";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// ─── Mock data ───────────────────────────────────────────────────────────────

const LINEAR_PROCESS = {
  process_id: "proc-linear-2027",
  label: "UserController.createUser → UserRepository.save",
  repo: "api-service",
  entry_id: "e0",
  entry_name: "UserController.createUser",
  entry_kind: "http_handler",
  terminal_id: "e3",
  step_count: 4,
  cross_stack: false,
  is_cross_repo: false,
  chain_labels: ["createUser", "validate", "hashPassword", "save"],
  is_dag: false,
  steps: [
    { entity_id: "e0", name: "UserController.createUser", step_index: 0, source_file: "src/controllers/user.ts", start_line: 12, repo: "api-service", edge_kind: null, step_kind: "http_fetch" },
    { entity_id: "e1", name: "ValidationService.validate", step_index: 1, source_file: "src/services/validation.ts", start_line: 45, repo: "api-service", edge_kind: "CALLS", step_kind: "validation" },
    { entity_id: "e2", name: "CryptoService.hashPassword", step_index: 2, source_file: "src/services/crypto.ts", start_line: 23, repo: "api-service", edge_kind: "CALLS", step_kind: "transform" },
    { entity_id: "e3", name: "UserRepository.save", step_index: 3, source_file: "src/repos/user.ts", start_line: 78, repo: "api-service", edge_kind: "CALLS", step_kind: "db_write" },
  ],
};

const BRANCHED_PROCESS = {
  process_id: "proc-branched-2027",
  label: "OrderService.processOrder → notify branch",
  repo: "order-service",
  entry_id: "b0",
  entry_name: "OrderService.processOrder",
  entry_kind: "http_handler",
  terminal_id: "b4",
  step_count: 5,
  cross_stack: false,
  is_cross_repo: false,
  chain_labels: ["processOrder", "validateOrder", "notifyEmail", "notifySms", "auditLog"],
  is_dag: true,
  branches_dag: JSON.stringify({
    step_index: 0,
    entity_id: "b0",
    label: "processOrder",
    branches: [{
      step_index: 1,
      entity_id: "b1",
      label: "validateOrder",
      branches: [
        {
          step_index: 2,
          entity_id: "b2",
          label: "notifyEmail",
          branches: [{ step_index: 4, entity_id: "b4", label: "auditLog", branches: [] }],
        },
        {
          step_index: 3,
          entity_id: "b3",
          label: "notifySms",
          branches: [{ step_index: 4, entity_id: "b4", label: "auditLog", branches: [] }],
        },
      ],
    }],
  }),
  steps: [
    { entity_id: "b0", name: "OrderService.processOrder",  step_index: 0, source_file: "src/order.ts",  start_line: 10, repo: "order-service", edge_kind: null,    step_kind: "http_fetch" },
    { entity_id: "b1", name: "OrderService.validateOrder", step_index: 1, source_file: "src/order.ts",  start_line: 34, repo: "order-service", edge_kind: "CALLS", step_kind: "validation" },
    { entity_id: "b2", name: "NotifyService.email",        step_index: 2, source_file: "src/notify.ts", start_line: 12, repo: "order-service", edge_kind: "CALLS", step_kind: "side_effect" },
    { entity_id: "b3", name: "NotifyService.sms",          step_index: 3, source_file: "src/notify.ts", start_line: 28, repo: "order-service", edge_kind: "CALLS", step_kind: "side_effect" },
    { entity_id: "b4", name: "AuditService.log",           step_index: 4, source_file: "src/audit.ts",  start_line: 56, repo: "order-service", edge_kind: "CALLS", step_kind: "db_write" },
  ],
};

// ─── Mock helper ─────────────────────────────────────────────────────────────

async function mockAll(page: Page, proc: typeof LINEAR_PROCESS | typeof BRANCHED_PROCESS) {
  const pid = proc.process_id;

  const metaBody = JSON.stringify({
    version: "dev", daemon_running: true, groups: ["demo"],
  });

  const groupsBody = JSON.stringify([
    { id: "demo", name: "Demo Group", repos: ["api-service"], entityCount: 10,
      fidelity: 0.95, indexedAt: Date.now(), health: "healthy" },
  ]);

  const flowsListBody = JSON.stringify({
    processes: [proc],
    count: 1,
    entry_kind_groups: [{ kind: proc.entry_kind, count: 1 }],
  });

  const flowDetailBody = JSON.stringify({
    process: proc,
    chain_entities: proc.steps,
    source_snippets: {},
  });

  const emptyDeadEnds = JSON.stringify({ dead_ends: [], count: 0 });
  const emptyTruncated = JSON.stringify({ processes: [], count: 0, entry_kind_groups: [] });

  // v2/meta
  await page.route("**/api/v2/meta**", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: metaBody }));
  // v2/groups list
  await page.route("**/api/v2/groups", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: groupsBody }));
  // v2/groups/:id (settings shape — has extra fields)
  await page.route("**/api/v2/groups/demo**", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "demo", name: "Demo", entities: 10, fidelity: 0.95, indexedAt: Date.now(), health: "healthy", features: { watchers: false, gitHooks: false }, docsPath: "", repos: [] }) }));
  // flows list
  await page.route(`**/api/flows/demo?**`, async (r) => r.fulfill({ status: 200, contentType: "application/json", body: flowsListBody }));
  await page.route(`**/api/flows/demo`, async (r) => r.fulfill({ status: 200, contentType: "application/json", body: flowsListBody }));
  // flow detail
  await page.route(`**/api/flows/demo/${encodeURIComponent(pid)}**`, async (r) => r.fulfill({ status: 200, contentType: "application/json", body: flowDetailBody }));
  await page.route(`**/api/flows/demo/${pid}**`, async (r) => r.fulfill({ status: 200, contentType: "application/json", body: flowDetailBody }));
  // dead-ends + truncated
  await page.route("**/api/flows/demo/dead-ends**", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: emptyDeadEnds }));
  await page.route("**/api/flows/demo/truncated**", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: emptyTruncated }));
  // system + misc (catch-all for anything else the app needs)
  await page.route("**/api/system**", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ status: "running", version: "dev", commit_sha: "abc", built_at: "2026-01-01", stale_build: false, pid: 1, rss_mb: 64 }) }));
  // groups (non-v2) — used by legacy hooks
  await page.route("**/api/groups/demo**", async (r) => r.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ id: "demo", name: "Demo", repos: ["api-service"], entityCount: 10, fidelity: 0.95, indexedAt: Date.now(), health: "healthy" }) }));
}

// ─── Tests ───────────────────────────────────────────────────────────────────

test.describe("#2027 — JARVIS branch-aware rendering", () => {
  test("01: linear flow — DAG canvas without branch edges", async ({ page }) => {
    await mockAll(page, LINEAR_PROCESS);
    await page.goto("/g/demo/flows");

    // Wait for any content to render
    await page.waitForTimeout(1000);

    // Take a screenshot of the page state (list + empty detail)
    await page.screenshot({
      path: path.join(__dirname, "../../2027-01-linear-flow.png"),
      fullPage: false,
    });

    // Try to click the flow row if visible
    const flowRow = page.locator("button").filter({ hasText: /UserController|createUser/ }).first();
    const isVisible = await flowRow.isVisible({ timeout: 3000 }).catch(() => false);
    if (isVisible) {
      await flowRow.click();
      await page.waitForTimeout(800);
      await page.screenshot({
        path: path.join(__dirname, "../../2027-01-linear-flow-detail.png"),
        fullPage: false,
      });
    }
  });

  test("02: branched flow — DAG chip + fork badges + branch arm edges", async ({ page }) => {
    await mockAll(page, BRANCHED_PROCESS);
    await page.goto("/g/demo/flows");

    await page.waitForTimeout(1000);

    // Capture the list state showing the branched chip
    await page.screenshot({
      path: path.join(__dirname, "../../2027-02-branched-list.png"),
      fullPage: false,
    });

    // Try to open the detail panel
    const flowRow = page.locator("button").filter({ hasText: /OrderService|processOrder/ }).first();
    const isVisible = await flowRow.isVisible({ timeout: 3000 }).catch(() => false);
    if (isVisible) {
      await flowRow.click();
      await page.waitForTimeout(1000);
      await page.screenshot({
        path: path.join(__dirname, "../../2027-02-branched-flow-dag.png"),
        fullPage: false,
      });
    }
  });

  test("03: branched flow — paused mid-sweep", async ({ page }) => {
    await mockAll(page, BRANCHED_PROCESS);
    await page.goto("/g/demo/flows");

    await page.waitForTimeout(1000);

    const flowRow = page.locator("button").filter({ hasText: /OrderService|processOrder/ }).first();
    const isVisible = await flowRow.isVisible({ timeout: 3000 }).catch(() => false);
    if (isVisible) {
      await flowRow.click();
      await page.waitForTimeout(800);

      // Start replay
      const replayBtn = page.locator("button").filter({ hasText: /Replay all/ }).first();
      const replayVisible = await replayBtn.isVisible({ timeout: 2000 }).catch(() => false);
      if (replayVisible) {
        await replayBtn.click();
        await page.waitForTimeout(320);
        // Pause
        const pauseBtn = page.locator("button").filter({ hasText: /Pause/ }).first();
        await pauseBtn.click().catch(() => {});
        await page.waitForTimeout(120);
      }
    }

    await page.screenshot({
      path: path.join(__dirname, "../../2027-03-branched-paused.png"),
      fullPage: false,
    });
  });
});
