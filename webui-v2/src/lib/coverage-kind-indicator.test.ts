/**
 * Branch-logic tests for resolveCoverageKindIndicator (#5067) — the compact
 * per-row / per-node coverage-kind chip.
 *
 * Verifies it is a faithful projection of resolveCoverageProvenance: the SAME
 * precedence (line ▸ reachability ▸ capability), the SAME tones, the correct
 * short label per kind, and — load-bearing for honest degradation — that ONLY
 * ingested line coverage is marked `authoritative`. Pure, so it runs under the
 * default (node) vitest environment with no DOM.
 *
 * Run with: npx vitest run src/lib/coverage-kind-indicator.test.ts
 */
import { describe, it, expect } from "vitest";
import {
  resolveCoverageKindIndicator,
  type CoverageSourceState,
} from "./coverage-provenance";

describe("resolveCoverageKindIndicator — which treatment per state", () => {
  it("LINE: ingested line coverage ⇒ success tone, 'Line', authoritative", () => {
    const state: CoverageSourceState = {
      line: { source: "lcov", measuredAt: "2026-06-12T10:00:00Z", pct: 73.4 },
      reachabilityAvailable: true, // line still wins
      reportIngestionConfigured: true,
    };
    const ind = resolveCoverageKindIndicator(state);
    expect(ind.kind).toBe("line");
    expect(ind.tone).toBe("success");
    expect(ind.short).toBe("Line");
    expect(ind.label).toBe("Line coverage");
    expect(ind.authoritative).toBe(true);
    expect(ind.title).toContain("LCOV");
  });

  it("REACH: reachability-only ⇒ info tone, 'Reach', NOT authoritative", () => {
    const state: CoverageSourceState = { reachabilityAvailable: true };
    const ind = resolveCoverageKindIndicator(state);
    expect(ind.kind).toBe("reachability");
    expect(ind.tone).toBe("info");
    expect(ind.short).toBe("Reach");
    expect(ind.label).toBe("Static test-reachability");
    expect(ind.authoritative).toBe(false);
    expect(ind.title.toLowerCase()).toContain("reachability");
  });

  it("CAPABILITY: nothing wired ⇒ neutral tone, 'Capability', NOT authoritative", () => {
    const ind = resolveCoverageKindIndicator({});
    expect(ind.kind).toBe("capability");
    expect(ind.tone).toBe("neutral");
    expect(ind.short).toBe("Capability");
    expect(ind.authoritative).toBe(false);
  });

  it("degrades to capability on null/undefined ⇒ never a misleading authoritative chip", () => {
    for (const s of [null, undefined] as const) {
      const ind = resolveCoverageKindIndicator(s);
      expect(ind.kind).toBe("capability");
      expect(ind.tone).toBe("neutral");
      expect(ind.authoritative).toBe(false);
    }
  });

  it("precedence: line beats reachability beats capability", () => {
    const line = resolveCoverageKindIndicator({
      line: { source: "jacoco" },
      reachabilityAvailable: true,
    });
    expect(line.kind).toBe("line");

    const reach = resolveCoverageKindIndicator({ reachabilityAvailable: true });
    expect(reach.kind).toBe("reachability");

    const cap = resolveCoverageKindIndicator({ reachabilityAvailable: false });
    expect(cap.kind).toBe("capability");
  });

  it("exactly one kind is ever authoritative (only line)", () => {
    expect(
      resolveCoverageKindIndicator({ line: { source: "lcov" } }).authoritative,
    ).toBe(true);
    expect(
      resolveCoverageKindIndicator({ reachabilityAvailable: true }).authoritative,
    ).toBe(false);
    expect(resolveCoverageKindIndicator({}).authoritative).toBe(false);
  });
});
