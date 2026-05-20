import { useRef, useEffect, memo } from 'react'
import ForceGraph3D from '3d-force-graph'
import { kindColors } from '@/lib/colors'
import { edgeKindColor } from './EdgeBadge'
import { communityColor } from '@/hooks/graph/useCommunityColors'
import type { GraphNode, GraphEdge } from '@/types/api'
import { useGraphCameraStore } from '@/store/graphCameraStore'

interface GraphCanvas3DProps {
  nodes: GraphNode[]
  edges: GraphEdge[]
  selectedNodeId: string | null
  hoveredNodeId: string | null
  onNodeClick: (node: GraphNode) => void
  onNodeHover: (node: GraphNode | null) => void
  onZoomChange?: (zoom: number) => void
  /** High-contrast mode — thicker edges, higher node opacity */
  highContrast?: boolean
  className?: string
}

// Minimum node size for centroids and regular nodes
const CENTROID_SCALE = 4.0
const NODE_BASE_SIZE = 3.0
const GOD_NODE_SIZE = 6.0
const PAGERANK_SCALE = 20.0

function hexToRgb(hex: string): [number, number, number] {
  const r = parseInt(hex.slice(1, 3), 16) / 255
  const g = parseInt(hex.slice(3, 5), 16) / 255
  const b = parseInt(hex.slice(5, 7), 16) / 255
  return [r, g, b]
}

/**
 * Wraps 3d-force-graph (vasturiano).
 * Receives pre-filtered nodes + edges from useGraphData — zero data logic here.
 *
 * Performance targets:
 * - 60fps at 5k nodes zoom-out (centroids only — ~8 spheres)
 * - 60fps at 5k nodes mid-zoom (~400 nodes)
 * - 60fps at <1k nodes zoom-in
 */
const GraphCanvas3DInner = ({
  nodes,
  edges,
  selectedNodeId,
  hoveredNodeId,
  onNodeClick,
  onNodeHover,
  onZoomChange,
  highContrast = false,
  className = '',
}: GraphCanvas3DProps) => {
  const containerRef = useRef<HTMLDivElement>(null)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const graphRef = useRef<any>(null)
  const { setGraphRef, setZoomLevel } = useGraphCameraStore()

  // Initialize graph instance once
  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const graph = (ForceGraph3D as any)()(el)
      .backgroundColor('#020617') // slate-950
      .showNavInfo(false)
      .nodeLabel((n: GraphNode) => `${n.label} (${n.kind})`)
      .nodeColor((n: GraphNode) => {
        if (n.id === selectedNodeId) return '#38bdf8' // sky-400
        if (n.id === hoveredNodeId) return '#e2e8f0'   // slate-200
        if (n.is_centroid) return communityColor(n.community_id ?? 0)
        // Dim non-community nodes when something is selected
        if (selectedNodeId && n.community_id !== undefined) {
          return communityColor(n.community_id)
        }
        const { text } = kindColors(n.kind)
        // text is a Tailwind class — extract hue from edgeKindColor fallback
        void text
        return communityColor(n.community_id ?? 0)
      })
      .nodeVal((n: GraphNode) => {
        if (n.is_centroid) return (n.centroid_size ?? 100) / 25 * CENTROID_SCALE
        if ((n.pagerank ?? 0) > 0.6) return GOD_NODE_SIZE
        return NODE_BASE_SIZE + (n.pagerank ?? 0) * PAGERANK_SCALE
      })
      .linkColor((e: GraphEdge) => {
        const base = edgeKindColor(e.kind)
        return highContrast ? base : base + '99' // alpha
      })
      .linkWidth(highContrast ? 1.5 : 0.8)
      .linkDirectionalParticles(0)
      .linkCurvature(0.1)
      .onNodeClick((n: GraphNode) => onNodeClick(n))
      .onNodeHover((n: GraphNode | null) => {
        onNodeHover(n)
        if (el) el.style.cursor = n ? 'pointer' : 'default'
      })
      .d3AlphaDecay(0.02)
      .d3VelocityDecay(0.3)
      .cooldownTime(3000)
      .warmupTicks(50)

    // Wire zoom callback separately — some 3d-force-graph versions don't
    // return `this` from onNodeHover, breaking the fluent chain.
    if (typeof graph.onZoom === 'function') {
      graph.onZoom(({ k }: { k: number }) => {
        setZoomLevel(k)
        onZoomChange?.(k)
      })
    }

    graphRef.current = graph
    setGraphRef(graph)

    // 3d-force-graph v1.80 removed the .onZoom() chainable setter.
    // Listen to the OrbitControls 'change' event instead; camera distance
    // from origin serves as the zoom proxy.
    const controls = graph.controls()
    const handleCameraChange = () => {
      const camera = graph.camera()
      if (!camera) return
      // Distance from origin — smaller = zoomed in
      const dist = camera.position.length()
      // Invert + scale to a zoom factor similar to what .onZoom({ k }) provided
      const k = dist > 0 ? 1000 / dist : 1
      setZoomLevel(k)
      onZoomChange?.(k)
    }
    if (controls) {
      controls.addEventListener('change', handleCameraChange)
    }

    // Resize observer
    const ro = new ResizeObserver(() => {
      graph.width(el.clientWidth).height(el.clientHeight)
    })
    ro.observe(el)

    return () => {
      ro.disconnect()
      if (controls) {
        controls.removeEventListener('change', handleCameraChange)
      }
      setGraphRef(null)
      try { graph._destructor?.() } catch { /* ignore */ }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Update graph data when nodes/edges change
  useEffect(() => {
    const graph = graphRef.current
    if (!graph) return
    // Clone to avoid 3d-force-graph mutating our arrays
    graph.graphData({
      nodes: nodes.map((n) => ({ ...n })),
      links: edges.map((e) => ({ ...e, source: e.source, target: e.target })),
    })
  }, [nodes, edges])

  // Update node colors when selection changes (no re-simulation)
  useEffect(() => {
    graphRef.current?.nodeColor(graphRef.current.nodeColor())
  }, [selectedNodeId, hoveredNodeId])

  return (
    <div
      ref={containerRef}
      className={['w-full h-full', className].join(' ')}
      aria-label="3D dependency graph"
      role="img"
      // Canvas is not keyboard-navigable; EntityInspector provides list fallback
      aria-describedby="graph-a11y-desc"
    >
      <span id="graph-a11y-desc" className="sr-only">
        Interactive 3D force-directed graph. Use the inspector panel to navigate nodes with keyboard.
      </span>
    </div>
  )
}

export const GraphCanvas3D = memo(GraphCanvas3DInner)
