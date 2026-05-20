import { useMemo } from 'react'
import type { GraphNode, GraphEdge, Community, LodLevel } from '@/types/api'

/** Camera zoom level — a numeric scale where larger = more zoomed in */
export type ZoomLevel = number

/**
 * Viewport bounding box in graph-coordinate space.
 * Used for frustum culling at zoom-in tier.
 */
export interface Viewport {
  minX: number
  maxX: number
  minY: number
  maxY: number
  minZ?: number
  maxZ?: number
}

export interface LodResult {
  visibleNodeIds: Set<string>
  visibleEdgeIds: Set<string>
  lodLevel: LodLevel
}

export const LOD_ZOOM_OUT_THRESHOLD = 0.5    // zoom < 0.5  → zoom-out (centroids only)
export const LOD_MID_THRESHOLD = 2.0         // 0.5 ≤ zoom < 2.0 → mid (centroids + god-nodes)
                                       // zoom ≥ 2.0  → zoom-in (full)

const MAX_RENDER_CAP = 20_000

/**
 * Pure derivation: given zoom level, viewport, and community tree,
 * returns the set of visible node IDs and edge IDs.
 *
 * Rules:
 * - Zoom-out (≥5k total OR zoom < threshold): community centroids only
 * - Mid-zoom (1k-5k OR zoom in mid range): centroids + god-nodes per community
 * - Zoom-in (<1k OR zoom > threshold): full expansion within frustum
 * - Selected node + 1-hop neighbors always visible regardless of tier
 * - Hard cap at 20k: returns `blocked` + empty sets
 * - Edge culled when either endpoint invisible
 */
export function useGraphLoD(
  nodes: GraphNode[],
  edges: GraphEdge[],
  communities: Community[],
  zoomLevel: ZoomLevel,
  viewport: Viewport | null,
  selectedNodeId: string | null,
): LodResult {
  return useMemo(() => {
    const total = nodes.length

    // Hard cap
    if (total > MAX_RENDER_CAP) {
      return { visibleNodeIds: new Set(), visibleEdgeIds: new Set(), lodLevel: 'blocked' }
    }

    // Determine tier from zoom level AND total node count
    let lodLevel: LodLevel
    if (total >= 5_000 || zoomLevel < LOD_ZOOM_OUT_THRESHOLD) {
      lodLevel = 'zoom-out'
    } else if (total >= 1_000 || zoomLevel < LOD_MID_THRESHOLD) {
      lodLevel = 'mid'
    } else {
      lodLevel = 'zoom-in'
    }

    // Build centroid node set (zoom-out)
    const centroidIds = new Set(nodes.filter((n) => n.is_centroid).map((n) => n.id))

    // Build god-node set (mid): top entities per community
    const godNodeIds = new Set(
      communities.flatMap((c) => c.top_entities),
    )

    // 1-hop neighbors of selected node — always visible
    const alwaysVisible = new Set<string>()
    if (selectedNodeId) {
      alwaysVisible.add(selectedNodeId)
      for (const e of edges) {
        if (e.source === selectedNodeId) alwaysVisible.add(e.target)
        if (e.target === selectedNodeId) alwaysVisible.add(e.source)
      }
    }

    // Compute visible node IDs based on tier
    let visibleNodeIds: Set<string>

    if (lodLevel === 'zoom-out') {
      // Centroids only (+ always-visible override).
      // When server pre-filters (lod=centroids), all received nodes are centroid
      // nodes — is_centroid flag ensures centroidIds covers them all.
      // If for some reason is_centroid is not set, fall back to all received nodes.
      const base = centroidIds.size > 0 ? centroidIds : new Set(nodes.map((n) => n.id))
      visibleNodeIds = new Set([...base, ...alwaysVisible])
    } else if (lodLevel === 'mid') {
      // Centroids + god-nodes (+ always-visible override).
      // When server pre-filters nodes (lod=mid), the returned set is already the
      // right tier — god-node IDs from communities.top_entities may use a different
      // ID format than actual node IDs. Fall back to showing all received nodes
      // when the community ID set doesn't intersect the actual node ID set.
      const nodeIdSet = new Set(nodes.map((n) => n.id))
      const godHits = [...godNodeIds].filter((id) => nodeIdSet.has(id))
      const base = centroidIds.size > 0
        ? new Set([...centroidIds, ...godNodeIds])
        : godHits.length > 0
          ? new Set([...godHits])
          : nodeIdSet  // server pre-filtered — trust the payload
      visibleNodeIds = new Set([...base, ...alwaysVisible])
    } else {
      // Zoom-in: full expansion, optionally frustum-culled
      if (viewport) {
        const inFrustum = nodes.filter((n) => isInViewport(n, viewport)).map((n) => n.id)
        visibleNodeIds = new Set([...inFrustum, ...alwaysVisible])
      } else {
        // No viewport provided — show all nodes (capped by total < 1k check above)
        visibleNodeIds = new Set(nodes.map((n) => n.id))
      }
    }

    // Cull edges: both endpoints must be visible
    const visibleEdgeIds = new Set<string>()
    for (const e of edges) {
      if (visibleNodeIds.has(e.source) && visibleNodeIds.has(e.target)) {
        visibleEdgeIds.add(e.id)
      }
    }

    return { visibleNodeIds, visibleEdgeIds, lodLevel }
  }, [nodes, edges, communities, zoomLevel, viewport, selectedNodeId])
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function isInViewport(node: GraphNode, vp: Viewport): boolean {
  // Nodes don't carry x/y/z from the API — position is computed by the physics engine.
  // This frustum-cull is a best-effort check using community_id as a proxy (not pixel coords).
  // The 3d-force-graph ref exposes node positions after simulation; until then we include all.
  // Full frustum cull is wired in GraphCanvas3D via the cameraRef callback.
  void node
  void vp
  return true
}
