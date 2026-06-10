/* ============================================================
   components/flow-dag/layout.ts — pure-tree unfold + tidy-tree layout for <FlowDag>.

   The daemon ships a deduped DAG (each node id once; a convergence node carries
   >1 in-edge). The renderer UNFOLDS that DAG into a pure TREE (#4479): starting
   at `root_id` we walk the edges and DUPLICATE any node reached via more than
   one path, so every rendered instance has exactly ONE incoming edge and may
   have many outgoing. The result lays out cleanly as a tidy tree with no
   fan-in / crossing edges.

   Each instance is keyed by its path ("<parentInstanceId>/<nodeId>") and carries
   the ORIGINAL node's data (name/kind/role/file/… ) for the detail panel and the
   caller's selection contract. Safety: the unfold honors the depth control and a
   hard max-node cap, and tracks the path's visited set so a diamond — or a stray
   cycle, which shouldn't occur in a DAG — can never infinitely expand or hang the
   browser. When a cap fires we stop expanding and surface honest truncation.

   The H/V toggle chooses which screen axis is the tree's MAIN (depth) axis:
   "LR" (horizontal, depth→x, left→right) or "TB" (vertical, depth→y,
   top→bottom). The cross axis is packed by a subtree-contiguous tidy layout
   (#4622) so each branch reads as one cluster.
   ============================================================ */

import type { Edge, Node } from "@xyflow/react";
import { Position } from "@xyflow/react";
import type {
  DownstreamDAGEdge,
  DownstreamDAGEdgeKind,
  DownstreamDAGNode,
} from "@/data/types";
import { nodeModule, type NodeModule } from "./style";

/** Orientation toggle → which screen axis is the tree's main (depth) axis. */
export type FlowDagDirection = "LR" | "TB";

/** Data carried on each React Flow node, consumed by the custom node renderer. */
export interface FlowDagNodeData extends Record<string, unknown> {
  /** The original (deduped) payload node — what the detail panel reads. */
  node: DownstreamDAGNode;
  /**
   * The edge kind by which the parent reached this instance (the node's single
   * incoming relationship in the unfolded tree) — calls/handler/joins/throws/
   * validates. Undefined for the root (no in-edge). Surfaced on the card (#4481).
   */
  edgeKind?: DownstreamDAGEdgeKind;
  /** Whether this instance's collapsed_children are currently expanded inline. */
  expanded: boolean;
  /** Toggle handler for the inline collapsed-children expander (keyed by instance id). */
  onToggleExpand: (instanceId: string) => void;
  /**
   * #4561: whether this rendered instance is a GENUINE terminal — a real leaf /
   * return (the node had no out-edges in the source DAG, or the backend marked
   * it terminal). Such instances get the 'Return / finish' end-cap.
   */
  isLeaf?: boolean;
  /**
   * #4561: whether this instance has rendered children CUT by the depth/node
   * cap — i.e. the source node had out-edges but none were expanded here. These
   * keep their normal bucket + a 'more downstream' affordance, and must NOT be
   * confused with a genuine terminal.
   */
  truncatedHere?: boolean;
  /** #4557: the node's module (file-path-derived) for the grouping band. */
  module?: NodeModule;
  /** Whether this node is the caller's selected node (Flows inspector, #4354). */
  selected?: boolean;
  /**
   * Highlight state for the click-to-highlight route (#4479):
   *  - undefined → no highlight active (normal view)
   *  - true      → this instance is on the highlighted route
   *  - false     → an active route exists but this instance is off it (dimmed)
   */
  onRoute?: boolean;
}

/** Data carried on each React Flow edge, consumed by the custom edge renderer. */
export interface FlowDagEdgeData extends Record<string, unknown> {
  kind: DownstreamDAGEdge["kind"];
  /** Highlight state, mirroring FlowDagNodeData.onRoute (#4479). */
  onRoute?: boolean;
}

export type FlowDagNode = Node<FlowDagNodeData>;
export type FlowDagEdge = Edge<FlowDagEdgeData>;

/** One unfolded tree instance. The id is path-keyed; `node` is the original. */
export interface TreeInstance {
  /** Path-keyed instance id: "<parentInstanceId>/<nodeId>" (root = nodeId). */
  id: string;
  /** Parent instance id, or null for the root. Exactly one in a tree. */
  parentId: string | null;
  /** The original payload node this instance renders. */
  node: DownstreamDAGNode;
  /** The edge kind by which the parent reached this instance (root: undefined). */
  edgeKind?: DownstreamDAGEdgeKind;
}

/** Result of the unfold, including whether the node cap clipped the tree. */
export interface UnfoldResult {
  instances: TreeInstance[];
  /** True when expansion stopped because the max-node cap was hit. */
  capped: boolean;
  /**
   * Source-node ids that have at least one out-edge in the DAG (#4561). An
   * instance that emitted NO children but whose source id is in this set was
   * truncated (depth/cap/cycle-guard), not a genuine leaf. Used by layoutTree
   * to distinguish a real terminal from a cut branch.
   */
  hasOutEdge: Set<string>;
}

// Node box sizing fed to the tidy-tree pack. Kept generous so labels + repo
// chip fit; the custom node renderer uses min-width/height matching these so
// edges dock cleanly. Expanded nodes grow downward in the DOM but we keep the
// layout box fixed — the inline rows overlay rather than reflow the graph.
// Widened for #4481: long names now wrap to ~2 lines and the card surfaces
// signature / file:line / effect badges, so the box is roomier. Height is the
// layout rank box (edges dock to it); the card itself grows downward in the DOM
// for the inline expander/extra rows without reflowing the graph.
const NODE_W = 268;
const NODE_H = 76;

const NODE_TYPE = "flowDag";
const EDGE_TYPE = "flowDag";

/**
 * Hard ceiling on unfolded instances. A pure-tree unfold of a diamond-heavy DAG
 * can blow up combinatorially; this caps the rendered tree so the browser never
 * hangs. The depth control is the primary lever; this is the safety net.
 */
export const MAX_TREE_NODES = 600;

/**
 * Unfold the deduped DAG into a pure tree rooted at `rootId`.
 *
 * BFS from the root following out-edges; every reached node becomes a NEW
 * instance keyed by its path, so a node reached via N paths yields N instances
 * (no fan-in). A per-path visited set guards against cycles (a node already on
 * the path is not re-expanded). Expansion stops at `maxNodes`.
 *
 * @param rootId   payload root_id (the endpoint instance)
 * @param nodes    payload nodes (deduped by id)
 * @param edges    payload edges (directed; a convergence node has >1 same `to`)
 * @param maxNodes hard cap on emitted instances
 */
export function unfoldTree(
  rootId: string,
  nodes: DownstreamDAGNode[],
  edges: DownstreamDAGEdge[],
  maxNodes = MAX_TREE_NODES,
): UnfoldResult {
  const nodeById = new Map<string, DownstreamDAGNode>();
  for (const n of nodes) nodeById.set(n.id, n);

  // Adjacency: from → list of (to, kind), preserving payload order.
  const out = new Map<string, { to: string; kind: DownstreamDAGEdgeKind }[]>();
  for (const e of edges) {
    if (!nodeById.has(e.from) || !nodeById.has(e.to)) continue;
    const list = out.get(e.from);
    if (list) list.push({ to: e.to, kind: e.kind });
    else out.set(e.from, [{ to: e.to, kind: e.kind }]);
  }

  // Source ids that have ≥1 out-edge in the DAG. An instance of such a node
  // that emits no children was truncated, not a genuine leaf (#4561).
  const hasOutEdge = new Set<string>(out.keys());

  const root = nodeById.get(rootId) ?? nodes[0];
  if (!root) return { instances: [], capped: false, hasOutEdge };

  const instances: TreeInstance[] = [];
  let capped = false;

  // BFS queue carries the instance plus the set of original node ids on its
  // path, so a diamond duplicates but a cycle can't re-expand a node already
  // on its own path.
  interface Frame {
    instance: TreeInstance;
    visited: Set<string>;
  }

  const rootInstance: TreeInstance = {
    id: root.id,
    parentId: null,
    node: root,
  };
  const queue: Frame[] = [
    { instance: rootInstance, visited: new Set([root.id]) },
  ];
  instances.push(rootInstance);

  while (queue.length > 0) {
    const { instance, visited } = queue.shift()!;
    const children = out.get(instance.node.id);
    if (!children) continue;

    for (const child of children) {
      // Cycle guard: skip a node already on this path.
      if (visited.has(child.to)) continue;
      if (instances.length >= maxNodes) {
        capped = true;
        break;
      }
      const childNode = nodeById.get(child.to);
      if (!childNode) continue;

      const childInstance: TreeInstance = {
        id: `${instance.id}/${child.to}`,
        parentId: instance.id,
        node: childNode,
        edgeKind: child.kind,
      };
      instances.push(childInstance);
      queue.push({
        instance: childInstance,
        // Clone the path set so siblings don't share a visited set.
        visited: new Set(visited).add(child.to),
      });
    }
    if (capped) break;
  }

  return { instances, capped, hasOutEdge };
}

/* ============================================================
   Tidy-tree positioning (#4622).

   dagre's per-rank ordering minimizes edge crossings but does NOT keep a
   subtree's nodes in a contiguous cross-axis band: a deep child of branch A
   can be placed inside the cross-axis span of sibling branch B, so the branch
   reads as belonging to the wrong cluster. We replace dagre's coordinate
   assignment with a classic Reingold–Tilford-style tidy tree where, by
   construction, every subtree occupies a CONTIGUOUS cross-axis band and
   sibling subtrees are separated by a clear gap. The result: clicking a node
   highlights a branch that is one visually-contiguous cluster.

   Main axis  = tree depth (rank). depth d → main = MARGIN + d * (NODE_main + RANK_GAP).
   Cross axis = packed leaf order. Leaves are placed left→right at a fixed
                cross-step; each internal node is centered over its children.
                A larger gap is inserted BETWEEN sibling subtrees than between
                adjacent leaves of the same parent, so branches separate.
   ============================================================ */

/** Spacing constants for the tidy-tree packing (px, pre-orientation). */
const RANK_GAP_LR = 90; // gap between depth columns, horizontal
const RANK_GAP_TB = 70; // gap between depth rows, vertical
const SIBLING_LEAF_GAP_LR = 28; // cross gap between adjacent leaves (same parent), horizontal
const SIBLING_LEAF_GAP_TB = 48; // …vertical
// Extra cross-axis separation inserted between DISTINCT sibling subtrees, so a
// branch reads as one cluster with a clear lane between it and its neighbor.
const BRANCH_GAP_LR = 40;
const BRANCH_GAP_TB = 56;
const MARGIN = 16;

interface TidyNode {
  inst: TreeInstance;
  children: TidyNode[];
  depth: number;
  /** Cross-axis center (in cross px), assigned by the tidy pass. */
  cross: number;
}

/**
 * Lay the unfolded tree out as a tidy tree, returning the cross-axis center for
 * every instance id. Guarantees: a node's subtree spans a contiguous cross-axis
 * band, and two sibling subtrees' bands never overlap (separated by a branch
 * gap). Pure function of the tree shape + spacing → deterministic.
 */
function tidyTreeCross(
  instances: TreeInstance[],
  crossStep: number,
  branchGap: number,
): Map<string, number> {
  const byId = new Map<string, TidyNode>();
  const roots: TidyNode[] = [];
  for (const inst of instances) {
    byId.set(inst.id, { inst, children: [], depth: 0, cross: 0 });
  }
  for (const inst of instances) {
    const node = byId.get(inst.id)!;
    if (inst.parentId != null && byId.has(inst.parentId)) {
      const parent = byId.get(inst.parentId)!;
      node.depth = parent.depth + 1;
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  // Cursor in cross px; advances as we place leaves so each subtree gets its own
  // contiguous span. `place` returns the subtree's center.
  let cursor = 0;
  const cross = new Map<string, number>();

  function place(node: TidyNode): number {
    if (node.children.length === 0) {
      const c = cursor;
      cursor += crossStep;
      node.cross = c;
      cross.set(node.inst.id, c);
      return c;
    }
    const childCenters: number[] = [];
    node.children.forEach((child, i) => {
      if (i > 0) cursor += branchGap; // clear lane BETWEEN sibling subtrees
      childCenters.push(place(child));
    });
    // Center the parent over the span of its children → subtree stays contiguous
    // and a parent never drifts into a sibling branch's band.
    const c = (childCenters[0] + childCenters[childCenters.length - 1]) / 2;
    node.cross = c;
    cross.set(node.inst.id, c);
    return c;
  }

  for (const root of roots) {
    if (cross.size > 0) cursor += branchGap; // separate disjoint roots too
    place(root);
  }
  return cross;
}

/**
 * Build positioned React Flow nodes + edges from the unfolded tree.
 *
 * Positioning is a tidy-tree pack (#4622): depth maps to the main axis and a
 * subtree-contiguous tidy layout maps to the cross axis, so each branch is one
 * visually-contiguous cluster. `direction` chooses which screen axis is main:
 * "LR" (horizontal, depth→x) or "TB" (vertical, depth→y).
 *
 * @param instances  unfolded tree instances (from unfoldTree)
 * @param direction  "LR" (horizontal) | "TB" (vertical)
 * @param expanded   set of INSTANCE ids whose collapsed_children show inline
 * @param onToggle   inline-expand toggle handler (keyed by instance id)
 */
export function layoutTree(
  instances: TreeInstance[],
  direction: FlowDagDirection,
  expanded: Set<string>,
  onToggle: (instanceId: string) => void,
  hasOutEdge?: Set<string>,
): { nodes: FlowDagNode[]; edges: FlowDagEdge[] } {
  // Which instances actually emitted children in this unfold — to tell a real
  // leaf from a depth-truncated branch (#4561).
  const renderedParents = new Set<string>();
  for (const inst of instances) {
    if (inst.parentId != null) renderedParents.add(inst.parentId);
  }

  // Depth of each instance (along the main axis), derived from the parent chain.
  // instances are emitted parent-before-child (BFS), so a single forward pass
  // resolves depths.
  const depthById = new Map<string, number>();
  for (const inst of instances) {
    if (inst.parentId == null) {
      depthById.set(inst.id, 0);
    } else {
      depthById.set(inst.id, (depthById.get(inst.parentId) ?? 0) + 1);
    }
  }

  const isLR = direction === "LR";
  // Main-axis step (between depth columns/rows): node extent along main + gap.
  const mainNode = isLR ? NODE_W : NODE_H;
  const rankGap = isLR ? RANK_GAP_LR : RANK_GAP_TB;
  const mainStep = mainNode + rankGap;
  // Cross-axis step (between adjacent same-parent leaves): node cross extent + gap.
  const crossNode = isLR ? NODE_H : NODE_W;
  const leafGap = isLR ? SIBLING_LEAF_GAP_LR : SIBLING_LEAF_GAP_TB;
  const crossStep = crossNode + leafGap;
  const branchGap = isLR ? BRANCH_GAP_LR : BRANCH_GAP_TB;

  const crossById = tidyTreeCross(instances, crossStep, branchGap);

  const sourcePos = isLR ? Position.Right : Position.Bottom;
  const targetPos = isLR ? Position.Left : Position.Top;

  const rfNodes: FlowDagNode[] = instances.map((inst) => {
    const depth = depthById.get(inst.id) ?? 0;
    const crossCenter = crossById.get(inst.id) ?? 0;
    // Center coordinates along each axis, then convert to top-left for RF.
    const mainCenter = MARGIN + depth * mainStep + mainNode / 2;
    const x = isLR ? mainCenter : MARGIN + crossCenter + crossNode / 2;
    const y = isLR ? MARGIN + crossCenter + crossNode / 2 : mainCenter;
    const pos = { x, y };
    // #4561: this instance emitted no children here.
    const childlessHere = !renderedParents.has(inst.id);
    // The source node has downstream edges in the DAG.
    const sourceHasChildren = hasOutEdge?.has(inst.node.id) ?? false;
    // A genuine terminal: the backend marked it terminal, OR it's childless AND
    // the source node truly has no out-edges (a real leaf / return).
    const isLeaf = childlessHere && (inst.node.terminal === true || !sourceHasChildren);
    // Cut by the depth/node cap: childless HERE but the source DID have children.
    const truncatedHere = childlessHere && sourceHasChildren && inst.node.terminal !== true;
    return {
      id: inst.id,
      type: NODE_TYPE,
      // pos is the node center; React Flow positions by top-left corner.
      position: { x: pos.x - NODE_W / 2, y: pos.y - NODE_H / 2 },
      data: {
        node: inst.node,
        edgeKind: inst.edgeKind,
        expanded: expanded.has(inst.id),
        onToggleExpand: onToggle,
        isLeaf,
        truncatedHere,
        module: nodeModule(inst.node),
      },
      sourcePosition: sourcePos,
      targetPosition: targetPos,
      width: NODE_W,
      height: NODE_H,
    };
  });

  const rfEdges: FlowDagEdge[] = instances
    .filter((inst) => inst.parentId != null)
    .map((inst) => ({
      // One edge per non-root instance (the tree's single in-edge). The
      // instance id is unique, so the edge id is too.
      id: `e__${inst.id}`,
      source: inst.parentId!,
      target: inst.id,
      type: EDGE_TYPE,
      data: { kind: inst.edgeKind ?? "CALLS" },
    }));

  return { nodes: rfNodes, edges: rfEdges };
}

export const FLOW_DAG_NODE_TYPE = NODE_TYPE;
export const FLOW_DAG_EDGE_TYPE = EDGE_TYPE;
