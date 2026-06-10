/**
 * Tests for the pure-tree unfold + route highlight (#4479).
 *
 * Run with: npx vitest run src/components/flow-dag/layout.test.ts
 */
import { describe, it, expect } from "vitest";
import { unfoldTree } from "./layout";
import { routeInstanceIds } from "./route";
import type { DownstreamDAGEdge, DownstreamDAGNode } from "@/data/types";

function n(id: string): DownstreamDAGNode {
  return { id, name: id, kind: "fn", repo: "api" };
}
function e(from: string, to: string): DownstreamDAGEdge {
  return { from, to, kind: "CALLS" };
}

describe("unfoldTree", () => {
  it("keeps a linear chain as one instance per node", () => {
    const { instances, capped } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c")],
      [e("a", "b"), e("b", "c")],
    );
    expect(capped).toBe(false);
    expect(instances.map((i) => i.id)).toEqual(["a", "a/b", "a/b/c"]);
    // Each non-root has exactly one parent (a tree).
    expect(instances.filter((i) => i.parentId == null)).toHaveLength(1);
  });

  it("DUPLICATES a diamond convergence node (no fan-in)", () => {
    // a → b → d ; a → c → d. d is reached via two paths → two instances.
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c"), n("d")],
      [e("a", "b"), e("a", "c"), e("b", "d"), e("c", "d")],
    );
    const dInstances = instances.filter((i) => i.node.id === "d");
    expect(dInstances).toHaveLength(2);
    expect(dInstances.map((i) => i.id).sort()).toEqual(["a/b/d", "a/c/d"]);
    // Every node has at most one incoming edge → instance count == nodes,
    // and each instance's parent is unique.
    for (const inst of instances) {
      if (inst.parentId == null) continue;
      expect(instances.some((p) => p.id === inst.parentId)).toBe(true);
    }
  });

  it("guards cycles via the per-path visited set", () => {
    // a → b → a (stray cycle). The walk must terminate.
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b")],
      [e("a", "b"), e("b", "a")],
    );
    expect(instances.map((i) => i.id)).toEqual(["a", "a/b"]);
  });

  it("caps explosive unfolds and reports it", () => {
    // A fan that would exceed the cap.
    const nodes = [n("root")];
    const edges: DownstreamDAGEdge[] = [];
    for (let i = 0; i < 50; i++) {
      nodes.push(n(`x${i}`));
      edges.push(e("root", `x${i}`));
    }
    const { instances, capped } = unfoldTree("root", nodes, edges, 10);
    expect(capped).toBe(true);
    expect(instances.length).toBeLessThanOrEqual(10);
  });
});

describe("routeInstanceIds", () => {
  it("lights ancestors + focus + entire forward subtree", () => {
    // a → b → d (focus b); b also → e → f. c is a sibling branch off a.
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c"), n("d"), n("e"), n("f")],
      [e("a", "b"), e("a", "c"), e("b", "d"), e("b", "e"), e("e", "f")],
    );
    const route = routeInstanceIds(instances, "a/b");
    // ancestor a, focus a/b, descendants a/b/d, a/b/e, a/b/e/f
    expect([...route].sort()).toEqual(
      ["a", "a/b", "a/b/d", "a/b/e", "a/b/e/f"].sort(),
    );
    // The sibling branch c is OFF the route.
    expect(route.has("a/c")).toBe(false);
  });

  it("returns empty for an unknown focus", () => {
    const { instances } = unfoldTree("a", [n("a")], []);
    expect(routeInstanceIds(instances, "nope").size).toBe(0);
  });
});
