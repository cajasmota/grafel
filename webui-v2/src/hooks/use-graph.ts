/* ============================================================
   hooks/use-graph.ts — Graph-screen data hooks.

   Wraps the typed api client in TanStack Query and normalizes the
   snake_case wire payload into the camelCase domain shapes the screen
   + cosmos canvas consume. Screens never call api.* directly.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type {
  GraphPayload,
  GraphPayloadWire,
  EntityDetailWire,
} from "@/data/types";

export const graphQueryKey = (groupId: string, repos?: string[], filterKind?: string) =>
  ["graph", groupId, repos?.slice().sort().join(",") ?? "", filterKind ?? ""] as const;

/** Normalize the wire payload (snake_case) into the domain shape. */
function normalize(w: GraphPayloadWire): GraphPayload {
  return {
    nodes: w.nodes.map((n) => ({
      id: n.id,
      label: n.label || n.id,
      kind: n.kind,
      repo: n.repo,
      degree: n.degree,
      pageRank: n.pagerank,
      communityId: n.community_id ?? null,
      sourceFile: n.source_file ?? "",
    })),
    edges: w.edges.map((e) => ({ source: e.source, target: e.target, kind: e.kind })),
    communities: w.communities.map((c) => ({
      id: c.id,
      label: c.label,
      repo: c.repo,
      size: c.size,
      colorIndex: c.color_index,
    })),
    repos: w.repos.map((r) => ({ id: r.id, language: r.language, colorIndex: r.color_index })),
    totalNodeCount: w.total_node_count,
  };
}

/**
 * The full graph payload. The cosmos canvas renders the FULL corpus (it caps
 * nothing); the repo / kind filters are passed to the daemon so large graphs
 * can be sliced server-side (network-payload paginating per graph.md).
 */
export function useGraph(
  groupId: string,
  opts?: { repos?: string[]; filterKind?: string },
) {
  return useQuery({
    queryKey: graphQueryKey(groupId, opts?.repos, opts?.filterKind),
    queryFn: async () => normalize(await api.getGraph(groupId, opts)),
    // The payload is large + server-cached (ETag/304); keep it warm.
    staleTime: 5 * 60 * 1000,
  });
}

/** Lazy Tier-3 entity detail for the inspector (fetched on node click). */
export function useEntityDetail(groupId: string, entityId: string | null) {
  return useQuery<EntityDetailWire>({
    queryKey: ["entity", groupId, entityId],
    queryFn: () => api.getEntityDetail(groupId, entityId as string),
    enabled: !!entityId,
    staleTime: 60 * 1000,
  });
}
