/* ============================================================
   lib/flow-to-dag.ts — map a process Flow onto the shared <FlowDag> payload.

   The Flows endpoint does NOT emit the DownstreamDAGResponse shape the shared
   React Flow renderer consumes (that comes from the v2 paths
   /downstream-dag endpoint, #4349). What it DOES emit per process is:

     - `steps: ProcessStep[]`   — the (linearised) chain, one entry per node,
                                  carrying step_index / entity_id / name / kind /
                                  repo / step_kind / edge_kind.
     - `branches_dag?: string`  — a JSON-serialised ChainStep tree (#2028) that
                                  carries the REAL branching structure. The old
                                  SVG renderer flattened each flow to its primary
                                  path and could not draw these arms; the rebuild
                                  (#4354) maps them into the payload so the actual
                                  DAG renders.

   This module adapts (steps + branches_dag) → DownstreamDAGResponse so the
   selected flow drives <FlowDag payload={…}> directly — no second fetch, no
   fork of the layout/node/edge logic.
   ============================================================ */

import type {
  ChainStep,
  DownstreamDAGEdge,
  DownstreamDAGEdgeKind,
  DownstreamDAGNode,
  DownstreamDAGResponse,
  DownstreamDAGRole,
  EntryKind,
  Process,
  ProcessStep,
  StepKind,
} from "@/data/types";

/** Stable, unique React Flow node id for a step. step_index is unique per flow
 *  (entity_id can recur when a flow revisits the same callee), so we key on it. */
function stepNodeId(stepIndex: number): string {
  return `flow-step-${stepIndex}`;
}

/** Best human label for a step — the detail endpoint serves `label`, older
 *  builds `name`; fall back to the entity id so a node is never blank. */
function stepLabel(s: ProcessStep): string {
  return s.name ?? s.label ?? s.entity_id;
}

/** Data-sink step kinds — these read as terminal collection nodes (DB tables,
 *  topics, external calls) the same way the downstream-DAG marks collections. */
const SINK_STEP_KINDS: ReadonlySet<StepKind> = new Set<StepKind>([
  "db_query",
  "db_write",
  "message_publish",
  "message_consume",
  "http_fetch",
  "external_lib",
]);

/**
 * Map a step onto a DAG role so the shared node styling lights up:
 *   - the entry step → "endpoint" when it's an HTTP handler (the request
 *     boundary), else "handler" so non-HTTP entries still read as the root.
 *   - data-sink steps → "collection".
 *   - everything else → "node" (the generic spine).
 */
function stepRole(
  s: ProcessStep,
  isEntry: boolean,
  entryKind: EntryKind,
): DownstreamDAGRole {
  if (isEntry) return entryKind === "http_handler" ? "endpoint" : "handler";
  if (s.step_kind && SINK_STEP_KINDS.has(s.step_kind)) return "collection";
  return "node";
}

/**
 * Map a step's incoming FlowRelationshipKind onto a DAG edge kind so the edge
 * styling/legend stays meaningful. The downstream-DAG vocabulary is a closed
 * set; the flow chain is overwhelmingly CALLS, with a few semantic hops:
 *   QUERIES / FETCHES / PUBLISHES_TO / SUBSCRIBES_TO → JOINS_COLLECTION
 *   (the dashed "joins a data store/topic" arm), everything else → CALLS.
 */
function edgeKindFor(step: ProcessStep | undefined): DownstreamDAGEdgeKind {
  switch (step?.edge_kind) {
    case "QUERIES":
    case "FETCHES":
    case "PUBLISHES_TO":
    case "SUBSCRIBES_TO":
      return "JOINS_COLLECTION";
    default:
      return "CALLS";
  }
}

/** Walk the ChainStep tree, calling `visit` on every node (DFS). */
function walkChain(node: ChainStep, visit: (n: ChainStep) => void): void {
  visit(node);
  for (const b of node.branches ?? []) walkChain(b, visit);
}

/**
 * Build the branch edges from the persisted ChainStep tree. Every parent→child
 * link becomes a directed edge keyed on step_index, including fan-out arms the
 * old SVG renderer dropped. "fanout_cap" overflow sentinels carry no real step,
 * so they're skipped (the legend's truncation still reflects the cap).
 *
 * Returns null when the tree references step indices the steps array doesn't
 * carry (defensive — callers fall back to the linear chain).
 */
function edgesFromBranchesDag(
  root: ChainStep,
  byIndex: Map<number, ProcessStep>,
): DownstreamDAGEdge[] | null {
  const edges: DownstreamDAGEdge[] = [];
  let ok = true;
  walkChain(root, (node) => {
    for (const child of node.branches ?? []) {
      if (child.reason === "fanout_cap") continue;
      const from = byIndex.get(node.step_index);
      const to = byIndex.get(child.step_index);
      if (!from || !to) {
        ok = false;
        continue;
      }
      edges.push({
        from: stepNodeId(node.step_index),
        to: stepNodeId(child.step_index),
        kind: edgeKindFor(to),
      });
    }
  });
  return ok ? edges : null;
}

/** Parse the JSON-serialised ChainStep tree; null on absent/garbage input. */
export function parseBranchesDag(raw?: string): ChainStep | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw);
    if (v && typeof v === "object" && typeof v.step_index === "number") {
      return v as ChainStep;
    }
  } catch {
    /* malformed — fall through to null so the caller uses the linear chain. */
  }
  return null;
}

/** Count internal fan-out points (kept steps with out-degree > 1). */
function countBranches(edges: DownstreamDAGEdge[]): number {
  const outDegree = new Map<string, number>();
  for (const e of edges) outDegree.set(e.from, (outDegree.get(e.from) ?? 0) + 1);
  let n = 0;
  for (const deg of outDegree.values()) if (deg > 1) n++;
  return n;
}

/**
 * Adapt a Process (with its resolved `steps`) onto the shared FlowDag payload.
 *
 * When `branches_dag` is present and consistent with the steps, the real
 * branching structure is rendered; otherwise the chain is laid out linearly
 * (step i → step i+1), matching the pre-rebuild behaviour for non-DAG flows.
 *
 * Returns null when the flow carries no steps (caller shows its empty state).
 */
export function flowToDagPayload(
  flow: Process,
  steps: ProcessStep[] | undefined,
): DownstreamDAGResponse | null {
  if (!steps || steps.length === 0) return null;

  // Sort by step_index so node[0] is the entry regardless of array order.
  const ordered = [...steps].sort((a, b) => a.step_index - b.step_index);
  const byIndex = new Map<number, ProcessStep>();
  for (const s of ordered) byIndex.set(s.step_index, s);

  const entryIndex = ordered[0].step_index;
  const terminalIndex = ordered[ordered.length - 1].step_index;

  const nodes: DownstreamDAGNode[] = ordered.map((s) => {
    const isEntry = s.step_index === entryIndex;
    return {
      id: stepNodeId(s.step_index),
      name: stepLabel(s),
      kind: s.kind ?? s.step_kind ?? "step",
      file: s.source_file,
      line: s.start_line,
      repo: s.repo,
      role: stepRole(s, isEntry, flow.entry_kind),
      // A leaf with no outgoing edge is a terminal sink; computed below once the
      // edge set is known, so seed false here and patch after.
      terminal: false,
    };
  });

  // Prefer the persisted DAG; fall back to a linear chain.
  const dagRoot = parseBranchesDag(flow.branches_dag);
  let edges: DownstreamDAGEdge[] | null = dagRoot
    ? edgesFromBranchesDag(dagRoot, byIndex)
    : null;

  if (!edges) {
    edges = [];
    for (let i = 1; i < ordered.length; i++) {
      edges.push({
        from: stepNodeId(ordered[i - 1].step_index),
        to: stepNodeId(ordered[i].step_index),
        kind: edgeKindFor(ordered[i]),
      });
    }
  }

  // Mark leaves (no outgoing edge) as terminal so collection sinks get the
  // doubled ring; always mark the last step so a strictly-linear chain still
  // reads as terminating.
  const hasOut = new Set(edges.map((e) => e.from));
  const nodeById = new Map(nodes.map((n) => [n.id, n]));
  for (const n of nodes) {
    if (!hasOut.has(n.id) || n.id === stepNodeId(terminalIndex)) {
      const node = nodeById.get(n.id);
      if (node) node.terminal = true;
    }
  }

  return {
    root_id: stepNodeId(entryIndex),
    path: flow.label,
    verb: flow.entry_kind,
    mode: "full",
    depth: ordered.length,
    nodes,
    edges,
    truncation: {
      // The flows endpoint applies its own fan-out cap upstream (surfaced as
      // "fanout_cap" sentinels in branches_dag); flag it so the legend is honest.
      depth_truncated: false,
      fanout_truncated: hadFanoutCap(dagRoot),
      node_truncated: false,
    },
    branch_count: countBranches(edges),
  };
}

/** True when the persisted DAG carried a fan-out overflow sentinel. */
function hadFanoutCap(root: ChainStep | null): boolean {
  if (!root) return false;
  let capped = false;
  walkChain(root, (n) => {
    if ((n.branches ?? []).some((b) => b.reason === "fanout_cap")) capped = true;
  });
  return capped;
}
