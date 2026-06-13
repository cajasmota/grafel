import { describe, expect, it } from "vitest";

import type { ReachabilitySummary } from "../data/types";
import { resolveReachabilityView } from "./reachability-summary";

function summary(over: Partial<ReachabilitySummary>): ReachabilitySummary {
  return {
    computed: true,
    total_endpoints: 0,
    tested_endpoints: 0,
    orphan_endpoints: 0,
    reachable_pct: 0,
    orphans: [],
    ...over,
  };
}

describe("resolveReachabilityView", () => {
  it("degrades to not-computed when the summary is absent (older backend)", () => {
    const v = resolveReachabilityView(undefined);
    expect(v.kind).toBe("not-computed");
    expect(v.orphans).toBe(0);
    expect(v.label).toMatch(/not computed/i);
  });

  it("degrades to not-computed when the pass never ran, NOT implying untested", () => {
    const v = resolveReachabilityView(summary({ computed: false }));
    expect(v.kind).toBe("not-computed");
    // Must not report orphans/untested counts when nothing was computed.
    expect(v.orphans).toBe(0);
    expect(v.total).toBe(0);
    expect(v.orphanRows).toHaveLength(0);
  });

  it("returns all-tested when there are zero orphans", () => {
    const v = resolveReachabilityView(
      summary({
        total_endpoints: 4,
        tested_endpoints: 4,
        orphan_endpoints: 0,
        reachable_pct: 100,
      }),
    );
    expect(v.kind).toBe("all-tested");
    expect(v.orphanRows).toHaveLength(0);
    expect(v.reachablePct).toBe(100);
    expect(v.label).toMatch(/all 4 endpoints/i);
  });

  it("returns has-orphans with the orphan rows when some endpoints are untested", () => {
    const v = resolveReachabilityView(
      summary({
        total_endpoints: 5,
        tested_endpoints: 3,
        orphan_endpoints: 2,
        reachable_pct: 60,
        orphans: [
          { id: "e1", name: "POST /inspections", kind: "SCOPE.Endpoint" },
          { id: "e2", name: "GET /audit", kind: "SCOPE.Endpoint" },
        ],
        orphans_more: 0,
      }),
    );
    expect(v.kind).toBe("has-orphans");
    expect(v.orphans).toBe(2);
    expect(v.orphanRows).toHaveLength(2);
    expect(v.reachablePct).toBe(60);
    expect(v.label).toMatch(/2 untested endpoints/i);
  });

  it("propagates the orphans_more cap overflow", () => {
    const v = resolveReachabilityView(
      summary({
        total_endpoints: 250,
        tested_endpoints: 0,
        orphan_endpoints: 250,
        orphans: [{ id: "e1", name: "GET /x", kind: "SCOPE.Endpoint" }],
        orphans_more: 49,
      }),
    );
    expect(v.kind).toBe("has-orphans");
    expect(v.orphansMore).toBe(49);
  });

  it("handles a singular orphan label", () => {
    const v = resolveReachabilityView(
      summary({
        total_endpoints: 1,
        tested_endpoints: 0,
        orphan_endpoints: 1,
        orphans: [{ id: "e1", name: "GET /x", kind: "SCOPE.Endpoint" }],
      }),
    );
    expect(v.label).toMatch(/1 untested endpoint\b/i);
  });
});
