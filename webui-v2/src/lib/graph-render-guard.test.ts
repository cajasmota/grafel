import { describe, it, expect } from "vitest";
import {
  MIN_TEXTURE_DIM,
  isRenderableGraph,
  clampTextureDim,
  textureSideFor,
  shouldStreamGrow,
} from "./graph-render-guard";

describe("isRenderableGraph (#4605)", () => {
  it("treats an empty graph as NON-renderable (would crash regl)", () => {
    expect(isRenderableGraph(0)).toBe(false);
    expect(isRenderableGraph(0, 0)).toBe(false);
  });

  it("treats a single isolated node as renderable", () => {
    expect(isRenderableGraph(1, 0)).toBe(true);
  });

  it("treats a normal graph as renderable", () => {
    expect(isRenderableGraph(1316, 4200)).toBe(true);
    expect(isRenderableGraph(19000, 50000)).toBe(true);
  });

  it("rejects non-finite / negative node counts", () => {
    expect(isRenderableGraph(NaN)).toBe(false);
    expect(isRenderableGraph(Infinity)).toBe(false);
    expect(isRenderableGraph(-1)).toBe(false);
  });
});

describe("clampTextureDim (#4605)", () => {
  it("clamps 0 to the minimum side", () => {
    expect(clampTextureDim(0)).toBe(MIN_TEXTURE_DIM);
  });

  it("clamps negative dimensions to the minimum", () => {
    expect(clampTextureDim(-5)).toBe(MIN_TEXTURE_DIM);
  });

  it("floors fractional dimensions", () => {
    expect(clampTextureDim(3.9)).toBe(3);
    expect(clampTextureDim(1.2)).toBe(1);
  });

  it("clamps non-finite dimensions to the minimum", () => {
    expect(clampTextureDim(NaN)).toBe(MIN_TEXTURE_DIM);
    expect(clampTextureDim(Infinity)).toBe(MIN_TEXTURE_DIM);
    expect(clampTextureDim(-Infinity)).toBe(MIN_TEXTURE_DIM);
  });

  it("passes through valid dimensions unchanged", () => {
    expect(clampTextureDim(1)).toBe(1);
    expect(clampTextureDim(128)).toBe(128);
  });
});

describe("textureSideFor (#4605)", () => {
  it("never returns a side below the minimum, even for 0/empty", () => {
    expect(textureSideFor(0)).toBe(MIN_TEXTURE_DIM);
    expect(textureSideFor(-3)).toBe(MIN_TEXTURE_DIM);
    expect(textureSideFor(NaN)).toBe(MIN_TEXTURE_DIM);
  });

  it("sizes a single point to a 1x1 texture", () => {
    expect(textureSideFor(1)).toBe(1);
  });

  it("mirrors ceil(sqrt(count)) for real counts", () => {
    expect(textureSideFor(4)).toBe(2);
    expect(textureSideFor(5)).toBe(3); // ceil(sqrt(5)) = 3
    expect(textureSideFor(1316)).toBe(Math.ceil(Math.sqrt(1316)));
  });
});

describe("shouldStreamGrow (#5446)", () => {
  // Buffer lengths are point-count * 2 (x,y per point); the predicate works on
  // raw lengths so the units don't matter — only "did it grow" does.
  it("grows when streaming and the new buffer is larger than what cosmos holds", () => {
    // chunk 2 lands: cosmos holds chunk-1 (200 floats), packed is now 500.
    expect(shouldStreamGrow(true, 200, 500)).toBe(true);
  });

  it("does NOT grow on the FIRST chunk (cosmos buffer still empty)", () => {
    // prevLen === 0 → the fresh-settle seed path owns the first chunk.
    expect(shouldStreamGrow(true, 0, 500)).toBe(false);
  });

  it("does NOT grow when not streaming (full-payload / settled path)", () => {
    expect(shouldStreamGrow(false, 200, 500)).toBe(false);
  });

  it("does NOT grow when the node set did not actually grow", () => {
    expect(shouldStreamGrow(true, 500, 500)).toBe(false); // same size (re-push)
    expect(shouldStreamGrow(true, 500, 300)).toBe(false); // shrank (filter)
  });

  it("fires regardless of any post-settle placed count (the #5446 fix)", () => {
    // The OLD gate also required placedCountRef.current > 0, which a fast early
    // chunk (before the deferred settle) or a cache-hit mount left at 0, so the
    // grown chunk silently fell through to the non-streaming path and never
    // re-heated. This predicate keys only on the uploaded buffer length, so the
    // second chunk grows even when no settle has populated a placed count yet.
    expect(shouldStreamGrow(true, 200, 240)).toBe(true);
  });
});
