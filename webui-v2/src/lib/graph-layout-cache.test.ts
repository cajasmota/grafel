import { describe, it, expect } from "vitest";
import { isDegenerateLayout, isLayoutHealthy } from "./graph-layout-cache";

/**
 * Build a Float32Array of [x0,y0,x1,y1,...] for `n` nodes spread uniformly on a
 * square grid of side `span` (so the bbox is exactly span×span). Used to model a
 * settled layout at a chosen density.
 */
function gridLayout(n: number, spanX: number, spanY = spanX): Float32Array {
  const out = new Float32Array(n * 2);
  const cols = Math.max(1, Math.ceil(Math.sqrt(n)));
  const rows = Math.max(1, Math.ceil(n / cols));
  const stepX = cols > 1 ? spanX / (cols - 1) : 0;
  const stepY = rows > 1 ? spanY / (rows - 1) : 0;
  for (let i = 0; i < n; i++) {
    const c = i % cols;
    const r = Math.floor(i / cols);
    out[i * 2] = c * stepX;
    out[i * 2 + 1] = r * stepY;
  }
  return out;
}

describe("isDegenerateLayout", () => {
  it("does not flag tiny graphs (<2 nodes)", () => {
    expect(isDegenerateLayout(new Float32Array([0, 0]))).toBe(false);
    expect(isDegenerateLayout(new Float32Array([]))).toBe(false);
  });

  it("flags a fully collapsed layout (all nodes near one point)", () => {
    const n = 1000;
    const out = new Float32Array(n * 2);
    for (let i = 0; i < n; i++) {
      out[i * 2] = Math.random() * 5; // 5-unit span — far below the floor
      out[i * 2 + 1] = Math.random() * 5;
    }
    expect(isDegenerateLayout(out)).toBe(true);
  });

  it("flags any non-finite coordinate as degenerate", () => {
    const bad = gridLayout(100, 1000);
    bad[10] = NaN;
    expect(isDegenerateLayout(bad)).toBe(true);
    const inf = gridLayout(100, 1000);
    inf[11] = Infinity;
    expect(isDegenerateLayout(inf)).toBe(true);
  });

  it("accepts a healthy, well-spread large layout", () => {
    // 14k nodes spread across a wide canvas: area/node ≈ (12000²)/14233 ≈ 10117 — a
    // genuinely spread settle is never rejected.
    const layout = gridLayout(14233, 12000);
    expect(isDegenerateLayout(layout)).toBe(false);
    expect(isLayoutHealthy(layout, 14233 * 2)).toBe(true);
  });

  // Fix #4844 — the regression this fix targets: a DENSE HAIRBALL on a large graph
  // whose WIDEST-axis span clears the span floor (sqrt(n)×6 ≈ 716 for 14233 nodes)
  // but packs all the nodes into a small box. The old span-only check accepted it,
  // so reload PINNED the collapsed cache while Reset spread it. The area-per-node
  // gate now flags it as degenerate so the caller re-settles (reload === Reset).
  it("flags a dense hairball whose span clears the span floor (#4844)", () => {
    const n = 14233;
    // Model an elongated hairball: widest axis (spanX) clears the span floor
    // (sqrt(n)×6 ≈ 716) so the OLD span-only check accepted it, but the bbox AREA
    // (spanX×spanY) is below the density floor (n×25 ≈ 355,825), so the new
    // area-per-node gate flags it. spanX=1000 (>716) × spanY=300 → area 300,000.
    const spanX = 1000;
    const spanY = 300;
    const hairball = gridLayout(n, spanX, spanY);
    // sanity: widest-axis span must clear the span floor so we're exercising the
    // NEW density gate, not the old span gate.
    expect(Math.max(spanX, spanY)).toBeGreaterThan(Math.sqrt(n) * 6);
    expect(spanX * spanY).toBeLessThan(n * 25);
    expect(isDegenerateLayout(hairball)).toBe(true);
    expect(isLayoutHealthy(hairball, n * 2)).toBe(false);
  });

  it("does not engage the density gate below the 500-node guard", () => {
    // A small graph with a modest-but-valid span is governed by the span floor only;
    // the density gate must not fire and wrongly reject it.
    const n = 400;
    const span = Math.sqrt(n) * 8; // clears sqrt(n)×6 span floor
    const layout = gridLayout(n, span);
    expect(isDegenerateLayout(layout)).toBe(false);
  });
});
