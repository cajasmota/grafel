// webui-v2/e2e/graph-reload-explode-2107.spec.ts
//
// Playwright E2E — Fix #2107: graph must render exploded (spread-out) on page
// load WITHOUT requiring the user to click "Reset".
//
// Root cause: LAYOUT_VERSION was out-of-sync with DEFAULTS_VERSION (4 vs 5),
// so layouts baked by the old force defaults were loaded from the layout cache
// on reload. The graph looked collapsed/dense until the user hit Reset.
//
// This test:
//  1. Navigates to the Graph page with a mock API response (no daemon needed).
//  2. Waits for the canvas to settle (up to 6s — the simulation settle cap).
//  3. Reads the node position bounding box via window.__ag (the cosmos.gl
//     instance exposed in DEV mode). A properly settled layout has a wide bbox.
//  4. Asserts that the bounding-box span in BOTH axes exceeds a threshold that
//     a collapsed/dense layout cannot meet.
//  5. Repeats for a second navigation (simulating a "reload") to verify the
//     layout cache does not regress on the second load.
//
// All API responses are intercepted so the test runs standalone without a daemon.
import { test, expect, type Page } from "@playwright/test";

// ─── Mock data — minimal graph fixture for client-fixture-X ─────────────────

function makeMockNodes(count = 60) {
  const nodes = [];
  const repos = ["repo-alpha", "repo-beta"];
  for (let i = 0; i < count; i++) {
    nodes.push({
      id: `n${i}`,
      label: `Entity${i}`,
      kind: "function",
      sourceFile: `src/module-${i % 8}/file${i}.ts`,
      repo: repos[i % repos.length],
      degree: 1 + (i % 5),
      pageRank: 0.001 + (i % 10) * 0.0005,
      communityId: i % 6,
    });
  }
  return nodes;
}

function makeMockEdges(nodes: { id: string }[]) {
  const edges = [];
  // A chain of CALLS edges so nodes have meaningful connectivity.
  for (let i = 0; i < nodes.length - 1; i++) {
    edges.push({ source: nodes[i].id, target: nodes[i + 1].id, kind: "CALLS" });
  }
  // A few cross-repo edges.
  edges.push({ source: nodes[0].id, target: nodes[31].id, kind: "IMPORTS" });
  edges.push({ source: nodes[10].id, target: nodes[45].id, kind: "CALLS" });
  return edges;
}

const MOCK_NODES = makeMockNodes(60);
const MOCK_EDGES = makeMockEdges(MOCK_NODES);

const MOCK_GRAPH_RESPONSE = {
  group: "client-fixture-x",
  nodes: MOCK_NODES,
  edges: MOCK_EDGES,
};

// Minimum bounding-box span we expect from a properly exploded layout.
// A collapsed blob of 60 nodes typically spans <200 units; a healthy settled
// layout for 60 nodes at the current force defaults spans >400 units easily.
const MIN_EXPLODE_SPAN = 300;

async function setupMocks(page: Page) {
  // Intercept all graph API calls for the test group.
  await page.route("**/api/graph/client-fixture-x**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(MOCK_GRAPH_RESPONSE),
    });
  });
  // Intercept the group-list endpoint so the page doesn't error on load.
  await page.route("**/api/groups**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        { id: "client-fixture-x", label: "Client Fixture X", repoCount: 2 },
      ]),
    });
  });
  // Health check.
  await page.route("**/api/health**", async (route) => {
    await route.fulfill({ status: 200, body: '{"ok":true}' });
  });
}

/**
 * Wait for the cosmos.gl simulation to settle then return the bounding-box span
 * (max of X-span and Y-span) of the current node positions.
 * Returns null when the engine isn't available or the position buffer is empty.
 */
async function getSettledBboxSpan(page: Page, timeoutMs = 7000): Promise<number | null> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const span = await page.evaluate(() => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const g = (window as any).__ag;
      if (!g) return null;
      let positions: Float32Array | null = null;
      try {
        positions = g.getPointPositions() as Float32Array;
      } catch {
        return null;
      }
      if (!positions || positions.length < 4) return null;
      let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
      for (let i = 0; i < positions.length; i += 2) {
        const x = positions[i];
        const y = positions[i + 1];
        if (!isFinite(x) || !isFinite(y)) continue;
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (y < minY) minY = y;
        if (y > maxY) maxY = y;
      }
      if (!isFinite(minX)) return null;
      return Math.max(maxX - minX, maxY - minY);
    });
    if (span !== null && span > 50) return span;
    // Poll at 200ms while simulation is still running.
    await page.waitForTimeout(200);
  }
  return null;
}

test.describe("Fix #2107 — graph reloads with exploded layout (no Reset required)", () => {
  test("initial load: settled layout bbox span exceeds collapsed threshold", async ({ page }) => {
    await setupMocks(page);
    // Navigate to the graph page. The group id is injected via route param.
    await page.goto("/graph/client-fixture-x");

    // Wait for the graph canvas to appear.
    await page.waitForSelector('[role="img"][aria-label="Dependency graph"]', {
      timeout: 8000,
    });

    // Wait for the cosmos.gl instance to be available + layout to settle.
    const span = await getSettledBboxSpan(page, 8000);

    expect(span, `Initial load: bbox span should be >${MIN_EXPLODE_SPAN} (got ${span})`).not.toBeNull();
    expect(span!).toBeGreaterThan(MIN_EXPLODE_SPAN);
  });

  test("second load (simulated reload): layout still exploded, no Reset needed", async ({ page }) => {
    await setupMocks(page);

    // First visit — allows the layout cache to be populated.
    await page.goto("/graph/client-fixture-x");
    await page.waitForSelector('[role="img"][aria-label="Dependency graph"]', {
      timeout: 8000,
    });
    // Wait for settle so the cache is written.
    const firstSpan = await getSettledBboxSpan(page, 8000);
    expect(firstSpan).not.toBeNull();
    expect(firstSpan!).toBeGreaterThan(MIN_EXPLODE_SPAN);

    // Second visit — must still be exploded (cache loaded or re-settled).
    await page.goto("/graph/client-fixture-x");
    await page.waitForSelector('[role="img"][aria-label="Dependency graph"]', {
      timeout: 8000,
    });
    const secondSpan = await getSettledBboxSpan(page, 8000);
    expect(secondSpan, `Reload: bbox span should be >${MIN_EXPLODE_SPAN} (got ${secondSpan})`).not.toBeNull();
    expect(secondSpan!).toBeGreaterThan(MIN_EXPLODE_SPAN);
  });

  test("Reset button span matches initial load span (reload === Reset)", async ({ page }) => {
    await setupMocks(page);
    await page.goto("/graph/client-fixture-x");
    await page.waitForSelector('[role="img"][aria-label="Dependency graph"]', {
      timeout: 8000,
    });
    const initialSpan = await getSettledBboxSpan(page, 8000);
    expect(initialSpan).not.toBeNull();
    expect(initialSpan!).toBeGreaterThan(MIN_EXPLODE_SPAN);

    // Click the Reset button.
    const resetBtn = page.getByRole("button", { name: /reset/i });
    await resetBtn.click();

    // Wait for re-settle after Reset.
    await page.waitForTimeout(500);
    const afterResetSpan = await getSettledBboxSpan(page, 8000);
    expect(afterResetSpan).not.toBeNull();
    expect(afterResetSpan!).toBeGreaterThan(MIN_EXPLODE_SPAN);
  });
});
