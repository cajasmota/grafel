#!/usr/bin/env node
/* eslint-disable */
// Headless Playwright screenshots for #1953 — JARVIS two-phase animation.
//
// Captures: idle, mid-Phase-1-arrow-sweep, Phase-2-glow-burst, paused,
// reduced-motion. Output: ./screenshots-1953/*.png at repo root.

import { chromium } from "playwright";
import { mkdirSync, existsSync } from "node:fs";
import path from "node:path";

const ROOT = path.resolve(new URL(".", import.meta.url).pathname, "..");
const OUT = path.join(ROOT, "screenshots-1953");
if (!existsSync(OUT)) mkdirSync(OUT, { recursive: true });

const BASE = process.env.AG_BASE ?? "http://127.0.0.1:47274";
const GROUP = process.env.AG_GROUP ?? "client-fixture-de";

// Each call returns a polyline of returned_node_ids; we pull real IDs from
// the live graph at runtime so the overlay can project them to screen coords.
let FAKE_NODES = null;

async function fetchRealNodeIds(browser) {
  if (FAKE_NODES) return FAKE_NODES;
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(`${BASE}/g/${GROUP}/graph`, { waitUntil: "domcontentloaded" });
  // Wait for graph data to land
  await page.waitForFunction(() => {
    // Cosmos handle exposes nodes via __ag_graph_canvas if wired; otherwise
    // walk react devtools. Simpler: query the API via fetch from in-page.
    return true;
  });
  const ids = await page.evaluate(async (group) => {
    const r = await fetch(`/api/v2/graph/${encodeURIComponent(group)}?lod=overview`);
    if (!r.ok) return null;
    const j = await r.json();
    const nodes = (j?.data?.nodes ?? j?.nodes ?? []).slice(0, 60);
    return nodes.map((n) => n.id ?? n.node_id ?? n.nodeId).filter(Boolean);
  }, GROUP);
  await ctx.close();
  if (!ids || ids.length < 8) {
    // Fall back to synthetic IDs (overlay will skip projection but UI chrome still visible)
    FAKE_NODES = [
      ["a","b","c","d"],["e","f","g"],["h","i"],["j","k","l","m","n"],
    ];
    return FAKE_NODES;
  }
  // Group into 4 calls of varying sizes
  FAKE_NODES = [
    ids.slice(0, 6),
    ids.slice(6, 11),
    ids.slice(11, 14),
    ids.slice(14, 20),
  ].filter((g) => g.length > 0);
  return FAKE_NODES;
}

async function makePage(browser, { reducedMotion = false } = {}) {
  const fakeNodes = await fetchRealNodeIds(browser);
  const ctx = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    reducedMotion: reducedMotion ? "reduce" : "no-preference",
  });
  const page = await ctx.newPage();
  // Stub the SSE stream so replay logic has a synthetic event log we control.
  // We DON'T want real daemon activity to interfere.
  await page.addInitScript((evtNodes) => {
    const SSE_URL = "/api/mcp-activity/stream";
    const realES = window.EventSource;
    function FakeES(url) {
      const target = new EventTarget();
      this.url = url;
      this.readyState = 1;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this.addEventListener = target.addEventListener.bind(target);
      this.removeEventListener = target.removeEventListener.bind(target);
      this.dispatchEvent = target.dispatchEvent.bind(target);
      this.close = () => { this.readyState = 2; };
      if (url === SSE_URL || url.endsWith(SSE_URL)) {
        setTimeout(() => {
          target.dispatchEvent(new Event("connected"));
          this.onopen && this.onopen(new Event("open"));
          evtNodes.forEach((ids, i) => {
            const ev = {
              tool_name: ["archigraph_find_entities", "archigraph_inspect_entity", "archigraph_get_source", "archigraph_traces"][i % 4],
              returned_node_ids: ids,
              returned_edge_ids: [],
              timestamp: Date.now() - (evtNodes.length - i) * 1000,
            };
            setTimeout(() => {
              const me = new MessageEvent("mcp_activity", { data: JSON.stringify(ev) });
              target.dispatchEvent(me);
            }, 200 + i * 80);
          });
        }, 50);
      } else {
        return new realES(url);
      }
    }
    FakeES.CONNECTING = 0; FakeES.OPEN = 1; FakeES.CLOSED = 2;
    window.EventSource = FakeES;
  }, fakeNodes);
  return { ctx, page };
}

async function gotoGraph(page) {
  await page.goto(`${BASE}/g/${GROUP}/graph`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector('[data-testid="mcp-activity-badge"]', { timeout: 15_000 });
  // Open the activity panel
  await page.click('[data-testid="mcp-activity-badge"]');
  await page.waitForSelector('[data-testid="mcp-activity-panel"]', { timeout: 5_000 });
  // Give SSE-fake time to inject events
  await page.waitForTimeout(1500);
}

async function captureIdle(browser) {
  const { ctx, page } = await makePage(browser);
  await gotoGraph(page);
  await page.screenshot({ path: path.join(OUT, "01-idle.png"), fullPage: false });
  await ctx.close();
}

async function captureSweep(browser) {
  const { ctx, page } = await makePage(browser);
  await gotoGraph(page);
  // Lock to slow speed (0.5×) so we can sit mid-sweep.
  await page.click('[data-testid="mcp-replay-speed-0.5"]').catch(() => {});
  await page.click('[data-testid="mcp-replay-all-toggle"]');
  // Phase 1 sweep is ~400ms at 0.5×. Catch around 200ms in.
  await page.waitForTimeout(200);
  await page.screenshot({ path: path.join(OUT, "02-phase1-sweep.png") });
  await ctx.close();
}

async function captureGlow(browser) {
  const { ctx, page } = await makePage(browser);
  await gotoGraph(page);
  await page.click('[data-testid="mcp-replay-speed-0.5"]').catch(() => {});
  await page.click('[data-testid="mcp-replay-all-toggle"]');
  // Phase 1 (~400ms) + early Phase 2: wait 500ms, hopefully glow peak.
  await page.waitForTimeout(550);
  await page.screenshot({ path: path.join(OUT, "03-phase2-glow.png") });
  await ctx.close();
}

async function capturePaused(browser) {
  const { ctx, page } = await makePage(browser);
  await gotoGraph(page);
  await page.click('[data-testid="mcp-replay-speed-0.5"]').catch(() => {});
  await page.click('[data-testid="mcp-replay-all-toggle"]');
  await page.waitForTimeout(300);
  await page.click('[data-testid="mcp-replay-pause"]');
  await page.waitForTimeout(150);
  await page.screenshot({ path: path.join(OUT, "04-paused.png") });
  await ctx.close();
}

async function captureReducedMotion(browser) {
  const { ctx, page } = await makePage(browser, { reducedMotion: true });
  await gotoGraph(page);
  await page.click('[data-testid="mcp-replay-all-toggle"]');
  // Reduced motion: only glow burst fires, no sweep. Grab during glow.
  await page.waitForTimeout(120);
  await page.screenshot({ path: path.join(OUT, "05-reduced-motion.png") });
  await ctx.close();
}

const browser = await chromium.launch({ headless: true });
try {
  await captureIdle(browser);
  await captureSweep(browser);
  await captureGlow(browser);
  await capturePaused(browser);
  await captureReducedMotion(browser);
  console.log("OK — screenshots in", OUT);
} finally {
  await browser.close();
}
