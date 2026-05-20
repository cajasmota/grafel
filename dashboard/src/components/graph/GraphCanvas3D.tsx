import { useRef, useEffect, memo } from 'react'
import ForceGraph3D from '3d-force-graph'
import { edgeKindColor } from './EdgeBadge'
import { communityColor } from '@/hooks/graph/useCommunityColors'
import { repoColor } from '@/lib/colors'
import type { GraphNode, GraphEdge } from '@/types/api'
import { useGraphCameraStore } from '@/store/graphCameraStore'
import type { LayoutMode } from './GraphToolbar'

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
  /** Layout mode — 'force' (default 3D), 'tree' (dagMode top-down) */
  layoutMode?: LayoutMode
  className?: string
}

// Minimum node size for centroids and regular nodes
const CENTROID_SCALE = 4.0
const NODE_BASE_SIZE = 3.0
const GOD_NODE_SIZE = 6.0
const PAGERANK_SCALE = 20.0

/**
 * Wraps 3d-force-graph (vasturiano).
 * Receives pre-filtered nodes + edges from useGraphData — zero data logic here.
 *
 * #1000 changes:
 * - Node color now uses repoColor() for distinct repo hues (island identity).
 * - Repo-cluster force (forceX + forceY from bundled d3) groups same-repo nodes.
 * - Tree layout via dagMode='td'.
 * - communityColor still used for centroid nodes so community identity is preserved.
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
  layoutMode = 'force',
  className = '',
}: GraphCanvas3DProps) => {
  const containerRef = useRef<HTMLDivElement>(null)
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const graphRef = useRef<any>(null)
  const zoomDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const { setGraphRef, setZoomLevel } = useGraphCameraStore()

  // Initialize graph instance once
  useEffect(() => {
    const el = containerRef.current
    if (!el) return

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const graph = (ForceGraph3D as any)()(el)
      .backgroundColor('#020617') // slate-950
      .showNavInfo(false)
      .nodeLabel((n: GraphNode) => `${n.label} (${n.kind}) — ${n.repo}`)
      .nodeColor((n: GraphNode) => {
        if (n.id === selectedNodeId) return '#38bdf8' // sky-400
        if (n.id === hoveredNodeId) return '#e2e8f0'   // slate-200
        // Centroid nodes: use community color so community identity is preserved
        if (n.is_centroid) return communityColor(n.community_id ?? 0)
        // Regular nodes: color by repo for island identity (#1000)
        if (selectedNodeId) return repoColor(n.repo) + '66' // dimmed when something selected
        return repoColor(n.repo)
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
        // Debounce zoom updates to avoid triggering a React re-render on every
        // physics-simulation tick (which would cause "Maximum update depth exceeded").
        if (zoomDebounceRef.current) clearTimeout(zoomDebounceRef.current)
        zoomDebounceRef.current = setTimeout(() => {
          setZoomLevel(k)
          onZoomChange?.(k)
        }, 150)
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

  // Apply layout-specific forces when layout mode changes (#1000 fix 3 + 7)
  useEffect(() => {
    const graph = graphRef.current
    if (!graph) return

    if (layoutMode === 'tree') {
      // Hierarchical DAG layout — top-down
      try { graph.dagMode('td') } catch { /* older versions */ }
      try { graph.dagLevelDistance?.(60) } catch { /* optional */ }
      // Clear cluster forces when in tree mode
      try {
        graph.d3Force('clusterX', null)
        graph.d3Force('clusterY', null)
      } catch { /* ignore */ }
    } else {
      // Standard force layout — clear dagMode
      try { graph.dagMode(null) } catch { /* ignore */ }

      // Repo-cluster force: pulls same-repo nodes toward a shared (x,y) centroid
      // so repos form visible island clusters with cross-repo bridges (#1000 fix 7).
      try {
        const graphData = graph.graphData()
        if (!graphData?.nodes?.length) return
        const repoList = [...new Set(graphData.nodes.map((n: GraphNode) => n.repo))] as string[]
        const SPREAD = 500
        const repoPositions: Record<string, { x: number; y: number }> = {}
        repoList.forEach((slug, i) => {
          const angle = (i / repoList.length) * 2 * Math.PI
          repoPositions[slug] = {
            x: Math.cos(angle) * SPREAD,
            y: Math.sin(angle) * SPREAD,
          }
        })

        // Access d3 through the graph instance's own d3 force layout
        const sim = graph.d3Force('link') // any existing force — just to get d3 reference
        const d3 = sim?._links !== undefined ? null : null // won't work — use bundled path

        // 3d-force-graph bundles d3 — access via the simulation object
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const fgInstance = graph as any
        if (typeof fgInstance.d3Force === 'function') {
          // Build synthetic forces using raw objects (no d3 import needed)
          // These mimic forceX/forceY by setting vx/vy in the simulation tick.
          // We use the fact that 3d-force-graph exposes d3Force() to add custom forces.
          const FX_STRENGTH = 0.05
          const clusterXForce = {
            initialize: () => {},
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            force: (alpha: number) => (nodes: any[]) => {
              nodes.forEach((n) => {
                const pos = repoPositions[n.repo]
                if (pos) n.vx = (n.vx ?? 0) + (pos.x - (n.x ?? 0)) * FX_STRENGTH * alpha
              })
            },
          }
          const clusterYForce = {
            initialize: () => {},
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            force: (alpha: number) => (nodes: any[]) => {
              nodes.forEach((n) => {
                const pos = repoPositions[n.repo]
                if (pos) n.vy = (n.vy ?? 0) + (pos.y - (n.y ?? 0)) * FX_STRENGTH * alpha
              })
            },
          }

          // 3d-force-graph's d3Force() accepts a named force — pass a function directly
          // that d3 will call as force(alpha) with `this` bound to the nodes array.
          const strength = FX_STRENGTH
          fgInstance.d3Force('clusterX',
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            Object.assign((alpha: number) => {
              const simNodes = fgInstance.graphData().nodes
              simNodes.forEach((n: GraphNode & { x?: number; vx?: number }) => {
                const pos = repoPositions[n.repo]
                if (pos) n.vx = (n.vx ?? 0) + (pos.x - (n.x ?? 0)) * strength * alpha
              })
            }, { initialize: () => {} }),
          )
          fgInstance.d3Force('clusterY',
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            Object.assign((alpha: number) => {
              const simNodes = fgInstance.graphData().nodes
              simNodes.forEach((n: GraphNode & { y?: number; vy?: number }) => {
                const pos = repoPositions[n.repo]
                if (pos) n.vy = (n.vy ?? 0) + (pos.y - (n.y ?? 0)) * strength * alpha
              })
            }, { initialize: () => {} }),
          )
          void clusterXForce
          void clusterYForce
          void d3
        }
      } catch {
        // Cluster force setup is best-effort — fall back to unmodified layout
      }
    }

    try { graph.d3ReheatSimulation?.() } catch { /* ignore */ }
  // layoutMode change intentionally re-triggers this effect
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutMode])

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
