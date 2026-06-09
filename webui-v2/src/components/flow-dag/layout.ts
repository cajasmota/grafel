/* ============================================================
   components/flow-dag/layout.ts — dagre layout for <FlowDag>.

   Maps the daemon's downstream-DAG payload onto React Flow node/edge
   objects and runs dagre to assign positions. The H/V toggle maps directly
   onto dagre's `rankdir`: "LR" (horizontal, left→right) or "TB" (vertical,
   top→bottom).

   The payload is already a deduped DAG (each node id appears once; a
   convergence node carries >1 in-edge), so layout simply keys on node id and
   lets dagre route every incoming edge — no client-side dedupe needed.
   ============================================================ */

import dagre from "dagre";
import type { Edge, Node } from "@xyflow/react";
import { Position } from "@xyflow/react";
import type {
  DownstreamDAGEdge,
  DownstreamDAGNode,
} from "@/data/types";

/** Orientation toggle → dagre rankdir. */
export type FlowDagDirection = "LR" | "TB";

/** Data carried on each React Flow node, consumed by the custom node renderer. */
export interface FlowDagNodeData extends Record<string, unknown> {
  node: DownstreamDAGNode;
  /** Whether this node's collapsed_children are currently expanded inline. */
  expanded: boolean;
  /** Toggle handler for the inline collapsed-children expander. */
  onToggleExpand: (id: string) => void;
  /** Whether this node is the caller's selected node (Flows inspector, #4354). */
  selected?: boolean;
}

/** Data carried on each React Flow edge, consumed by the custom edge renderer. */
export interface FlowDagEdgeData extends Record<string, unknown> {
  kind: DownstreamDAGEdge["kind"];
}

export type FlowDagNode = Node<FlowDagNodeData>;
export type FlowDagEdge = Edge<FlowDagEdgeData>;

// Node box sizing fed to dagre. Kept generous so labels + repo chip fit; the
// custom node renderer uses min-width/height matching these so edges dock
// cleanly. Expanded nodes grow downward in the DOM but we keep the dagre box
// fixed — the inline rows overlay rather than reflow the graph.
const NODE_W = 220;
const NODE_H = 64;

const NODE_TYPE = "flowDag";
const EDGE_TYPE = "flowDag";

/**
 * Build positioned React Flow nodes + edges from the DAG payload.
 *
 * @param nodes      payload nodes (already deduped by id)
 * @param edges      payload edges (a convergence node has >1 with same `to`)
 * @param direction  "LR" (horizontal) | "TB" (vertical) → dagre rankdir
 * @param expanded   set of node ids whose collapsed_children are shown inline
 * @param onToggle   inline-expand toggle handler
 */
export function layoutDAG(
  nodes: DownstreamDAGNode[],
  edges: DownstreamDAGEdge[],
  direction: FlowDagDirection,
  expanded: Set<string>,
  onToggle: (id: string) => void,
): { nodes: FlowDagNode[]; edges: FlowDagEdge[] } {
  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: direction,
    // Roomy spacing so branchy DAGs don't overlap; tighter on the cross axis.
    nodesep: direction === "LR" ? 28 : 48,
    ranksep: direction === "LR" ? 90 : 70,
    marginx: 16,
    marginy: 16,
  });
  g.setDefaultEdgeLabel(() => ({}));

  for (const n of nodes) {
    g.setNode(n.id, { width: NODE_W, height: NODE_H });
  }
  for (const e of edges) {
    // dagre tolerates parallel edges between the same pair; convergence (>1
    // edge into the same `to`) is preserved because we never dedupe here.
    if (g.hasNode(e.from) && g.hasNode(e.to)) {
      g.setEdge(e.from, e.to);
    }
  }

  dagre.layout(g);

  const sourcePos = direction === "LR" ? Position.Right : Position.Bottom;
  const targetPos = direction === "LR" ? Position.Left : Position.Top;

  const rfNodes: FlowDagNode[] = nodes.map((n) => {
    const pos = g.node(n.id);
    return {
      id: n.id,
      type: NODE_TYPE,
      // dagre centers nodes; React Flow positions by top-left corner.
      position: { x: (pos?.x ?? 0) - NODE_W / 2, y: (pos?.y ?? 0) - NODE_H / 2 },
      data: { node: n, expanded: expanded.has(n.id), onToggleExpand: onToggle },
      sourcePosition: sourcePos,
      targetPosition: targetPos,
      width: NODE_W,
      height: NODE_H,
    };
  });

  const rfEdges: FlowDagEdge[] = edges.map((e, i) => ({
    // Edge id must be unique even for convergence (same to, different from) and
    // for parallel kinds between the same pair — index-suffix guarantees it.
    id: `${e.from}__${e.to}__${e.kind}__${i}`,
    source: e.from,
    target: e.to,
    type: EDGE_TYPE,
    data: { kind: e.kind },
  }));

  return { nodes: rfNodes, edges: rfEdges };
}

export const FLOW_DAG_NODE_TYPE = NODE_TYPE;
export const FLOW_DAG_EDGE_TYPE = EDGE_TYPE;
