import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { fetchGraph } from '@/api/client'
import type { GraphFilters, GraphNode, GraphEdge, Community } from '@/types/api'

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
  /** Total node count */
  totalNodeCount: number
  isLoading: boolean
  error: Error | null
  refetch: () => void
}

/**
 * Fetches the graph for a group and applies client-side filters.
 *
 * #1023: LoD architecture removed. The server now always returns the dense tier
 * (top-500 per repo by PageRank). No zoom-level-based tier switching, no centroid
 * nodes, no COMMUNITY_LINK synthetic edges, no LodLevelIndicator.
 *
 * #1069: repo filter is now client-side only — the full graph is always fetched
 * and Cosmograph selection-greyout is used to hide filtered-out nodes without
 * re-running the force layout. `activeRepos` is intentionally NOT included in the
 * queryKey so toggling a repo chip never triggers a network request.
 *
 * @param group - group ID
 * @param filters - edge-kind and repo filters
 * @param _zoomLevel - kept for call-site compat; no longer drives server LoD
 * @param _viewport - kept for call-site compat; no longer used
 * @param selectedNodeId - kept for call-site compat; Cosmograph renders all nodes
 * @param selectedCommunityId - community drill-in filter (client-side)
 * @param _activeRepos - kept for call-site compat; filtering is now handled in GraphCanvas
 * @param includeExternal - when true, External stdlib/builtin nodes are included
 */
export function useGraphData(
  group: string,
  filters: GraphFilters,
  _zoomLevel: number,
  _viewport: null,
  selectedNodeId: string | null,
  selectedCommunityId?: number | null,
  _activeRepos?: Set<string> | null,
  includeExternal?: boolean,
): GraphDataResult {
  void selectedNodeId  // kept in signature for call-site compat
  void _activeRepos    // kept in signature for call-site compat; not used in query

  const {
    data,
    isLoading,
    error,
    refetch,
  } = useQuery({
    // #1069: repos intentionally excluded — repo filter is client-side, no refetch
    queryKey: ['graph', group, filters.repo, includeExternal],
    queryFn: () => fetchGraph(group, { repo: filters.repo, include_external: includeExternal }),
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

  // Community drill-in filter: when a community is selected,
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

  // Exclude synthetic centroid-tier edges from the filter chip bar.
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
    totalNodeCount: data?.total_node_count ?? 0,
    isLoading,
    error: error as Error | null,
    refetch,
  }
}
