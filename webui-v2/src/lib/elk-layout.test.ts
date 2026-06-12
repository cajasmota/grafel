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

describe("centeredPorts (#4874)", () => {
  // With centered ports the edge route must START at the source node's centered
  // LEADING-face point and END at the target node's centered TRAILING-face point,
  // so ELK's bendPoints coincide with React Flow's centered handles.
  it("anchors the route at the centered right/left faces for RIGHT", async () => {
    const nodes: ElkLayoutNode[] = [
      { id: "a", width: 120, height: 40, lane: 0 },
      { id: "b", width: 120, height: 40, lane: 1 },
    ];
    const edges: ElkLayoutEdge[] = [{ id: "e1", source: "a", target: "b" }];

    const { nodes: pos, edges: routes } = await layoutWithElk(nodes, edges, {
      direction: "RIGHT",
      centeredPorts: true,
    });
    const a = pos.get("a")!;
    const b = pos.get("b")!;
    const route = routes.get("e1")!;
    const start = route.points[0];
    const end = route.points[route.points.length - 1];

    // Source leaves the RIGHT-edge center of a; target enters the LEFT-edge
    // center of b. (1px tolerance for ELK's float coords.)
    expect(start.x).toBeCloseTo(a.x + a.width, 1);
    expect(start.y).toBeCloseTo(a.y + a.height / 2, 1);
    expect(end.x).toBeCloseTo(b.x, 1);
    expect(end.y).toBeCloseTo(b.y + b.height / 2, 1);
  });

  it("anchors the route at the centered bottom/top faces for DOWN", async () => {
    const nodes: ElkLayoutNode[] = [
      { id: "a", width: 120, height: 40, lane: 0 },
      { id: "b", width: 120, height: 40, lane: 1 },
    ];
    const edges: ElkLayoutEdge[] = [{ id: "e1", source: "a", target: "b" }];

    const { nodes: pos, edges: routes } = await layoutWithElk(nodes, edges, {
      direction: "DOWN",
      centeredPorts: true,
    });
    const a = pos.get("a")!;
    const b = pos.get("b")!;
    const route = routes.get("e1")!;
    const start = route.points[0];
    const end = route.points[route.points.length - 1];

    // Source leaves the BOTTOM-edge center of a; target enters the TOP-edge
    // center of b (horizontally centered).
    expect(start.x).toBeCloseTo(a.x + a.width / 2, 1);
    expect(start.y).toBeCloseTo(a.y + a.height, 1);
    expect(end.x).toBeCloseTo(b.x + b.width / 2, 1);
    expect(end.y).toBeCloseTo(b.y, 1);
  });

  it("shares ONE centered source trunk for multiple outgoing edges (DOWN)", async () => {
    // a → b and a → c: both must leave from a's single bottom-center port.
    const nodes: ElkLayoutNode[] = [
      { id: "a", width: 120, height: 40, lane: 0 },
      { id: "b", width: 120, height: 40, lane: 1 },
      { id: "c", width: 120, height: 40, lane: 1 },
    ];
    const edges: ElkLayoutEdge[] = [
      { id: "e1", source: "a", target: "b" },
      { id: "e2", source: "a", target: "c" },
    ];
    const { nodes: pos, edges: routes } = await layoutWithElk(nodes, edges, {
      direction: "DOWN",
      centeredPorts: true,
    });
    const a = pos.get("a")!;
    const s1 = routes.get("e1")!.points[0];
    const s2 = routes.get("e2")!.points[0];
    // Both outgoing edges start at the SAME centered bottom-center trunk point.
    expect(s1.x).toBeCloseTo(a.x + a.width / 2, 1);
    expect(s1.y).toBeCloseTo(a.y + a.height, 1);
    expect(s2.x).toBeCloseTo(s1.x, 1);
    expect(s2.y).toBeCloseTo(s1.y, 1);
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
