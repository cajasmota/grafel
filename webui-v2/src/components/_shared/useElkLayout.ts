/* ============================================================
   components/_shared/useElkLayout.ts — React hook wrapper around layoutWithElk.

   Foundation of the elkjs adoption epic (#4824/#4825). Diagrams call this hook
   with a builder that produces (a) the raw React-Flow nodes/edges and (b) the
   ELK layout inputs, then receive the laid-out nodes/edges once ELK resolves.

   The hook owns the async lifecycle: it re-runs layout when `deps` change,
   cancels stale runs (last-write-wins), and exposes a `laidOut` flag so the
   consumer can show a loading state on first paint. It is GENERIC — it knows
   nothing about zones/tiers/IaC; the consumer supplies an `apply` callback that
   maps ELK positions back onto its own node objects.
   ============================================================ */

import { useEffect, useRef, useState, type DependencyList } from "react";
import {
  layoutWithElk,
  type ElkLayoutNode,
  type ElkLayoutEdge,
  type ElkLayoutOptions,
  type ElkLayoutPosition,
  type ElkLayoutEdgeRoute,
} from "@/lib/elk-layout";

export interface UseElkLayoutInput<N, E> {
  /** Final React-Flow nodes to render (pre-layout positions are ignored). */
  nodes: N[];
  /** Final React-Flow edges to render. */
  edges: E[];
  /** Layout inputs derived from `nodes` (ids, parentId, sizes, lanes). */
  elkNodes: ElkLayoutNode[];
  /** Layout inputs (edges) — may be a folded/aggregated subset of `edges`. */
  elkEdges: ElkLayoutEdge[];
  /** ELK layout options (direction, laneOf, spacing, …). */
  options?: ElkLayoutOptions;
}

export interface UseElkLayoutResult<N, E> {
  /** Nodes with ELK positions applied (empty until first layout resolves). */
  nodes: N[];
  /** Edges, passed through unchanged. */
  edges: E[];
  /** False until the first ELK layout has resolved (drive a loading state). */
  laidOut: boolean;
}

/**
 * useElkLayout runs ELK asynchronously and returns positioned nodes.
 *
 * @param build      produces the render nodes/edges + ELK layout inputs.
 *                   Recomputed when `deps` change.
 * @param apply      maps an ELK position onto a render node (set position + size).
 * @param deps       dependency list that retriggers the build + layout.
 * @param applyEdge  optional: maps an ELK orthogonal route (bendPoints, absolute
 *                   flow coords) onto a render edge, so its component can follow
 *                   ELK's route instead of a bezier (#4843). Edges keyed by id.
 */
export function useElkLayout<N extends { id: string }, E extends { id: string }>(
  build: () => UseElkLayoutInput<N, E>,
  apply: (node: N, pos: ElkLayoutPosition | undefined) => N,
  deps: DependencyList,
  applyEdge?: (edge: E, route: ElkLayoutEdgeRoute | undefined) => E,
): UseElkLayoutResult<N, E> {
  const [state, setState] = useState<{ nodes: N[]; edges: E[]; laidOut: boolean }>({
    nodes: [],
    edges: [],
    laidOut: false,
  });
  const runId = useRef(0);

  useEffect(() => {
    const myRun = ++runId.current;
    const { nodes, edges, elkNodes, elkEdges, options } = build();

    if (nodes.length === 0) {
      setState({ nodes: [], edges, laidOut: true });
      return;
    }

    let cancelled = false;
    layoutWithElk(elkNodes, elkEdges, options)
      .then(({ nodes: positions, edges: routes }) => {
        if (cancelled || myRun !== runId.current) return;
        const placed = nodes.map((n) => apply(n, positions.get(n.id)));
        const routed = applyEdge
          ? edges.map((e) => applyEdge(e, routes.get(e.id)))
          : edges;
        setState({ nodes: placed, edges: routed, laidOut: true });
      })
      .catch(() => {
        if (cancelled || myRun !== runId.current) return;
        // On layout failure, fall back to the unpositioned nodes so the canvas
        // still mounts (better than a perpetual spinner).
        setState({ nodes, edges, laidOut: true });
      });

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { nodes: state.nodes, edges: state.edges, laidOut: state.laidOut };
}
