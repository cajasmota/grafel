import { useRef, useEffect, useCallback, memo, useMemo, useState } from 'react'
import { Graph } from '@cosmos.gl/graph'
import { communityColor } from '@/hooks/graph/useCommunityColors'
import { repoColor } from '@/lib/colors'
import type { GraphNode, GraphEdge } from '@/types/api'
import { useGraphCameraStore } from '@/store/graphCameraStore'
import type { ColorMode } from '@/hooks/graph/useColorMode'
import type { SimulationConfig } from '@/hooks/graph/useSimulationConfig'
import { SILK_ROAD_DEFAULTS } from '@/hooks/graph/useSimulationConfig'
import type { NodeSizingConfig } from '@/hooks/graph/useNodeSizingConfig'
import {
  buildDegreePercentileFn,
  computeTunedSize,
} from '@/hooks/graph/useNodeSizingConfig'
import type { RenderConfig } from '@/hooks/graph/useRenderConfig'
import { DEFAULT_RENDER_CONFIG } from '@/hooks/graph/useRenderConfig'

// ---------------------------------------------------------------------------
// cosmos.gl (MIT) engine wrapper — replaces @cosmograph/react (#1373)
//
// Phase 1 of the cosmos.gl migration. The @cosmograph/* product layer
// (CC-BY-NC) is dropped in favour of the MIT-licensed `@cosmos.gl/graph`
// low-level engine, driven imperatively. The surrounding chrome (sidebar,
// filters, search, inspector, keyboard nav, toolbar) is unchanged because
// this component publishes a CosmographRef-shaped shim into the camera store.
//
// cosmos.gl is a buffer-packed engine: data is uploaded as Float32Arrays
// (positions / colors / sizes / links). We compute those buffers ourselves
// in memoised packers and push them imperatively — never recreating the
// Graph instance on data change.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Semantic layout helpers (#1072 / #1106 repo-first layout)
// ---------------------------------------------------------------------------

/**
 * Derive a module key from a source_file path.
 * `src/upvate_core/serializers/foo.py` → `upvate_core/serializers`
 */
function moduleKey(sourceFile: string | undefined): string {
  if (!sourceFile) return ''
  const parts = sourceFile.replace(/\\/g, '/').split('/')
  const dirs = parts.slice(0, -1)
  return dirs.slice(-2).join('/')
}

/** Stable hash of a string. Produces values in [0, 999]. */
function hashMod1000(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = ((h << 5) - h + s.charCodeAt(i)) | 0
  }
  return Math.abs(h) % 1000
}

/**
 * #1106 — Repo-first composite cluster id:
 *   repoIdx * 10_000_000  +  community_id * 1000  +  moduleHash % 1000
 */
function clusterIdFor(n: GraphNode, repoIdx: number): number {
  const mod = hashMod1000(moduleKey(n.source_file))
  const cid = n.community_id ?? 0
  return repoIdx * 10_000_000 + cid * 1000 + mod
}

/** #1106 — Build a repo → canvas-center map (deterministic ring layout). */
function buildRepoCenters(
  nodes: GraphNode[],
): Map<string, { x: number; y: number }> {
  const repos = Array.from(new Set(nodes.map((n) => n.repo ?? ''))).sort()
  const N = repos.length
  if (N === 0) return new Map()
  const R = Math.max(3000, Math.sqrt(nodes.length) * 50)
  return new Map(
    repos.map((repo, i) => {
      const angle = (i / N) * 2 * Math.PI
      return [repo, { x: R * Math.cos(angle), y: R * Math.sin(angle) }]
    }),
  )
}

// ---------------------------------------------------------------------------
// Color helpers — cosmos.gl wants packed RGBA Float32Array
//   format: [r, g, b, a, ...] where r/g/b are 0–255 and a is 0–1.
// ---------------------------------------------------------------------------

type RGBA = [number, number, number, number]

/** Parse a #rrggbb / #rgb / rgba(...) string into [r,g,b,a] (rgb 0-255, a 0-1). */
function parseColor(c: string | null | undefined): RGBA {
  // Guard: if c is not a non-empty string, return the slate-500 fallback immediately
  if (!c || typeof c !== 'string') return [100, 116, 139, 1]
  if (c.startsWith('#')) {
    let hex = c.slice(1)
    if (hex.length === 3) {
      hex = hex.split('').map((ch) => ch + ch).join('')
    }
    // Support an optional 8th/2-char alpha suffix (e.g. repoColor() + '66')
    const r = parseInt(hex.slice(0, 2), 16)
    const g = parseInt(hex.slice(2, 4), 16)
    const b = parseInt(hex.slice(4, 6), 16)
    const a = hex.length >= 8 ? parseInt(hex.slice(6, 8), 16) / 255 : 1
    return [r, g, b, a]
  }
  const m = c.match(/rgba?\(([^)]+)\)/)
  if (m) {
    const parts = m[1].split(',').map((s) => parseFloat(s.trim()))
    return [parts[0] ?? 0, parts[1] ?? 0, parts[2] ?? 0, parts[3] ?? 1]
  }
  return [100, 116, 139, 1] // slate-500 fallback
}

/**
 * #1153 — Silk Road degree gradient ("connections count" preset).
 * t in [0,1]: low degree = cool indigo/purple, mid = pink/magenta,
 * high = warm yellow. Mirrors run.cosmograph.app's Silk Road look.
 *
 * 4-stop gradient: indigo → violet → pink → amber/yellow.
 */
// Brighter / more saturated stops than the literal Tailwind ramp: cosmos.gl
// blends points additively, so a dark indigo low end disappears into the
// background. These lifted cool→warm stops keep the gradient legible.
// Silk Road degree ramp: deep-violet (low degree) → purple → pink → yellow
// (high degree). The COOL end is kept dark/saturated on purpose: ~95% of nodes
// are low-degree and pile up in island cores, and cosmos.gl blends additively.
// A dark deep-violet floor accumulates toward purple rather than clipping to
// white, so dense low-degree cores read as COLOR. The warm hubs still pop
// because the percentile/gamma ramp pushes the rare high-degree nodes to the
// pink/yellow stops.
const SILK_STOPS: RGBA[] = [
  [49, 46, 129, 1],   // indigo-900  (low degree — dark, additive-safe floor)
  [124, 58, 237, 1],  // violet-600
  [219, 39, 119, 1],  // pink-600
  [250, 204, 21, 1],  // yellow-400  (high degree, warm)
]

function silkRoadColor(t: number): RGBA {
  const x = Math.max(0, Math.min(1, t))
  const seg = x * (SILK_STOPS.length - 1)
  const i = Math.min(Math.floor(seg), SILK_STOPS.length - 2)
  const f = seg - i
  const a = SILK_STOPS[i]
  const b = SILK_STOPS[i + 1]
  return [
    a[0] + (b[0] - a[0]) * f,
    a[1] + (b[1] - a[1]) * f,
    a[2] + (b[2] - a[2]) * f,
    1,
  ]
}

// ---------------------------------------------------------------------------
// Component interface (unchanged contract from the Cosmograph version)
// ---------------------------------------------------------------------------

export interface GraphCanvasProps {
  nodes: GraphNode[]
  edges: GraphEdge[]
  selectedNodeId: string | null
  hoveredNodeId: string | null
  onNodeClick: (node: GraphNode) => void
  onNodeHover: (node: GraphNode | null) => void
  onCursorMove?: (x: number, y: number) => void
  onEmptyClick?: () => void
  onZoomChange?: (zoom: number) => void
  highContrast?: boolean
  isDark?: boolean
  crossRepoOnly?: boolean
  simulationRunning?: boolean
  onSimulationRunningChange?: (running: boolean) => void
  className?: string
  activeRepos?: Set<string> | null
  colorMode?: ColorMode
  forceVisibleIds?: ReadonlySet<string>
  highlightedEdgeIds?: ReadonlySet<string>
  simulationConfig?: SimulationConfig
  nodeFilterIndices?: number[] | null
  nodeSizingConfig?: NodeSizingConfig
  renderConfig?: RenderConfig
}

/** Truncate long labels at ~30 chars for layout legibility */
function truncateLabel(text: string): string {
  return text.length > 30 ? text.slice(0, 28) + '…' : text
}

/** Number of HTML labels rendered in the overlay (top-N by degree). */
const LABEL_COUNT = 26

/**
 * GPU-accelerated WebGL force-graph via the MIT cosmos.gl engine.
 *
 * #1373: migrated from @cosmograph/react to @cosmos.gl/graph@2.6.4.
 * #1153: Silk Road galaxy params applied for distinct community islands.
 */
const GraphCanvasInner = ({
  nodes,
  edges,
  selectedNodeId,
  hoveredNodeId,
  onNodeClick,
  onNodeHover,
  onCursorMove,
  onEmptyClick,
  onZoomChange,
  highContrast = false,
  isDark = true,
  crossRepoOnly = false,
  simulationRunning,
  onSimulationRunningChange,
  className = '',
  activeRepos,
  colorMode = 'repo',
  forceVisibleIds,
  highlightedEdgeIds,
  simulationConfig,
  nodeFilterIndices,
  nodeSizingConfig,
  renderConfig,
}: GraphCanvasProps) => {
  // #1361: merge tunable params with Silk Road defaults
  const simCfg: SimulationConfig = useMemo(
    () => simulationConfig ? { ...SILK_ROAD_DEFAULTS, ...simulationConfig } : SILK_ROAD_DEFAULTS,
    [simulationConfig],
  )

  // #1380: merge tunable render params with defaults so nothing changes if
  // renderConfig is not supplied (maintains backward compat with all callers).
  const rc: RenderConfig = useMemo(
    () => renderConfig ? { ...DEFAULT_RENDER_CONFIG, ...renderConfig } : DEFAULT_RENDER_CONFIG,
    [renderConfig],
  )

  const containerRef = useRef<HTMLDivElement>(null)
  const graphRef = useRef<Graph | null>(null)
  const { setGraphRef, setZoomLevel } = useGraphCameraStore()

  const [hasSettled, setHasSettled] = useState(false)
  const hardStopTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Mirror of nodes so click handler can resolve index → GraphNode synchronously
  const nodesRef = useRef<GraphNode[]>(nodes)
  nodesRef.current = nodes

  const selectedNodeIdRef = useRef<string | null>(selectedNodeId)
  selectedNodeIdRef.current = selectedNodeId
  const hoveredNodeIdRef = useRef<string | null>(hoveredNodeId)
  hoveredNodeIdRef.current = hoveredNodeId

  const hoverDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const lastHoverIndexRef = useRef<number | null>(null)

  const stableEmptySet = useMemo(() => new Set<string>(), [])
  const effectiveForceIds = forceVisibleIds ?? stableEmptySet

  const repoFilterActive = activeRepos != null

  // ---------------------------------------------------------------------------
  // id → index map (backs getPointIndicesByIds in the ref shim)
  // ---------------------------------------------------------------------------
  const idToIdx = useMemo(() => {
    const m = new Map<string, number>()
    nodes.forEach((n, i) => m.set(String(n.id), i))
    return m
  }, [nodes])
  const idToIdxRef = useRef(idToIdx)
  idToIdxRef.current = idToIdx

  // ---------------------------------------------------------------------------
  // Derived per-node values
  // ---------------------------------------------------------------------------
  // scalePointsOnZoom:true means rendered screen-px = setPointSizes[i] * pointSizeScale * zoomLevel.
  // Full-fit zoom for 19k nodes ≈ 0.074 (nodes span ~12k of 32768 space on 1248px canvas).
  // With pointSizeScale=0.22: rendered_px = size * 0.22 * 0.074 = size * 0.016.
  // DEFAULT_BASE_SIZE=120 → ~2px at full-fit (visible dots), 120*0.22*5=132px at zoom=5.
  // The fallback computeSize uses the same base so both paths produce compatible sizes.
  const computeSize = (d: number): number => 120 + Math.log10(d + 1) * 30

  const sortedDegrees = useMemo(
    () => nodes.map((n) => n.degree ?? 0).sort((a, b) => a - b),
    [nodes],
  )

  const repoToIdx = useMemo(() => {
    const repos = Array.from(new Set(nodes.map((n) => n.repo ?? ''))).sort()
    return new Map(repos.map((r, i) => [r, i]))
  }, [nodes])

  const repoCenters = useMemo(() => buildRepoCenters(nodes), [nodes])

  // Per-node packed buffers (positions seed, sizes, clusters, strengths).
  const packed = useMemo(() => {
    const count = nodes.length
    const positions = new Float32Array(count * 2)
    const sizes = new Float32Array(count)
    const clusters: (number | undefined)[] = new Array(count)
    const clusterStrength = new Float32Array(count)

    let maxPR = 0
    for (const n of nodes) { if ((n.pagerank ?? 0) > maxPR) maxPR = n.pagerank ?? 0 }
    if (maxPR === 0) maxPR = 1

    const repoEntityCount = new Map<string, number>()
    for (const n of nodes) {
      const repo = n.repo ?? ''
      repoEntityCount.set(repo, (repoEntityCount.get(repo) ?? 0) + 1)
    }

    const getPercentile = buildDegreePercentileFn(sortedDegrees)

    nodes.forEach((n, i) => {
      const repoIdx = repoToIdx.get(n.repo ?? '') ?? 0
      clusters[i] = clusterIdFor(n, repoIdx)
      // Per-node pull toward the cluster center. Kept LOW so islands form but
      // don't collapse their cores into a single overplotted (additive-white)
      // point — high-pagerank hubs anchor a little harder so they sit nearer
      // the island core, while the bulk stays loosely spread for legible color.
      const normalizedPR = (n.pagerank ?? 0) / maxPR
      clusterStrength[i] = 0.04 + normalizedPR * 0.06

      sizes[i] = n.kind === 'Process'
        // Process nodes: same base scale (120) so they're visible at fit zoom
        ? 120 + Math.min((n.degree ?? 0) * 0.5, 60)
        : nodeSizingConfig
          ? computeTunedSize(n.degree ?? 0, getPercentile, nodeSizingConfig)
          : computeSize(n.degree ?? 0)

      // Seed each node near its repo center but with a WIDE per-repo spread so
      // the initial state is dispersed (not a tight ball). The cluster force
      // then tightens repo islands while repulsion keeps the galaxy expanded.
      // A tight seed collapses to a single overplotted disc under gravity.
      const center = repoCenters.get(n.repo ?? '')
      const jitterR = Math.max(600, Math.sqrt(repoEntityCount.get(n.repo ?? '') ?? 1) * 40)
      const angle = Math.random() * 2 * Math.PI
      const r = Math.random() * jitterR
      positions[i * 2] = center ? center.x + r * Math.cos(angle) : (Math.random() - 0.5) * 4000
      positions[i * 2 + 1] = center ? center.y + r * Math.sin(angle) : (Math.random() - 0.5) * 4000
    })

    return { positions, sizes, clusters, clusterStrength }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes, repoToIdx, repoCenters, sortedDegrees, nodeSizingConfig])

  // Packed node colors — depends on colorMode + selection/hover.
  // Recomputed only when colorMode / nodes / theme change (NOT on hover; hover
  // dimming is done GPU-side via selection + pointGreyoutOpacity).
  const packPointColors = useCallback((): Float32Array => {
    const count = nodes.length
    const out = new Float32Array(count * 4)
    // Degree distributions are heavily long-tailed: a handful of hubs hold most
    // of the connections while the vast majority of nodes are degree 0–2. A
    // LINEAR degree/maxDegree ramp therefore leaves ~95% of the graph stuck at
    // the cool (indigo) end and the gradient never reads. Map degree through
    // its PERCENTILE rank instead so the purple→pink→yellow ramp spreads across
    // the actual population (this is what makes the Silk Road look pop).
    const pctFn = colorMode === 'degree' ? buildDegreePercentileFn(sortedDegrees) : null
    for (let i = 0; i < count; i++) {
      const n = nodes[i]
      let rgba: RGBA
      if (colorMode === 'degree') {
        // percentile in [0,100] → t in [0,1]; gamma <1 lifts mid/high so hubs
        // reach the warm end while the bulk still shows graded cool→violet.
        const pct = pctFn!(n.degree ?? 0) / 100
        const t = Math.pow(pct, 0.7)
        rgba = silkRoadColor(t)
      } else if (colorMode === 'community') {
        // Pass community_id directly; communityColor handles -1 (ungrouped) and null/undefined
        rgba = parseColor(communityColor(n.community_id))
      } else {
        // repo mode
        if (n.is_centroid) rgba = parseColor(communityColor(n.community_id))
        else rgba = parseColor(repoColor(n.repo))
      }
      out[i * 4] = rgba[0]
      out[i * 4 + 1] = rgba[1]
      out[i * 4 + 2] = rgba[2]
      out[i * 4 + 3] = rgba[3]
    }
    return out
  }, [nodes, colorMode, sortedDegrees])

  // ---------------------------------------------------------------------------
  // Links — packed [src, tgt, ...] + per-link RGBA colors + widths
  // ---------------------------------------------------------------------------
  const linkData = useMemo(() => {
    const idToRepo = new Map(nodes.map((n) => [String(n.id), n.repo ?? '']))
    const srcIdx: number[] = []
    const tgtIdx: number[] = []
    const states: number[] = [] // 2 highlighted, 1 cross-repo, 0 same-repo

    for (const e of edges) {
      const s = idToIdx.get(String(e.source))
      const t = idToIdx.get(String(e.target))
      if (s === undefined || t === undefined) continue
      const srcRepo = idToRepo.get(String(e.source)) ?? ''
      const tgtRepo = idToRepo.get(String(e.target)) ?? ''
      const isCrossRepo = srcRepo !== tgtRepo
      if (crossRepoOnly && !isCrossRepo) continue
      const isHighlighted = highlightedEdgeIds?.has(String(e.id)) ?? false
      srcIdx.push(s)
      tgtIdx.push(t)
      states.push(isHighlighted ? 2 : (isCrossRepo ? 1 : 0))
    }

    const n = srcIdx.length
    const links = new Float32Array(n * 2)
    for (let i = 0; i < n; i++) {
      links[i * 2] = srcIdx[i]
      links[i * 2 + 1] = tgtIdx[i]
    }
    return { links, states }
  }, [nodes, edges, idToIdx, crossRepoOnly, highlightedEdgeIds])

  const packLinkColors = useCallback((): Float32Array => {
    const { states } = linkData
    const out = new Float32Array(states.length * 4)
    for (let i = 0; i < states.length; i++) {
      let rgba: RGBA
      if (states[i] === 2) {
        rgba = highContrast ? [251, 146, 60, 1.0] : [251, 146, 60, 0.85] // amber — highlighted
      } else if (states[i] === 1) {
        rgba = highContrast ? [56, 189, 248, 1.0] : [56, 189, 248, 0.7]  // sky — cross-repo
      } else {
        // same-repo: use live linkOpacity knob (#1380, was hardcoded 0.15)
        const alpha = highContrast ? Math.min(1, rc.linkOpacity * 2) : rc.linkOpacity
        rgba = [100, 116, 139, alpha] // slate
      }
      out[i * 4] = rgba[0]
      out[i * 4 + 1] = rgba[1]
      out[i * 4 + 2] = rgba[2]
      out[i * 4 + 3] = rgba[3]
    }
    return out
  }, [linkData, highContrast, rc])

  const packLinkWidths = useCallback((): Float32Array => {
    const { states } = linkData
    const out = new Float32Array(states.length)
    if (!rc.showLinks) return out // all zeros → edges hidden
    const base = highContrast ? 1.5 : 1.0
    for (let i = 0; i < states.length; i++) {
      // #1380: apply live linkWidthScale knob (was implicit via cosmos linkWidthScale config only)
      out[i] = (states[i] === 0 ? base * 0.6 : base) * rc.linkWidthScale
    }
    return out
  }, [linkData, highContrast, rc])

  // ---------------------------------------------------------------------------
  // Top-N labels by degree (HTML overlay)
  // ---------------------------------------------------------------------------
  const topLabelIndices = useMemo(() => {
    return nodes
      .map((n, i) => ({ i, deg: n.degree ?? 0 }))
      .sort((a, b) => b.deg - a.deg)
      .slice(0, LABEL_COUNT)
      .map((x) => x.i)
  }, [nodes])

  const [labelPositions, setLabelPositions] = useState<
    { idx: number; x: number; y: number; text: string }[]
  >([])
  const labelRafRef = useRef<number | null>(null)
  const labelDirtyRef = useRef(true)

  const refreshLabels = useCallback(() => {
    const g = graphRef.current
    if (!g) return
    // getPointPositions() returns the full flat [x0,y0,x1,y1,...] array and
    // works whether the simulation is running or paused (unlike the tracked-
    // positions map, which only populates during ticks).
    const positions = g.getPointPositions()
    if (!positions || positions.length === 0) return
    const out: { idx: number; x: number; y: number; text: string }[] = []
    const w = containerRef.current?.clientWidth ?? 0
    const h = containerRef.current?.clientHeight ?? 0
    for (const idx of topLabelIndices) {
      const n = nodesRef.current[idx]
      if (!n) continue
      const px = positions[idx * 2]
      const py = positions[idx * 2 + 1]
      if (px === undefined || py === undefined) continue
      const radius = g.getPointRadiusByIndex(idx)
      const [sx, sy] = g.spaceToScreenPosition([px, py])
      // Skip labels well outside the viewport to avoid clutter / offscreen DOM
      if (sx < -50 || sy < -50 || sx > w + 50 || sy > h + 50) continue
      out.push({
        idx,
        x: sx,
        y: sy - (radius ?? 4) - 6,
        text: truncateLabel(n.label ?? String(n.id)),
      })
    }
    setLabelPositions(out)
  }, [topLabelIndices])

  // ---------------------------------------------------------------------------
  // Simulation settle / hard-stop
  // ---------------------------------------------------------------------------
  const hasSettledRef = useRef(false)
  hasSettledRef.current = hasSettled

  const doSettle = useCallback(() => {
    if (hardStopTimerRef.current) {
      clearTimeout(hardStopTimerRef.current)
      hardStopTimerRef.current = null
    }
    graphRef.current?.pause()
    setHasSettled(true)
    onSimulationRunningChange?.(false)
    labelDirtyRef.current = true
    refreshLabels()
  }, [onSimulationRunningChange, refreshLabels])

  const doSettleRef = useRef(doSettle)
  doSettleRef.current = doSettle

  // ---------------------------------------------------------------------------
  // Event handlers (stable — read live values from refs)
  // ---------------------------------------------------------------------------
  const onNodeClickRef = useRef(onNodeClick)
  onNodeClickRef.current = onNodeClick
  const onEmptyClickRef = useRef(onEmptyClick)
  onEmptyClickRef.current = onEmptyClick
  const onNodeHoverRef = useRef(onNodeHover)
  onNodeHoverRef.current = onNodeHover
  const onCursorMoveRef = useRef(onCursorMove)
  onCursorMoveRef.current = onCursorMove
  const onZoomChangeRef = useRef(onZoomChange)
  onZoomChangeRef.current = onZoomChange

  const handleClick = useCallback((index: number | undefined) => {
    if (index === undefined) {
      onEmptyClickRef.current?.()
      return
    }
    const node = nodesRef.current[index]
    if (!node) return
    if (node.id === selectedNodeIdRef.current) {
      onEmptyClickRef.current?.()
      return
    }
    onNodeClickRef.current(node)
  }, [])

  const handleBackgroundClick = useCallback(() => {
    if (hoverDebounceRef.current) clearTimeout(hoverDebounceRef.current)
    lastHoverIndexRef.current = null
    graphRef.current?.unselectPoints()
    onNodeHoverRef.current(null)
    onEmptyClickRef.current?.()
  }, [])

  const handleMouseMove = useCallback((index: number | undefined) => {
    if (hoverDebounceRef.current) clearTimeout(hoverDebounceRef.current)
    if (index === undefined) {
      hoverDebounceRef.current = setTimeout(() => {
        lastHoverIndexRef.current = null
        graphRef.current?.unselectPoints()
        if (hasSettledRef.current) graphRef.current?.pause()
        onNodeHoverRef.current(null)
      }, 50)
      return
    }
    if (index === lastHoverIndexRef.current) return
    hoverDebounceRef.current = setTimeout(() => {
      lastHoverIndexRef.current = index
      const node = nodesRef.current[index]
      if (!node) return
      // selectAdjacentPoints=true highlights node + 1-degree neighbors; others
      // are greyed out GPU-side via pointGreyoutOpacity (no buffer re-upload).
      graphRef.current?.selectPointByIndex(index, true)
      if (hasSettledRef.current) graphRef.current?.pause()
      onNodeHoverRef.current(node)
    }, 50)
  }, [])

  const handleWrapperMouseMove = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    onCursorMoveRef.current?.(e.clientX, e.clientY)
  }, [])

  // ---------------------------------------------------------------------------
  // Mount — instantiate the Graph ONCE
  // ---------------------------------------------------------------------------
  useEffect(() => {
    const container = containerRef.current
    if (!container) return

    const bg = isDark ? '#020617' : '#f8fafc'

    const graph = new Graph(container, {
      backgroundColor: bg,
      // Max space so the galaxy can expand into distinct islands instead of
      // collapsing to one dense disc. Larger = more room between communities.
      // Raised from 16384 → 32768 so the seed jitter radius (up to ~10k units
      // for large repos) stays well within the half-space (16384 margin) and
      // nodes never stack against the simulation boundary (which created the
      // rectangular perimeter artifact visible at full-fit zoom).
      spaceSize: 32768,
      pixelRatio: Math.min(window.devicePixelRatio, 1.5),
      // scalePointsOnZoom: true — nodes grow visibly as you zoom in so you can
      // see and click individual nodes at deep zoom. Phase 2 had this false which
      // kept nodes as tiny dots even at maximum zoom. With scale-on-zoom enabled
      // we can afford a larger base size (see useNodeSizingConfig DEFAULT_BASE_SIZE
      // raised to 9) and a moderate pointSizeScale that balances both extremes:
      //   full-fit  → nodes are small (far out) but not washed out / white
      //   deep zoom → nodes grow with zoom and are clearly visible + clickable
      // #1380: scalePointsOnZoom, pointSizeScale, pointOpacity, linkWidthScale are
      // now driven by the renderConfig prop (live tuning panel). The initial values
      // match the previous hardcoded defaults so nothing changes until the owner tweaks.
      scalePointsOnZoom: rc.scalePointsOnZoom,
      pointSizeScale: rc.pointSizeScale,
      pointOpacity: rc.pointOpacity,
      pointGreyoutOpacity: (repoFilterActive || !!nodeFilterIndices) ? 0 : 0.18,
      linkGreyoutOpacity: repoFilterActive ? 0 : rc.linkOpacity * 0.5,
      linkWidthScale: rc.showLinks ? rc.linkWidthScale : 0,
      renderHoveredPointRing: true,
      hoveredPointRingColor: isDark ? '#e2e8f0' : '#1e293b',
      pointSamplingDistance: 120,

      // Simulation — Silk Road island params (#1153 / Phase 2). The goal is
      // run.cosmograph.app's look: many DISTINCT separated island clusters on a
      // dark field, not one fused blob. Lever summary:
      //   - near-zero gravity + zero center pull → no global collapse
      //   - strong cluster force → each repo/community contracts into an island
      //   - strong repulsion → islands push APART from each other
      //   - longer link distance → edges don't yank everything into one mass
      //   - slow decay → enough sim time for islands to separate before cooling
      enableSimulation: true,
      simulationLinkSpring: simCfg.linkSpring,
      // Longer link rest length so connected nodes don't collapse into a single
      // overplotted core; gives islands breathing room.
      simulationLinkDistance: Math.max(simCfg.linkDistance, 8),
      // Near-zero gravity: cosmos.gl gravity collapses far faster than the old
      // Cosmograph product layer, so any meaningful value fuses the islands.
      simulationGravity: 0.02,
      simulationFriction: simCfg.friction,
      // Slower decay so repulsion + cluster forces have time to pull the
      // communities into separated islands before the simulation cools.
      simulationDecay: 6000,
      // Moderate cluster force pulls each repo/community toward its own island
      // center — strong enough to form distinct islands, but NOT so strong it
      // collapses every island into a single overplotted dot (which re-creates
      // the white-core washout locally even at low opacity). We rely on the
      // higher repulsion below to give each island internal breathing room.
      simulationCluster: 0.28,
      // High repulsion does double duty: it (a) pushes the separate islands
      // APART from each other and (b) spreads nodes WITHIN each island so the
      // core isn't a single saturated point — local density drops and the
      // purple→pink→yellow degree gradient becomes legible across the island.
      simulationRepulsion: 4.0,
      // No center pull — center gravity re-fuses islands into a single mass.
      simulationCenter: 0.0,

      // rescalePositions: true — let cosmos.gl rescale the seeded ring positions
      // into the canvas space at init. Phase 2 had this false which, combined
      // with the 16384 spaceSize, caused the outermost seeded nodes (at radius
      // up to ~10k) to land near or at the simulation boundary, producing a
      // hard RECTANGULAR perimeter (nodes piled on the box edges). With
      // rescalePositions:true the engine fits the positions into the canvas
      // area so no nodes start at a boundary, and the cluster + repulsion forces
      // pull them into organic island shapes instead of a box outline.
      rescalePositions: true,
      fitViewOnInit: true,
      fitViewDelay: 3500,

      onSimulationEnd: () => {
        if (!hasSettledRef.current) doSettleRef.current()
      },
      onSimulationTick: () => {
        if (labelDirtyRef.current && labelRafRef.current === null) {
          labelRafRef.current = requestAnimationFrame(() => {
            labelRafRef.current = null
            refreshLabels()
          })
        }
      },
      onClick: (index) => handleClick(index),
      onBackgroundClick: () => handleBackgroundClick(),
      onMouseMove: (index) => handleMouseMove(index),
      onZoom: (e) => {
        const t = e.transform
        const k = t?.k ?? 1
        setZoomLevel(k)
        onZoomChangeRef.current?.(k)
        // Reposition labels on pan/zoom
        if (labelRafRef.current === null) {
          labelRafRef.current = requestAnimationFrame(() => {
            labelRafRef.current = null
            refreshLabels()
          })
        }
      },
    })

    graphRef.current = graph

    // Publish a CosmographRef-shaped shim so the camera store / toolbar /
    // keyboard nav / inspector consume the engine without code changes.
    const shim = {
      getZoomLevel: () => graph.getZoomLevel(),
      setZoomLevel: (v: number, dur?: number) => graph.setZoomLevel(v, dur),
      fitView: (dur?: number) => graph.fitView(dur),
      fitViewByCoordinates: (coords: number[], dur?: number) =>
        graph.fitViewByPointPositions(coords, dur),
      getPointPositions: () => graph.getPointPositions(),
      getPointIndicesByIds: (ids: string[]) =>
        ids.map((id) => idToIdxRef.current.get(String(id))).filter((i): i is number => i !== undefined),
      zoomToPoint: (index: number, dur?: number) => graph.zoomToPointByIndex(index, dur),
      pause: () => graph.pause(),
      unpause: () => graph.unpause(),
      start: (alpha?: number) => graph.start(alpha),
      selectPoint: (index: number, _focus?: boolean, selectAdjacent?: boolean) =>
        graph.selectPointByIndex(index, selectAdjacent),
      selectPoints: (indices?: (number | undefined)[] | null) =>
        graph.selectPointsByIndices(indices),
      unselectPoints: () => graph.unselectPoints(),
      unselectAllPoints: () => graph.unselectPoints(),
    }
    setGraphRef(shim as unknown as Parameters<typeof setGraphRef>[0])

    // Hard-stop in case onSimulationEnd never fires (#1153). Raised to 16s to
    // give the slower decay time to settle into separated islands first.
    hardStopTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current()
    }, 16000)

    return () => {
      if (hardStopTimerRef.current) clearTimeout(hardStopTimerRef.current)
      if (hoverDebounceRef.current) clearTimeout(hoverDebounceRef.current)
      if (labelRafRef.current !== null) cancelAnimationFrame(labelRafRef.current)
      graph.destroy()
      graphRef.current = null
      setGraphRef(null)
    }
  // mount-only — do NOT recreate on data/config change
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // ---------------------------------------------------------------------------
  // Data push — positions / colors / sizes / clusters / links (imperative)
  // ---------------------------------------------------------------------------
  useEffect(() => {
    const g = graphRef.current
    if (!g) return
    // Carry over current positions on data swap to avoid a full re-layout
    // (cosmos.gl has no preservePointPositionsOnDataUpdate flag).
    const prev = g.getPointPositions()
    const usePrev = hasSettledRef.current && prev.length === packed.positions.length
    if (usePrev) {
      g.setPointPositions(new Float32Array(prev), true)
    } else {
      g.setPointPositions(packed.positions)
    }
    g.setPointSizes(packed.sizes)
    g.setPointClusters(packed.clusters)
    g.setPointClusterStrength(packed.clusterStrength)
    g.setPointColors(packPointColors())
    g.setLinks(linkData.links)
    g.setLinkColors(packLinkColors())
    g.setLinkWidths(packLinkWidths())
    // render() runs graph.update() (rebuilds CPU-side buffers + inits GL
    // programs); create() then flushes per-attribute GPU buffers
    // (colors/sizes/links). Order matters: render() must run first or
    // create() throws "missing buffer pointIndices" (verified in 2.6.4 dist).
    g.render()
    g.create()
    labelDirtyRef.current = true
    if (usePrev) refreshLabels()
  }, [packed, packPointColors, linkData, packLinkColors, packLinkWidths, topLabelIndices, refreshLabels])

  // Recolor when colorMode / theme / hover-selection styling changes.
  useEffect(() => {
    const g = graphRef.current
    if (!g) return
    g.setPointColors(packPointColors())
    g.setLinkColors(packLinkColors())
    g.setLinkWidths(packLinkWidths())
    g.render()
    g.create()
  }, [packPointColors, packLinkColors, packLinkWidths])

  // ---------------------------------------------------------------------------
  // Config updates — sim sliders / theme / greyout (setConfig merges in 2.6.4)
  // ---------------------------------------------------------------------------
  useEffect(() => {
    const g = graphRef.current
    if (!g) return
    // Note: gravity / spaceSize / decay are intentionally NOT driven by simCfg
    // here — the expanded-galaxy values are set once at construction. Changing
    // spaceSize at runtime forces a costly resize + re-layout.
    g.setConfig({
      backgroundColor: isDark ? '#020617' : '#f8fafc',
      pointGreyoutOpacity: (repoFilterActive || !!nodeFilterIndices) ? 0 : 0.18,
      linkGreyoutOpacity: repoFilterActive ? 0 : 0.1,
      simulationLinkSpring: simCfg.linkSpring,
      simulationLinkDistance: simCfg.linkDistance,
      simulationFriction: simCfg.friction,
    })
  }, [isDark, repoFilterActive, nodeFilterIndices, simCfg])

  // ---------------------------------------------------------------------------
  // #1380: Live render config — apply immediately via setConfig (no re-init).
  // Debounced at 16 ms so rapid slider drags don't spam per-frame setConfig calls.
  // maxPointSize clamp: cosmos.gl 2.6.4 has no maxPointSize option; we enforce
  // it by capping pointSizeScale so a tier-5 node (base=120, mult=3.0) never
  // exceeds maxPointSize px at zoom=1.
  // ---------------------------------------------------------------------------
  const renderDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => {
    if (renderDebounceRef.current) clearTimeout(renderDebounceRef.current)
    renderDebounceRef.current = setTimeout(() => {
      const g = graphRef.current
      if (!g) return
      const BASE = 120
      const MAX_MULT = 3.0
      const clampedScale = Math.min(rc.pointSizeScale, rc.maxPointSize / (BASE * MAX_MULT))
      g.setConfig({
        pointOpacity: rc.pointOpacity,
        pointSizeScale: clampedScale,
        scalePointsOnZoom: rc.scalePointsOnZoom,
        linkWidthScale: rc.showLinks ? rc.linkWidthScale : 0,
      })
      // Re-push link colors/widths so linkOpacity + hide/show takes effect immediately
      g.setLinkColors(packLinkColors())
      g.setLinkWidths(packLinkWidths())
      g.render()
    }, 16)
    return () => {
      if (renderDebounceRef.current) clearTimeout(renderDebounceRef.current)
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rc.pointOpacity, rc.pointSizeScale, rc.scalePointsOnZoom, rc.linkWidthScale, rc.showLinks, rc.linkOpacity, rc.maxPointSize])

  // ---------------------------------------------------------------------------
  // Selection — repo filter + multi-criteria filter (visibility via greyout)
  // ---------------------------------------------------------------------------
  const visibleIndices = useMemo<number[] | null>(() => {
    if (!activeRepos) return null
    return nodes
      .map((n, i) => (activeRepos.has(n.repo) ? i : -1))
      .filter((i) => i !== -1)
  }, [nodes, activeRepos])

  useEffect(() => {
    const g = graphRef.current
    if (!g) return
    const noOverrides = !activeRepos && effectiveForceIds.size === 0 && !nodeFilterIndices
    if (noOverrides) {
      g.selectPointsByIndices(null)
      return
    }
    // Build the effective visible set: repo filter ∩ multi-criteria filter,
    // unioned with force-visible (Jarvis) ids.
    let effective: number[] | null = visibleIndices
    if (nodeFilterIndices != null) {
      if (effective === null) {
        effective = nodeFilterIndices
      } else {
        const fs = new Set(nodeFilterIndices)
        effective = effective.filter((i) => fs.has(i))
      }
    }
    if (effectiveForceIds.size > 0) {
      const set = new Set(effective ?? nodes.map((_, i) => i))
      nodes.forEach((n, i) => { if (effectiveForceIds.has(n.id)) set.add(i) })
      effective = Array.from(set)
    }
    g.selectPointsByIndices(effective)
  }, [visibleIndices, activeRepos, effectiveForceIds, nodeFilterIndices, nodes])

  // ---------------------------------------------------------------------------
  // Simulation run/pause (resume layout)
  // ---------------------------------------------------------------------------
  useEffect(() => {
    if (!hasSettled) return
    if (simulationRunning === true) graphRef.current?.start()
    else if (simulationRunning === false) graphRef.current?.pause()
  }, [simulationRunning, hasSettled])

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------
  const labelPillStyle: React.CSSProperties = isDark
    ? { background: 'rgba(2,6,23,0.72)', color: '#e2e8f0' }
    : { background: 'rgba(248,250,252,0.82)', color: '#1e293b' }

  return (
    <div
      className={['w-full h-full cursor-pointer relative', className].join(' ')}
      aria-label="Dependency graph"
      role="img"
      aria-describedby="graph-canvas-a11y-desc"
      onMouseMove={handleWrapperMouseMove}
    >
      <span id="graph-canvas-a11y-desc" className="sr-only">
        Interactive GPU-accelerated force-directed graph. Use the inspector panel to navigate nodes with keyboard.
      </span>

      {/* cosmos.gl mounts its own <canvas> inside this div */}
      <div ref={containerRef} style={{ width: '100%', height: '100%' }} />

      {/* Top-N label overlay (#1373 Phase 1 — basic HTML labels by degree) */}
      <div
        aria-hidden
        style={{ position: 'absolute', inset: 0, pointerEvents: 'none', zIndex: 10 }}
      >
        {labelPositions.map((l) => (
          <span
            key={l.idx}
            style={{
              position: 'absolute',
              left: l.x,
              top: l.y,
              transform: 'translate(-50%, -100%)',
              whiteSpace: 'nowrap',
              fontSize: 11,
              fontWeight: 500,
              padding: '1px 5px',
              borderRadius: 4,
              ...labelPillStyle,
            }}
          >
            {l.text}
          </span>
        ))}
      </div>

      {/* Vignette overlay — radial gradient for perceived depth. */}
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
