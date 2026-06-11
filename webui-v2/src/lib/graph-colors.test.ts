/**
 * Fix #4852 — link-alpha model tests.
 *
 * Covers the pure alpha seam factored out of graph-canvas:
 *   - linkTierAlpha  : per-tier base alpha (faded < subtle < emphasized).
 *   - packedLinkAlpha: the value baked into color.a so that, after the shader's
 *     linkOpacity multiply, the rendered alpha never compounds below the floor.
 *
 * The key invariant this guards is the #4852 bug: the cosmos frag shader
 * multiplies the packed color.a by the linkOpacity uniform AGAIN, and the tier
 * alpha was ALSO scaled by linkOpacity, so a low slider setting drove faded
 * links toward invisibility. packedLinkAlpha keeps the rendered product
 * (color.a × linkOpacity) at or above LINK_ALPHA_FLOOR.
 *
 * Run with: npx vitest run src/lib/graph-colors.test.ts
 */
import { describe, it, expect } from "vitest";
import {
  LINK_ALPHA_FLOOR,
  linkTierAlpha,
  packedLinkAlpha,
  type LinkTier,
} from "./graph-colors";

const TIERS: LinkTier[] = ["faded", "subtle", "emphasized"];

describe("linkTierAlpha", () => {
  it("orders faded < subtle <= emphasized in single-repo mode", () => {
    const f = linkTierAlpha("faded", 0.6, false);
    const s = linkTierAlpha("subtle", 0.6, false);
    const e = linkTierAlpha("emphasized", 0.6, false);
    expect(f).toBeLessThan(s);
    expect(s).toBeLessThanOrEqual(e);
  });

  it("orders faded < subtle < emphasized in multi-repo mode", () => {
    const f = linkTierAlpha("faded", 0.6, true);
    const s = linkTierAlpha("subtle", 0.6, true);
    const e = linkTierAlpha("emphasized", 0.6, true);
    expect(f).toBeLessThan(s);
    expect(s).toBeLessThan(e);
  });

  it("stays within [0,1] across the full slider range and both modes", () => {
    for (const isMulti of [false, true]) {
      for (let op = 0; op <= 1.0001; op += 0.05) {
        for (const t of TIERS) {
          const a = linkTierAlpha(t, op, isMulti);
          expect(a).toBeGreaterThanOrEqual(0);
          expect(a).toBeLessThanOrEqual(1);
        }
      }
    }
  });
});

describe("packedLinkAlpha — combined-alpha floor (#4852)", () => {
  it("renders at the floor whenever opacity allows it (op >= floor)", () => {
    // For op >= FLOOR the packed alpha can reach the floor without exceeding a
    // valid [0,1] opacity, so rendered = color.a × op must be >= the floor.
    for (const isMulti of [false, true]) {
      for (let op = LINK_ALPHA_FLOOR; op <= 1.0001; op += 0.01) {
        for (const t of TIERS) {
          const packed = packedLinkAlpha(t, op, isMulti);
          const rendered = packed * op; // what the shader actually outputs
          expect(rendered).toBeGreaterThanOrEqual(LINK_ALPHA_FLOOR - 1e-6);
        }
      }
    }
  });

  it("at very low opacity (op < floor) renders as bright as possible (color.a clamps to 1)", () => {
    // Below the floor the floor is physically unreachable (can't render > full
    // opacity); the best we can do is color.a = 1, i.e. rendered ≈ op. Verify we
    // saturate there rather than silently dropping the link.
    for (const isMulti of [false, true]) {
      for (let op = 0.01; op < LINK_ALPHA_FLOOR; op += 0.01) {
        for (const t of TIERS) {
          const packed = packedLinkAlpha(t, op, isMulti);
          const rendered = packed * op;
          expect(packed).toBeCloseTo(1, 6);
          expect(rendered).toBeCloseTo(op, 6);
        }
      }
    }
  });

  it("keeps the packed alpha a valid opacity in [0,1]", () => {
    for (const isMulti of [false, true]) {
      for (let op = 0; op <= 1.0001; op += 0.05) {
        for (const t of TIERS) {
          const packed = packedLinkAlpha(t, op, isMulti);
          expect(packed).toBeGreaterThanOrEqual(0);
          expect(packed).toBeLessThanOrEqual(1);
        }
      }
    }
  });

  it("preserves tier ORDER after the floor at a healthy default opacity", () => {
    // At the default 0.55 the floor shouldn't have collapsed the emphasis gaps.
    const op = 0.55;
    for (const isMulti of [false, true]) {
      const f = packedLinkAlpha("faded", op, isMulti) * op;
      const s = packedLinkAlpha("subtle", op, isMulti) * op;
      const e = packedLinkAlpha("emphasized", op, isMulti) * op;
      expect(f).toBeLessThanOrEqual(s);
      expect(s).toBeLessThanOrEqual(e);
    }
  });

  it("the floor only lifts the LOW end — emphasized at full opacity is untouched", () => {
    // emphasized tier × linkOpacity is already well above the floor, so
    // packedLinkAlpha should equal the raw tier alpha (no flooring).
    const raw = linkTierAlpha("emphasized", 1.0, false);
    const packed = packedLinkAlpha("emphasized", 1.0, false);
    expect(packed).toBeCloseTo(Math.min(1, raw), 6);
  });

  it("does not divide-by-zero when links are turned fully off", () => {
    for (const t of TIERS) {
      const packed = packedLinkAlpha(t, 0, false);
      expect(Number.isFinite(packed)).toBe(true);
      expect(packed).toBeLessThanOrEqual(1);
    }
  });
});
