/* elk-layout.test.ts — shared elkjs layout helper (#4825). Verifies the helper
   lays out a nested/compound graph (container + children + cross-container edge)
   and returns finite parent-relative positions + a sized container box. */

import { describe, it, expect } from "vitest";
import {
  layoutWithElk,
  orthogonalPath,
  type ElkLayoutNode,
  type ElkLayoutEdge,
} from "./elk-layout";

describe("layoutWithElk", () => {
  it("returns positions for a nested compound graph", async () => {
    // group A contains a + b; standalone c. Edges a→b (inside A) and b→c (cross).
    const nodes: ElkLayoutNode[] = [
      { id: "A", isContainer: true },
      { id: "a", parentId: "A", width: 120, height: 40, lane: 0 },
      { id: "b", parentId: "A", width: 120, height: 40, lane: 1 },
      { id: "c", width: 120, height: 40, lane: 2 },
    ];
    const edges: ElkLayoutEdge[] = [
      { id: "e1", source: "a", target: "b" },
      { id: "e2", source: "b", target: "c" },
    ];

    const { nodes: pos } = await layoutWithElk(nodes, edges, { direction: "RIGHT" });

    for (const id of ["A", "a", "b", "c"]) {
      const p = pos.get(id);
      expect(p, `position for ${id}`).toBeDefined();
      expect(Number.isFinite(p!.x)).toBe(true);
      expect(Number.isFinite(p!.y)).toBe(true);
    }

    // The container A is sized from its children (non-zero bounding box).
    const a = pos.get("A")!;
    expect(a.width).toBeGreaterThan(0);
    expect(a.height).toBeGreaterThan(0);

    // Children a/b carry their measured leaf size.
    expect(pos.get("a")!.width).toBe(120);
    expect(pos.get("b")!.height).toBe(40);
  });

  it("respects lane order along the flow direction (left→right)", async () => {
    // Three unconnected nodes with ascending lanes should land in lane order
    // along x (RIGHT direction) thanks to the layer constraint hint.
    const nodes: ElkLayoutNode[] = [
      { id: "n0", width: 100, height: 40, lane: 0 },
      { id: "n1", width: 100, height: 40, lane: 1 },
      { id: "n2", width: 100, height: 40, lane: 2 },
    ];
    const { nodes: pos } = await layoutWithElk(nodes, [], { direction: "RIGHT" });
    const x0 = pos.get("n0")!.x;
    const x1 = pos.get("n1")!.x;
    const x2 = pos.get("n2")!.x;
    expect(x0).toBeLessThanOrEqual(x1);
    expect(x1).toBeLessThanOrEqual(x2);
  });

  it("returns an empty result for no nodes", async () => {
    const { nodes, edges } = await layoutWithElk([], []);
    expect(nodes.size).toBe(0);
    expect(edges.size).toBe(0);
  });

  it("returns ELK orthogonal edge routes (bendPoints) per edge (#4843)", async () => {
    // A simple 2-node graph yields a route with ≥2 points (start + end).
    const nodes: ElkLayoutNode[] = [
      { id: "a", width: 120, height: 40, lane: 0 },
      { id: "b", width: 120, height: 40, lane: 1 },
    ];
    const edges: ElkLayoutEdge[] = [{ id: "e1", source: "a", target: "b" }];

    const { nodes: pos, edges: routes } = await layoutWithElk(nodes, edges, {
      direction: "RIGHT",
    });

    const route = routes.get("e1");
    expect(route, "route for e1").toBeDefined();
    expect(route!.points.length).toBeGreaterThanOrEqual(2);
    for (const pt of route!.points) {
      expect(Number.isFinite(pt.x)).toBe(true);
      expect(Number.isFinite(pt.y)).toBe(true);
    }
    // The route runs from near node a to near node b (RIGHT direction → x grows).
    const a = pos.get("a")!;
    const b = pos.get("b")!;
    const start = route!.points[0];
    const end = route!.points[route!.points.length - 1];
    expect(start.x).toBeGreaterThanOrEqual(a.x - 1);
    expect(end.x).toBeLessThanOrEqual(b.x + b.width + 1);
    expect(end.x).toBeGreaterThan(start.x);
  });

  it("translates routes of a cross-container edge into absolute flow coords", async () => {
    // group A contains a; standalone c. Edge a→c is a cross-container edge ELK
    // stores at the root → its points must already be absolute.
    const nodes: ElkLayoutNode[] = [
      { id: "A", isContainer: true },
      { id: "a", parentId: "A", width: 120, height: 40, lane: 0 },
      { id: "c", width: 120, height: 40, lane: 1 },
    ];
    const edges: ElkLayoutEdge[] = [{ id: "e1", source: "a", target: "c" }];

    const { edges: routes } = await layoutWithElk(nodes, edges, { direction: "RIGHT" });
    const route = routes.get("e1");
    expect(route).toBeDefined();
    expect(route!.points.length).toBeGreaterThanOrEqual(2);
  });
});

describe("orthogonalPath", () => {
  it("returns null for <2 points (caller falls back to smoothstep)", () => {
    expect(orthogonalPath([])).toBeNull();
    expect(orthogonalPath([{ x: 0, y: 0 }])).toBeNull();
  });

  it("builds a path with a mid-length label for a routed polyline", () => {
    const res = orthogonalPath([
      { x: 0, y: 0 },
      { x: 50, y: 0 },
      { x: 50, y: 40 },
      { x: 100, y: 40 },
    ]);
    expect(res).not.toBeNull();
    expect(res!.path.startsWith("M 0,0")).toBe(true);
    expect(Number.isFinite(res!.labelX)).toBe(true);
    expect(Number.isFinite(res!.labelY)).toBe(true);
  });
});
