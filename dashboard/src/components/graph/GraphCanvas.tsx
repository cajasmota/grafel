import { useRef, useEffect, useCallback, memo } from 'react'
import { Cosmograph } from '@cosmograph/react'
import type { CosmographRef } from '@cosmograph/react'
import { edgeKindColor } from './EdgeBadge'
import { communityColor } from '@/hooks/graph/useCommunityColors'
import { repoColor } from '@/lib/colors'
import type { GraphNode, GraphEdge } from '@/types/api'
import { useGraphCameraStore } from '@/store/graphCameraStore'

export interface GraphCanvasProps {
  nodes: GraphNode[]
  edges: GraphEdge[]
  selectedNodeId: string | null
  hoveredNodeId: string | null
  onNodeClick: (node: GraphNode) => void
  onNodeHover: (node: GraphNode | null) => void
  onZoomChange?: (zoom: number) => void
  /** High-contrast mode — wider edges, higher opacity */
  highContrast?: boolean
  className?: string
}

/**
 * GPU-accelerated WebGL force-graph via Cosmograph.
 *
 * Replaces GraphCanvas3D + GraphCanvas2D (#1023).
 * - No LoD: receives pre-filtered nodes + edges from useGraphData
 * - Single canvas, 2D force simulation (60fps at 1M+ nodes)
 * - Drop-in prop interface — route component unchanged
 *
 * Cosmograph data model:
 *   points  = nodes array  (pointIdBy: 'id')
 *   links   = edges array  (linkSourceBy: 'source', linkTargetBy: 'target')
 * Both arrays must be stable references across renders to avoid full rebuilds;
 * we memoise them via useMemo in the calling component (useGraphData already does this).
 *
 * Click handling: Cosmograph provides the point _index_ in onClick.
 * We keep a ref mirror of `nodes` so we can do O(1) lookup without async API calls.
 */
const GraphCanvasInner = ({
  nodes,
  edges,
  selectedNodeId,
  hoveredNodeId,
  onNodeClick,
  onNodeHover,
  onZoomChange,
  highContrast = false,
  className = '',
}: GraphCanvasProps) => {
  const cosmographRef = useRef<CosmographRef>(undefined)
  const { setGraphRef, setZoomLevel } = useGraphCameraStore()

  // Mirror of nodes so click handler can resolve index → GraphNode synchronously
  const nodesRef = useRef<GraphNode[]>(nodes)
  nodesRef.current = nodes

  // Expose a cosmograph-compatible ref to the camera store so
  // resetView / zoomToNode keep working with the new renderer.
  const handleMount = useCallback((instance: NonNullable<CosmographRef>) => {
    cosmographRef.current = instance
    // Wrap the Cosmograph instance to match the camera store's ForceGraphInstance duck-type
    setGraphRef(instance as unknown as Parameters<typeof setGraphRef>[0])
  }, [setGraphRef])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      setGraphRef(null)
    }
  }, [setGraphRef])

  // Point color accessor — receives the value of the `pointColorBy` column ('id'),
  // so `value` here is the node id string.
  const pointColorByFn = useCallback((nodeId: string) => {
    if (nodeId === selectedNodeId) return '#38bdf8'   // sky-400 — selected
    if (nodeId === hoveredNodeId)  return '#e2e8f0'   // slate-200 — hovered
    // Find node to determine color strategy
    const node = nodesRef.current.find((n) => n.id === nodeId)
    if (!node) return '#64748b'
    if (node.is_centroid) return communityColor(node.community_id ?? 0)
    if (selectedNodeId) return repoColor(node.repo) + '66' // dimmed when another selected
    return repoColor(node.repo)
  }, [selectedNodeId, hoveredNodeId])

  // Point size accessor — receives the node id (same column as pointColorBy)
  const pointSizeByFn = useCallback((nodeId: string) => {
    const node = nodesRef.current.find((n) => n.id === nodeId)
    if (!node) return 4
    if (node.is_centroid) return Math.max(4, Math.min(20, (node.centroid_size ?? 100) / 25))
    if ((node.pagerank ?? 0) > 0.6) return 10  // god node
    return 4 + (node.pagerank ?? 0) * 12
  }, [])

  // Link color accessor — receives the value of the `linkColorBy` column ('kind')
  const linkColorByFn = useCallback((kind: string) => {
    const base = edgeKindColor(kind as GraphEdge['kind'])
    return highContrast ? base : base + '99'
  }, [highContrast])

  // Click: Cosmograph provides the point index in the current `nodes` array
  const handleClick = useCallback((index: number | undefined) => {
    if (index === undefined) return
    const node = nodesRef.current[index]
    if (node) onNodeClick(node)
  }, [onNodeClick])

  // Background click: clear hover
  const handleBackgroundClick = useCallback(() => {
    onNodeHover(null)
  }, [onNodeHover])

  // Hover: Cosmograph provides the point index on mouse move
  const handleMouseMove = useCallback((index: number | undefined) => {
    if (index === undefined) {
      onNodeHover(null)
      return
    }
    const node = nodesRef.current[index]
    onNodeHover(node ?? null)
  }, [onNodeHover])

  // Zoom: Cosmograph's onZoom fires with a D3 zoom event; extract the k scale
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const handleZoom = useCallback((e: any) => {
    const k: number = e?.transform?.k ?? 1
    setZoomLevel(k)
    onZoomChange?.(k)
  }, [setZoomLevel, onZoomChange])

  return (
    <div
      className={['w-full h-full', className].join(' ')}
      aria-label="Dependency graph"
      role="img"
      aria-describedby="graph-canvas-a11y-desc"
    >
      <span id="graph-canvas-a11y-desc" className="sr-only">
        Interactive GPU-accelerated force-directed graph. Use the inspector panel to navigate nodes with keyboard.
      </span>
      <Cosmograph
        ref={cosmographRef}
        style={{ width: '100%', height: '100%', background: '#020617' }}
        onMount={handleMount}

        // ── Data ──────────────────────────────────────────────────────────────
        points={nodes as unknown as Record<string, unknown>[]}
        pointIdBy="id"
        pointIncludeColumns={['*']}

        links={edges as unknown as Record<string, unknown>[]}
        linkSourceBy="source"
        linkTargetBy="target"
        linkIncludeColumns={['kind']}

        // ── Node appearance ────────────────────────────────────────────────
        pointColorBy="id"
        pointColorByFn={pointColorByFn as (value: unknown) => string}
        pointSizeBy="id"
        pointSizeByFn={pointSizeByFn as (value: unknown) => number}
        pointLabelBy="label"

        // ── Edge appearance ────────────────────────────────────────────────
        linkColorBy="kind"
        linkColorByFn={linkColorByFn as (value: unknown) => string}
        linkWidthRange={highContrast ? [1, 2] : [0.5, 1.5]}

        // ── Simulation ─────────────────────────────────────────────────────
        enableSimulation={true}
        preservePointPositionsOnDataUpdate={true}

        // ── Selection / interaction ────────────────────────────────────────
        selectPointOnClick="single"
        focusPointOnClick={false}
        resetSelectionOnEmptyCanvasClick={false}

        // ── Events ────────────────────────────────────────────────────────
        onClick={handleClick}
        onBackgroundClick={handleBackgroundClick}
        onMouseMove={handleMouseMove}
        onZoom={handleZoom}

        // Suppress internal status messages — we have our own loading states
        statusIndicatorMode={false}
        disableLogging={true}
      />
    </div>
  )
}

export const GraphCanvas = memo(GraphCanvasInner)
