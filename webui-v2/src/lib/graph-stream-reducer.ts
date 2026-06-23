/* ============================================================
   lib/graph-stream-reducer.ts — pure accumulation seam for the
   progressive graph stream (increment 2 of epic #5446).

   The SSE consumer (hooks/use-graph-stream) is a thin transport
   wrapper; ALL accumulation logic lives here as a pure reducer so it
   is unit-testable without a live EventSource:

     meta  → seed totals + legend metadata, start an empty payload
     chunk → append normalized nodes/edges to the growing arrays
     done  → mark finished

   The accumulated `payload` is the SAME normalized GraphPayload shape
   the full-payload fetch (hooks/use-graph → normalize) produces, so the
   Graph screen + cosmos canvas consume it with no data-model change —
   the stream is a drop-in source.
   ============================================================ */

import type {
  GraphPayload,
  GraphNodeWire,
  GraphEdgeWire,
  GraphCommunityWire,
  GraphRepoWire,
} from "@/data/types";

/** `meta` event payload (snake_case wire) — totals + legend metadata. */
export interface GraphStreamMetaWire {
  total_nodes: number;
  total_edges: number;
  communities: GraphCommunityWire[];
  repos: GraphRepoWire[];
}

/** `chunk` event payload (snake_case wire) — a batch of nodes + deliverable edges. */
export interface GraphStreamChunkWire {
  nodes: GraphNodeWire[];
  edges: GraphEdgeWire[];
}

/**
 * The accumulating stream state. `payload` is always a valid (possibly
 * partial) GraphPayload the canvas can render as-is; the counters drive the
 * progress affordance.
 */
export interface GraphStreamState {
  /** Normalized, growing graph payload (camelCase domain shape). */
  payload: GraphPayload;
  /** Totals from `meta` (progress denominator). 0 until meta arrives. */
  totalNodes: number;
  totalEdges: number;
  /** True once `meta` has been applied (payload is initialized + has legend). */
  hasMeta: boolean;
  /** True once `done` has been applied (stream finished cleanly). */
  done: boolean;
}

/** The empty initial state — before `meta`, an empty renderable payload. */
export function initialStreamState(): GraphStreamState {
  return {
    payload: { nodes: [], edges: [], communities: [], repos: [], totalNodeCount: 0 },
    totalNodes: 0,
    totalEdges: 0,
    hasMeta: false,
    done: false,
  };
}

/** Normalize a wire node into the camelCase domain shape (mirrors use-graph). */
function normalizeNode(n: GraphNodeWire) {
  return {
    id: n.id,
    label: n.label || n.id,
    kind: n.kind,
    repo: n.repo,
    degree: n.degree,
    pageRank: n.pagerank,
    communityId: n.community_id ?? null,
    sourceFile: n.source_file ?? "",
  };
}

/** Apply a `meta` event: seed totals + communities/repos, reset the payload. */
export function applyMeta(
  _prev: GraphStreamState,
  meta: GraphStreamMetaWire,
): GraphStreamState {
  return {
    payload: {
      nodes: [],
      edges: [],
      communities: (meta.communities ?? []).map((c) => ({
        id: c.id,
        label: c.label,
        repo: c.repo,
        size: c.size,
        colorIndex: c.color_index,
      })),
      repos: (meta.repos ?? []).map((r) => ({
        id: r.id,
        language: r.language,
        colorIndex: r.color_index,
      })),
      // The Graph screen's LOD badge reads totalNodeCount as the denominator;
      // the stream knows the true total up front from `meta`.
      totalNodeCount: meta.total_nodes,
    },
    totalNodes: meta.total_nodes,
    totalEdges: meta.total_edges,
    hasMeta: true,
    done: false,
  };
}

/**
 * Apply a `chunk` event: append the batch's nodes/edges to the growing arrays.
 *
 * New arrays are returned (immutable update) so React/consumers see a changed
 * reference and re-render. The backend guarantees an edge only appears once both
 * endpoints have streamed, so the appended edges never dangle.
 */
export function applyChunk(
  prev: GraphStreamState,
  chunk: GraphStreamChunkWire,
): GraphStreamState {
  const nodes = chunk.nodes?.length
    ? prev.payload.nodes.concat(chunk.nodes.map(normalizeNode))
    : prev.payload.nodes;
  const edges = chunk.edges?.length
    ? prev.payload.edges.concat(
        chunk.edges.map((e: GraphEdgeWire) => ({
          source: e.source,
          target: e.target,
          kind: e.kind,
        })),
      )
    : prev.payload.edges;
  return {
    ...prev,
    payload: { ...prev.payload, nodes, edges },
  };
}

/** Apply the `done` event: mark the stream finished. */
export function applyDone(prev: GraphStreamState): GraphStreamState {
  return { ...prev, done: true };
}
