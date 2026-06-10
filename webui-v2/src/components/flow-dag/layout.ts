/* ============================================================
   components/flow-dag/layout.ts — pure-tree unfold + dagre layout for <FlowDag>.

   The daemon ships a deduped DAG (each node id once; a convergence node carries
   >1 in-edge). The renderer UNFOLDS that DAG into a pure TREE (#4479): starting
   at `root_id` we walk the edges and DUPLICATE any node reached via more than
   one path, so every rendered instance has exactly ONE incoming edge and may
   have many outgoing. The result lays out cleanly under dagre with no fan-in /
   crossing edges.

   Each instance is keyed by its path ("<parentInstanceId>/<nodeId>") and carries
   the ORIGINAL node's data (name/kind/role/file/… ) for the detail panel and the
   caller's selection contract. Safety: the unfold honors the depth control and a
   hard max-node cap, and tracks the path's visited set so a diamond — or a stray
   cycle, which shouldn't occur in a DAG — can never infinitely expand or hang the
   browser. When a cap fires we stop expanding and surface honest truncation.

   The H/V toggle maps directly onto dagre's `rankdir`: "LR" (horizontal,
   left→right) or "TB" (vertical, top→bottom).
   ============================================================ */

import dagre from "dagre";
import type { Edge, Node } from "@xyflow/react";
import { Position } from "@xyflow/react";
import type {
  DownstreamDAGEdge,
  DownstreamDAGEdgeKind,
  DownstreamDAGNode,
} from "@/data/types";

/** Orientation toggle → dagre rankdir. */
export type FlowDagDirection = "LR" | "TB";

/** Data carried on each React Flow node, consumed by the custom node renderer. */
export interface FlowDagNodeData extends Record<string, unknown> {
  /** The original (deduped) payload node — what the detail panel reads. */
  node: DownstreamDAGNode;
  /** Whether this instance's collapsed_children are currently expanded inline. */
  expanded: boolean;
  /** Toggle handler for the inline collapsed-children expander (keyed by instance id). */
  onToggleExpand: (instanceId: string) => void;
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
}

// Node box sizing fed to dagre. Kept generous so labels + repo chip fit; the
// custom node renderer uses min-width/height matching these so edges dock
// cleanly. Expanded nodes grow downward in the DOM but we keep the dagre box
// fixed — the inline rows overlay rather than reflow the graph.
const NODE_W = 220;
const NODE_H = 64;

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

  const root = nodeById.get(rootId) ?? nodes[0];
  if (!root) return { instances: [], capped: false };

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

  return { instances, capped };
}

/**
 * Build positioned React Flow nodes + edges from the unfolded tree.
 *
 * @param instances  unfolded tree instances (from unfoldTree)
 * @param direction  "LR" (horizontal) | "TB" (vertical) → dagre rankdir
 * @param expanded   set of INSTANCE ids whose collapsed_children show inline
 * @param onToggle   inline-expand toggle handler (keyed by instance id)
 */
export function layoutTree(
  instances: TreeInstance[],
  direction: FlowDagDirection,
  expanded: Set<string>,
  onToggle: (instanceId: string) => void,
): { nodes: FlowDagNode[]; edges: FlowDagEdge[] } {
  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: direction,
    // Roomy spacing so branchy trees don't overlap; tighter on the cross axis.
    nodesep: direction === "LR" ? 28 : 48,
    ranksep: direction === "LR" ? 90 : 70,
    marginx: 16,
    marginy: 16,
  });
  g.setDefaultEdgeLabel(() => ({}));

  for (const inst of instances) {
    g.setNode(inst.id, { width: NODE_W, height: NODE_H });
  }
  for (const inst of instances) {
    if (inst.parentId != null) g.setEdge(inst.parentId, inst.id);
  }

  dagre.layout(g);

  const sourcePos = direction === "LR" ? Position.Right : Position.Bottom;
  const targetPos = direction === "LR" ? Position.Left : Position.Top;

  const rfNodes: FlowDagNode[] = instances.map((inst) => {
    const pos = g.node(inst.id);
    return {
      id: inst.id,
      type: NODE_TYPE,
      // dagre centers nodes; React Flow positions by top-left corner.
      position: { x: (pos?.x ?? 0) - NODE_W / 2, y: (pos?.y ?? 0) - NODE_H / 2 },
      data: {
        node: inst.node,
        expanded: expanded.has(inst.id),
        onToggleExpand: onToggle,
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
