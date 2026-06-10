/**
 * Tests for the pure-tree unfold + route highlight (#4479).
 *
 * Run with: npx vitest run src/components/flow-dag/layout.test.ts
 */
import { describe, it, expect } from "vitest";
import { unfoldTree, layoutTree } from "./layout";
import { routeInstanceIds } from "./route";
import {
  nodeBucket,
  isExternalNode,
  nodeModule,
  moduleBand,
} from "./style";
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

describe("nodeBucket taxonomy (#4566)", () => {
  const base = (over: Partial<DownstreamDAGNode>): DownstreamDAGNode => ({
    id: "x",
    name: "x",
    kind: "Operation",
    repo: "api",
    ...over,
  });

  it("classifies the strong backend role priors", () => {
    expect(nodeBucket(base({ role: "endpoint" }))).toBe("endpoint");
    expect(nodeBucket(base({ role: "handler" }))).toBe("handler");
    expect(nodeBucket(base({ role: "collection" }))).toBe("collection");
  });

  it("routes data effects to repository", () => {
    expect(nodeBucket(base({ effects: ["db_read"] }))).toBe("repository");
    expect(nodeBucket(base({ kind: "Repository" }))).toBe("repository");
  });

  it("routes schema/dto kinds to schema", () => {
    expect(nodeBucket(base({ kind: "Schema" }))).toBe("schema");
    expect(nodeBucket(base({ kind: "Operation", subtype: "dto" }))).toBe("schema");
  });

  it("splits service methods from free functions", () => {
    expect(nodeBucket(base({ kind: "Operation", subtype: "method" }))).toBe("service");
    expect(nodeBucket(base({ kind: "Operation", subtype: "function" }))).toBe("function");
  });

  it("paints exceptions red (precedence over everything)", () => {
    expect(nodeBucket(base({ kind: "ExceptionType" }))).toBe("exception");
    expect(nodeBucket(base({ name: "x" }), "THROWS")).toBe("exception");
  });

  it("folds unknown kinds into the neutral node bucket", () => {
    expect(nodeBucket(base({ kind: "WeirdThing", role: undefined }))).toBe("node");
  });

  it("only emits the terminal end-cap for a genuine leaf", () => {
    // isLeaf=true on a plain node → terminal end-cap (#4561).
    expect(nodeBucket(base({ role: "node", kind: "WeirdThing" }), undefined, true)).toBe("terminal");
    // isLeaf=false → keeps its normal bucket (a depth-cut branch).
    expect(nodeBucket(base({ role: "node", kind: "WeirdThing" }), undefined, false)).toBe("node");
    // A service method that is a genuine leaf still becomes the end-cap.
    expect(nodeBucket(base({ kind: "Operation", subtype: "method" }), undefined, true)).toBe("terminal");
    // A collection sink keeps its own bucket even as a leaf.
    expect(nodeBucket(base({ role: "collection" }), undefined, true)).toBe("collection");
  });
});

describe("isExternalNode (#4558/#4564)", () => {
  const base = (over: Partial<DownstreamDAGNode>): DownstreamDAGNode => ({
    id: "x",
    name: "x",
    kind: "Operation",
    repo: "api",
    ...over,
  });

  it("honors the explicit external flag", () => {
    expect(isExternalNode(base({ external: true }))).toBe(true);
  });

  it("detects synthetic/unresolved nodes with no real file", () => {
    expect(isExternalNode(base({ file: "<external>", name: "scope.foo" }))).toBe(true);
    expect(isExternalNode(base({ file: "", kind: "Unresolved" }))).toBe(true);
  });

  it("does NOT mark a resolved internal node external", () => {
    expect(isExternalNode(base({ file: "src/foo.ts" }))).toBe(false);
    // An empty file alone, with no corroborating signal, is not external.
    expect(isExternalNode(base({ file: "" }))).toBe(false);
  });
});

describe("nodeModule + moduleBand (#4557)", () => {
  const nf = (file?: string): DownstreamDAGNode => ({
    id: "x",
    name: "x",
    kind: "Operation",
    repo: "api",
    file,
  });

  it("prefers a modules/<name> anchor segment", () => {
    expect(nodeModule(nf("src/modules/billing/svc.ts")).label).toBe("modules/billing");
  });

  it("falls back to the source directory", () => {
    expect(nodeModule(nf("src/lib/util.ts")).label).toBe("src/lib");
  });

  it("buckets file-less nodes into a shared external module with no band", () => {
    const mod = nodeModule(nf(undefined));
    expect(mod.key).toBe("__external__");
    expect(moduleBand(mod)).toBeNull();
  });

  it("gives same-module nodes the same band color", () => {
    const a = moduleBand(nodeModule(nf("src/modules/billing/a.ts")));
    const b = moduleBand(nodeModule(nf("src/modules/billing/b.ts")));
    expect(a?.color).toBe(b?.color);
  });
});

describe("layoutTree subtree contiguity (#4622)", () => {
  const NODE_W = 268;
  const NODE_H = 76;

  /** Center of an instance along the CROSS axis (the one packed by the tidy tree). */
  function crossCenters(
    rf: ReturnType<typeof layoutTree>["nodes"],
    direction: "LR" | "TB",
  ): Map<string, number> {
    const m = new Map<string, number>();
    for (const node of rf) {
      // cross axis is y for LR (depth→x), x for TB (depth→y).
      const center =
        direction === "LR"
          ? node.position.y + NODE_H / 2
          : node.position.x + NODE_W / 2;
      m.set(node.id, center);
    }
    return m;
  }

  /** [min,max] cross-band of an instance's whole subtree (itself + descendants). */
  function subtreeBand(
    instances: ReturnType<typeof unfoldTree>["instances"],
    centers: Map<string, number>,
    rootId: string,
    halfExtent: number,
  ): [number, number] {
    const ids = new Set<string>([rootId]);
    // path-keyed ids: a descendant id starts with "<rootId>/".
    for (const inst of instances) {
      if (inst.id === rootId || inst.id.startsWith(rootId + "/")) ids.add(inst.id);
    }
    let min = Infinity;
    let max = -Infinity;
    for (const id of ids) {
      const c = centers.get(id)!;
      min = Math.min(min, c - halfExtent);
      max = Math.max(max, c + halfExtent);
    }
    return [min, max];
  }

  it("keeps each child's subtree in a contiguous band that does not overlap a sibling's (LR)", () => {
    // a has two branches: a/b → {a/b/d, a/b/e → a/b/e/f}, and a/c → a/c/g.
    // The bug: a/b's first child sits in a/c's vertical band. Assert disjoint.
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c"), n("d"), n("e"), n("f"), n("g")],
      [
        e("a", "b"),
        e("a", "c"),
        e("b", "d"),
        e("b", "e"),
        e("e", "f"),
        e("c", "g"),
      ],
    );
    const { nodes: rf } = layoutTree(instances, "LR", new Set(), () => {});
    const centers = crossCenters(rf, "LR");
    const half = NODE_H / 2;
    const bandB = subtreeBand(instances, centers, "a/b", half);
    const bandC = subtreeBand(instances, centers, "a/c", half);
    // The two sibling subtrees occupy disjoint cross-axis bands.
    const disjoint = bandB[1] <= bandC[0] || bandC[1] <= bandB[0];
    expect(disjoint).toBe(true);

    // Every descendant of a/b lies within a/b's band (subtree is contiguous and
    // doesn't leak into the sibling).
    for (const inst of instances) {
      if (inst.id === "a/b" || inst.id.startsWith("a/b/")) {
        const c = centers.get(inst.id)!;
        expect(c).toBeGreaterThanOrEqual(bandB[0]);
        expect(c).toBeLessThanOrEqual(bandB[1]);
        // …and is NOT inside the sibling's band.
        const inSibling = c >= bandC[0] && c <= bandC[1];
        expect(inSibling).toBe(false);
      }
    }
  });

  it("centers a parent over its children's cross span (LR)", () => {
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c"), n("d")],
      [e("a", "b"), e("b", "c"), e("b", "d")],
    );
    const { nodes: rf } = layoutTree(instances, "LR", new Set(), () => {});
    const centers = crossCenters(rf, "LR");
    const parent = centers.get("a/b")!;
    const c1 = centers.get("a/b/c")!;
    const c2 = centers.get("a/b/d")!;
    expect(parent).toBeCloseTo((c1 + c2) / 2, 5);
  });

  it("enforces subtree contiguity on the cross axis for TB too", () => {
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c"), n("d"), n("e")],
      [e("a", "b"), e("a", "c"), e("b", "d"), e("c", "e")],
    );
    const { nodes: rf } = layoutTree(instances, "TB", new Set(), () => {});
    const centers = crossCenters(rf, "TB");
    const half = NODE_W / 2;
    const bandB = subtreeBand(instances, centers, "a/b", half);
    const bandC = subtreeBand(instances, centers, "a/c", half);
    const disjoint = bandB[1] <= bandC[0] || bandC[1] <= bandB[0];
    expect(disjoint).toBe(true);
  });

  it("ranks each node along the main axis by its tree depth (LR→x)", () => {
    const { instances } = unfoldTree(
      "a",
      [n("a"), n("b"), n("c")],
      [e("a", "b"), e("b", "c")],
    );
    const { nodes: rf } = layoutTree(instances, "LR", new Set(), () => {});
    const x = new Map(rf.map((node) => [node.id, node.position.x]));
    // deeper nodes are strictly further along the main (x) axis.
    expect(x.get("a")!).toBeLessThan(x.get("a/b")!);
    expect(x.get("a/b")!).toBeLessThan(x.get("a/b/c")!);
  });
});

describe("layoutTree leaf vs truncated (#4561)", () => {
  it("marks a real leaf isLeaf and a depth-cut branch truncatedHere", () => {
    // DAG: a → b → c. Unfold with maxNodes=2 cuts c, so b is childless-but-cut.
    const nodes = [n("a"), n("b"), n("c")];
    const edges = [e("a", "b"), e("b", "c")];
    const { instances, hasOutEdge } = unfoldTree("a", nodes, edges, 2);
    const { nodes: rf } = layoutTree(
      instances,
      "LR",
      new Set(),
      () => {},
      hasOutEdge,
    );
    const byId = new Map(rf.map((x) => [x.id, x.data]));
    // b emitted no child (cut at cap) but source b has an out-edge → truncated.
    expect(byId.get("a/b")?.truncatedHere).toBe(true);
    expect(byId.get("a/b")?.isLeaf).toBe(false);
  });

  it("marks a node with no out-edges as a genuine leaf", () => {
    const nodes = [n("a"), n("b")];
    const edges = [e("a", "b")];
    const { instances, hasOutEdge } = unfoldTree("a", nodes, edges);
    const { nodes: rf } = layoutTree(
      instances,
      "LR",
      new Set(),
      () => {},
      hasOutEdge,
    );
    const byId = new Map(rf.map((x) => [x.id, x.data]));
    // b has no out-edge in the DAG → genuine leaf, not truncated.
    expect(byId.get("a/b")?.isLeaf).toBe(true);
    expect(byId.get("a/b")?.truncatedHere).toBe(false);
  });
});
