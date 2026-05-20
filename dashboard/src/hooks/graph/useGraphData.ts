import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { fetchGraph } from '@/api/client'
import { useGraphLoD, LOD_ZOOM_OUT_THRESHOLD, LOD_MID_THRESHOLD } from './useGraphLoD'
import type { GraphFilters, GraphNode, GraphEdge, Community, LodLevel, ServerLodLevel } from '@/types/api'
import type { ZoomLevel, Viewport } from './useGraphLoD'

// ── LOD tier selection ────────────────────────────────────────────────────────
// Map the camera zoom level to a server-side LOD tier.  The server caps the
// "full" tier at 20 000 nodes; for large groups we must start at "centroids"
// and step up as the user zooms in.
//
// Thresholds (issue #1000):
//   zoom < 0.05  → centroids  (community blobs, ~50-200 nodes) — only when very far out
//   zoom < 0.15  → mid        (top-50 god nodes, ~150 nodes)
//   zoom < 3.0   → dense      (top-500/repo — NEW default, ~500-1500 nodes)
//   zoom >= 3.0  → full       (all nodes up to 20k cap)
//
// The very low thresholds ensure that the default rendering (zoom ~0.05-1.0 for
// a fresh 3D graph) maps to 'dense' rather than 'centroids' or 'mid'.
// 3d-force-graph emits onZoom with k values that start at ~0.05-0.5 depending on
// graph size; keeping the 'dense' window wide prevents thrashing to sparse tiers.
const LOD_ZOOM_OUT_THRESHOLD = 0.05
const LOD_MID_THRESHOLD = 0.15
const LOD_DENSE_THRESHOLD = 3.0

function zoomToServerLod(zoom: ZoomLevel): ServerLodLevel {
  if (zoom < LOD_ZOOM_OUT_THRESHOLD) return 'centroids'
  if (zoom < LOD_MID_THRESHOLD) return 'mid'
  if (zoom < LOD_DENSE_THRESHOLD) return 'dense'
  return 'full'
}

// Synthetic edge kinds emitted by the server only for layout purposes.
// These should not appear as user-facing filter chips.
const SYNTHETIC_KINDS = new Set<string>(['COMMUNITY_LINK'])

export interface GraphDataResult {
  /** Filtered node array for the graph renderer */
  nodes: GraphNode[]
  /** Filtered edge array for the graph renderer */
  edges: GraphEdge[]
  /** All communities */
  communities: Community[]
  /** All unique edge kinds present in the full graph */
  allEdgeKinds: string[]
  /** Current LoD level */
  lodLevel: LodLevel
  /** Total node count (unfiltered, for cap display) */
  totalNodeCount: number
  isLoading: boolean
  error: Error | null
  refetch: () => void
}

/**
 * Fetches the graph for a group, then optionally applies client-side LoD
 * culling via useGraphLoD.
 *
 * When we send a server-side LOD param the server has already filtered the
 * node set to the appropriate tier; client-side re-filtering would remove
 * nodes that don't match the client's heuristic (e.g. centroid nodes have
 * is_centroid=true but the mid-zoom heuristic looks for god-node IDs in
 * community top_entities, which wouldn't match centroid IDs).  In server-LOD
 * mode we therefore show all returned nodes and skip the useGraphLoD filter.
 *
 * @param group - group ID
 * @param filters - edge-kind, repo, and repos[] filters
 * @param zoomLevel - current camera zoom (drives LoD tier)
 * @param viewport - frustum bounds for zoom-in culling (null = no cull)
 * @param selectedNodeId - always visible regardless of LoD
 * @param selectedCommunityId - community drill-in filter (client-side, #1000)
 * @param activeRepos - set of active repo slugs for multi-repo filter (#1000)
 */
export function useGraphData(
  group: string,
  filters: GraphFilters,
  zoomLevel: ZoomLevel,
  viewport: Viewport | null,
  selectedNodeId: string | null,
  selectedCommunityId?: number | null,
  activeRepos?: Set<string> | null,
): GraphDataResult {
  // Map zoom level to server-side LoD so the API pre-filters to a safe node count.
  // Server accepts: "centroids" | "mid" | "full". We never request "full" for large
  // graphs to avoid the 20k-node blocked response.
  const serverLod = zoomToServerLod(zoomLevel)

  // Build repos param for multi-repo filter (#1000)
  const reposParam = activeRepos && activeRepos.size > 0
    ? [...activeRepos].sort().join(',')
    : undefined

  const {
    data,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ['graph', group, filters.repo, reposParam, serverLod],
    queryFn: () => fetchGraph(group, { repo: filters.repo, lod: serverLod, repos: reposParam }),
    staleTime: 5 * 60 * 1000,
    enabled: !!group,
  })

  // Apply edge-kind filter client-side (cheap — just a Set lookup)
  const filteredEdges = useMemo(() => {
    if (!data) return []
    if (!filters.edge_kinds || filters.edge_kinds.length === 0) return data.edges
    const kinds = new Set(filters.edge_kinds)
    return data.edges.filter((e) => kinds.has(e.kind))
  }, [data, filters.edge_kinds])

  // The server already performed LOD selection — trust it and show all
  // returned nodes rather than re-filtering client-side.  useGraphLoD is
  // still called to derive the LodLevel enum value (used for the LoD
  // indicator UI) and to apply selected-node 1-hop expansion, but we bypass
  // its visibility filter and show all server-returned nodes directly.
  const { lodLevel } = useGraphLoD(
    data?.nodes ?? [],
    filteredEdges,
    data?.communities ?? [],
    zoomLevel,
    viewport,
    selectedNodeId,
  )

  // For the "blocked" sentinel the server returns 0 nodes; surface that as-is.
  const serverLodLevel: LodLevel = data?.lod ?? lodLevel

  // Community drill-in filter (#1000): when a community is selected,
  // show only nodes in that community plus their direct neighbors.
  const nodes = useMemo<GraphNode[]>(() => {
    if (!data) return []
    if (!selectedCommunityId && selectedCommunityId !== 0) return data.nodes

    // Find nodes in the selected community
    const communityNodes = new Set(
      data.nodes
        .filter((n) => n.community_id === selectedCommunityId)
        .map((n) => n.id),
    )

    // Find direct neighbors (1-hop) via edges
    const neighbors = new Set<string>()
    for (const e of filteredEdges) {
      if (communityNodes.has(e.source)) neighbors.add(e.target)
      if (communityNodes.has(e.target)) neighbors.add(e.source)
    }

    return data.nodes.filter(
      (n) => communityNodes.has(n.id) || neighbors.has(n.id),
    )
  }, [data, selectedCommunityId, filteredEdges])

  const edges = useMemo<GraphEdge[]>(() => {
    if (!data) return []
    if (!selectedCommunityId && selectedCommunityId !== 0) return filteredEdges

    const nodeIds = new Set(nodes.map((n) => n.id))
    return filteredEdges.filter(
      (e) => nodeIds.has(e.source) && nodeIds.has(e.target),
    )
  }, [data, filteredEdges, selectedCommunityId, nodes])

  // Exclude synthetic centroid-tier edges (COMMUNITY_LINK) from the filter
  // chip bar — they are internal layout hints and not meaningful to the user.
  const allEdgeKinds = useMemo(() => {
    if (!data) return []
    return [...new Set(data.edges.map((e) => e.kind))].filter(
      (k) => !SYNTHETIC_KINDS.has(k),
    )
  }, [data])

  return {
    nodes,
    edges,
    communities: data?.communities ?? [],
    allEdgeKinds,
    lodLevel: serverLodLevel,
    totalNodeCount: data?.total_node_count ?? 0,
    isLoading,
    error: error as Error | null,
    refetch,
  }
}
