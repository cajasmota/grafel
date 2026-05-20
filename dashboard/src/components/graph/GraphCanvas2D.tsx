import { useRef, useCallback, useEffect, useState, memo } from 'react'
import ForceGraph2D from 'react-force-graph-2d'
import { communityColor } from '@/hooks/graph/useCommunityColors'
import { edgeKindColor } from './EdgeBadge'
import type { GraphNode, GraphEdge } from '@/types/api'
import { useGraphCameraStore } from '@/store/graphCameraStore'
import type { LayoutMode } from './GraphToolbar'

interface GraphCanvas2DProps {
  nodes: GraphNode[]
  edges: GraphEdge[]
  selectedNodeId: string | null
  hoveredNodeId: string | null
  onNodeClick: (node: GraphNode) => void
  onNodeHover: (node: GraphNode | null) => void
  onZoomChange?: (zoom: number) => void
  highContrast?: boolean
  /** Layout mode — '2d' or 'tree' both use this canvas; 'tree' activates dagMode */
  layoutMode?: LayoutMode
  className?: string
}

const NODE_BASE_R = 4
const CENTROID_SCALE = 3.0
const PAGERANK_SCALE = 12.0

/**
 * 2D force-graph.  Same prop interface as GraphCanvas3D.
 * Used for:
 * - prefers-reduced-motion (no WebGL spin)
 * - low-end devices (2D canvas is ~5x cheaper than 3D WebGL)
 * - explicit user toggle (layoutMode '2d' or 'tree')
 *
 * Fix #1000:
 * - width/height now tracked via ResizeObserver — fixes blank canvas on initial mount
 * - Tree layout uses dagMode='td' (top-down hierarchy)
 */
const GraphCanvas2DInner = ({
  nodes,
  edges,
  selectedNodeId,
  hoveredNodeId,
  onNodeClick,
  onNodeHover,
  onZoomChange,
  highContrast = false,
  layoutMode = '2d',
  className = '',
}: GraphCanvas2DProps) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const { setZoomLevel } = useGraphCameraStore()

  // Track actual container dimensions so the canvas fills the element (#1000)
  const [dims, setDims] = useState({ width: 800, height: 600 })
  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const update = () => {
      const w = Math.floor(el.clientWidth) || 800
      const h = Math.floor(el.clientHeight) || 600
      setDims({ width: w, height: h })
    }
    const ro = new ResizeObserver(update)
    ro.observe(el)
    update() // set immediately on mount
    return () => ro.disconnect()
  }, [])

  const nodeColor = useCallback((n: GraphNode) => {
    if (n.id === selectedNodeId) return '#38bdf8'
    if (n.id === hoveredNodeId) return '#e2e8f0'
    return communityColor(n.community_id ?? 0)
  }, [selectedNodeId, hoveredNodeId])

  const nodeRelSize = useCallback((n: GraphNode) => {
    if (n.is_centroid) return (n.centroid_size ?? 100) / 50 * CENTROID_SCALE
    return NODE_BASE_R + (n.pagerank ?? 0) * PAGERANK_SCALE
  }, [])

  const linkColor = useCallback((e: GraphEdge) => {
    const base = edgeKindColor(e.kind)
    return highContrast ? base : base + '99'
  }, [highContrast])

  const handleZoom = useCallback(({ k }: { k: number }) => {
    setZoomLevel(k)
    onZoomChange?.(k)
  }, [setZoomLevel, onZoomChange])

  // dagMode='td' for tree layout, undefined for standard force
  const dagMode = layoutMode === 'tree' ? 'td' : undefined

  return (
    <div
      ref={containerRef}
      className={['w-full h-full', className].join(' ')}
      aria-label="2D dependency graph"
      role="img"
      aria-describedby="graph-2d-a11y-desc"
    >
      <span id="graph-2d-a11y-desc" className="sr-only">
        Interactive 2D force-directed graph. Use the inspector panel to navigate nodes with keyboard.
      </span>
      {dims.width > 0 && dims.height > 0 && (
        <ForceGraph2D
          graphData={{
            nodes: nodes.map((n) => ({ ...n })),
            links: edges.map((e) => ({ ...e, source: e.source, target: e.target })),
          }}
          backgroundColor="#020617"
          nodeColor={nodeColor}
          nodeRelSize={nodeRelSize}
          linkColor={linkColor}
          linkWidth={highContrast ? 1.5 : 0.8}
          onNodeClick={(n) => onNodeClick(n as GraphNode)}
          onNodeHover={(n) => onNodeHover(n as GraphNode | null)}
          onZoom={handleZoom}
          cooldownTime={2000}
          d3AlphaDecay={0.02}
          d3VelocityDecay={0.3}
          width={dims.width}
          height={dims.height}
          dagMode={dagMode}
          dagLevelDistance={40}
          // Suppress cycle errors — real dependency graphs always have cycles.
          // dagMode still applies to the acyclic portion of the graph.
          onDagError={() => { /* cycles expected — suppress console error */ }}
        />
      )}
    </div>
  )
}

export const GraphCanvas2D = memo(GraphCanvas2DInner)
