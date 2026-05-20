import { useRef, useEffect, useCallback, memo, useMemo } from 'react'
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
  /** Current theme — drives canvas background color */
  isDark?: boolean
  className?: string
}

/** Truncate long labels at ~30 chars for layout legibility */
function truncateLabel(text: string): string {
  return text.length > 30 ? text.slice(0, 28) + '…' : text
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
  isDark = true,
  className = '',
}: GraphCanvasProps) => {
  const cosmographRef = useRef<CosmographRef>(undefined)
  const { setGraphRef, setZoomLevel } = useGraphCameraStore()

  // Mirror of nodes so click handler can resolve index → GraphNode synchronously
  const nodesRef = useRef<GraphNode[]>(nodes)
  nodesRef.current = nodes

  // Cosmograph requires a sequential numeric index column on both points and links.
  // We derive these from the incoming arrays rather than mutating the originals.
  const cosmographPoints = useMemo(() =>
    nodes.map((n, i) => ({ ...n, __idx: i })),
  [nodes])

  const cosmographLinks = useMemo(() => {
    const idToIdx = new Map(nodes.map((n, i) => [String(n.id), i]))
    return edges
      .map((e, i) => ({
        ...e,
        __idx: i,
        __srcIdx: idToIdx.get(String(e.source)),
        __tgtIdx: idToIdx.get(String(e.target)),
      }))
      .filter((e) => e.__srcIdx !== undefined && e.__tgtIdx !== undefined)
  }, [nodes, edges])

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

  // #1059: size nodes by degree (hub nodes appear larger than leaves).
  // CosmographPointSizeStrategy.Degree uses quantile-bounded degree distribution.
  // pointSizeRange controls min/max pixel sizes across the full degree spectrum.

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

  // Theme-aware canvas background:
  //   dark mode  → deep slate-950 (#020617) — same as before, no visual regression
  //   light mode → slate-50 (#f8fafc) — avoids the jarring black-on-light layout
  const canvasBg = isDark ? '#020617' : '#f8fafc'

  // Label pill style — gives a semi-transparent background behind label text so
  // it reads over edges. Uses inline CSS (Cosmograph className prop accepts style strings).
  const labelPillStyle = isDark
    ? 'background: rgba(2,6,23,0.72); color: #e2e8f0; font-weight: 500; padding: 1px 5px; border-radius: 4px; font-size: 11px; white-space: nowrap;'
    : 'background: rgba(248,250,252,0.82); color: #1e293b; font-weight: 500; padding: 1px 5px; border-radius: 4px; font-size: 11px; white-space: nowrap;'

  return (
    <div
      className={['w-full h-full cursor-pointer relative', className].join(' ')}
      aria-label="Dependency graph"
      role="img"
      aria-describedby="graph-canvas-a11y-desc"
    >
      <span id="graph-canvas-a11y-desc" className="sr-only">
        Interactive GPU-accelerated force-directed graph. Use the inspector panel to navigate nodes with keyboard.
      </span>

      <Cosmograph
        ref={cosmographRef}
        style={{ width: '100%', height: '100%' }}
        onMount={handleMount}

        // ── Data ──────────────────────────────────────────────────────────────
        points={cosmographPoints as unknown as Record<string, unknown>[]}
        pointIdBy="id"
        pointIndexBy="__idx"
        // Explicit allowlist guards against any future non-primitive field reaching
        // DuckDB-WASM (nested objects/arrays crash columnar type inference).
        // __idx is included so Cosmograph can resolve its numeric index lookups.
        pointIncludeColumns={['__idx', 'id', 'label', 'kind', 'repo', 'community_id', 'pagerank', 'is_centroid', 'centroid_size', 'source_file', 'start_line', 'degree']}

        links={cosmographLinks as unknown as Record<string, unknown>[]}
        linkSourceBy="source"
        linkSourceIndexBy="__srcIdx"
        linkTargetBy="target"
        linkTargetIndexBy="__tgtIdx"
        linkIncludeColumns={['kind']}

        // ── Node appearance ────────────────────────────────────────────────
        pointColorBy="id"
        pointColorByFn={pointColorByFn as (value: unknown) => string}
        // #1059: size by degree — hub nodes are visually larger than leaf nodes.
        // CosmographPointSizeStrategy.Degree uses quantile-bounded (p5–p95) degree distribution.
        pointSizeStrategy="degree"
        pointSizeRange={[4, 30]}
        pointLabelBy="label"

        // ── Labels ────────────────────────────────────────────────────────
        // #1059: show dynamic + top labels so hubs are named at a glance.
        // showDynamicLabels: evenly distributed visible nodes get labels automatically.
        // showTopLabels: highest-degree nodes always show labels regardless of viewport.
        showDynamicLabels={true}
        showTopLabels={true}
        showTopLabelsLimit={15}
        showDynamicLabelsLimit={20}
        showHoveredPointLabel={true}
        pointLabelClassName="text-xs font-mono"
        pointLabelColor="#e2e8f0"
        pointLabelFontSize={11}

        // ── Edge appearance ────────────────────────────────────────────────
        linkColorBy="kind"
        linkColorByFn={linkColorByFn as (value: unknown) => string}
        linkWidthRange={highContrast ? [1, 2] : [0.5, 1.5]}

        // ── Background ────────────────────────────────────────────────────
        backgroundColor={canvasBg}

        // ── Labels ────────────────────────────────────────────────────────
        // Truncate long entity names at 30 chars; pill background for readability.
        showLabels={true}
        showTopLabels={true}
        showTopLabelsLimit={60}
        showDynamicLabels={true}
        showDynamicLabelsLimit={40}
        showFocusedPointLabel={true}
        pointLabelFontSize={11}
        pointLabelFn={truncateLabel as (value: unknown) => string}
        pointLabelClassName={labelPillStyle}

        // ── Simulation ─────────────────────────────────────────────────────
        enableSimulation={true}
        preservePointPositionsOnDataUpdate={true}
        // Higher friction → nodes settle more smoothly (less jitter after layout)
        simulationFriction={0.7}
        // Slightly stronger repulsion → cleaner separation between dense clusters
        simulationRepulsion={0.6}

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

      {/* Vignette overlay — radial gradient darker at edges for perceived depth.
          pointer-events:none so it doesn't block canvas interaction. */}
      <div
        aria-hidden
        style={{
          position: 'absolute',
          inset: 0,
          pointerEvents: 'none',
          background: isDark
            ? 'radial-gradient(ellipse at 50% 50%, transparent 55%, rgba(2,6,23,0.55) 100%)'
            : 'radial-gradient(ellipse at 50% 50%, transparent 55%, rgba(226,232,240,0.45) 100%)',
        }}
      />
    </div>
  )
}

export const GraphCanvas = memo(GraphCanvasInner)
