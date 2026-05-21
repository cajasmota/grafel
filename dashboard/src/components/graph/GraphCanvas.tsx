import { useRef, useEffect, useCallback, memo, useMemo, useState } from 'react'
import { Graph } from '@cosmos.gl/graph'
import { communityColor } from '@/hooks/graph/useCommunityColors'
import { repoColor } from '@/lib/colors'
import { nodeDisplayLabel } from '@/lib/utils'
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
import type { LayoutCacheEntry } from '@/hooks/graph/useLayoutCache'
import type { GroupByMode } from '@/hooks/graph/useGroupByConfig'

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
 * #1392 — "Group by" key for a node along the chosen dimension. This single
 * string identity drives BOTH the cluster id (which island a node belongs to)
 * and the island center placement, so the two always agree.
 *   'repo'      → repo slug
 *   'community' → community_id
 *   'module'    → last-2-dir module key derived from source_file
 *   'none'      → '' (no grouping)
 */
function groupKeyFor(n: GraphNode, mode: GroupByMode): string {
  switch (mode) {
    case 'repo':
      return n.repo ?? ''
    case 'community':
      return `c:${n.community_id ?? -1}`
    case 'module':
      return moduleKey(n.source_file) || `repo:${n.repo ?? ''}`
    case 'none':
    default:
      return ''
  }
}

/**
 * #1106 / #1392 — composite cluster id. When a group dimension is active the
 * id is derived from the group key so all members share one island; the legacy
 * repo-first composite is kept for the 'repo' default to preserve sub-structure.
 */
function clusterIdFor(
  n: GraphNode,
  repoIdx: number,
  mode: GroupByMode,
): number | undefined {
  if (mode === 'none') return undefined // no cluster force
  if (mode === 'repo') {
    // repo-first composite: keep community + module sub-structure inside the
    // repo island (#1106) so the island has internal texture, not a flat disc.
    const mod = hashMod1000(moduleKey(n.source_file))
    const cid = n.community_id ?? 0
    return repoIdx * 10_000_000 + cid * 1000 + mod
  }
  // community / module: one tight cluster per group key.
  return hashMod1000(groupKeyFor(n, mode)) + hashMod1000(groupKeyFor(n, mode) + '#') * 1000
}

/**
 * #1106 / #1392 — Build a group-key → canvas-center map (deterministic ring
 * layout). Used to SEED node positions near their island center so the cluster
 * force has a head start and islands separate quickly within the settle cap.
 */
function buildGroupCenters(
  nodes: GraphNode[],
  mode: GroupByMode,
): Map<string, { x: number; y: number }> {
  if (mode === 'none') return new Map()
  const keys = Array.from(new Set(nodes.map((n) => groupKeyFor(n, mode)))).sort()
  const N = keys.length
  if (N === 0) return new Map()
  // Radius scales with node count AND group count so many islands fan out wide
  // enough to stay distinct, but the ring stays bounded so the camera fit keeps
  // everything on-screen.
  const R = Math.max(3000, Math.sqrt(nodes.length) * 50, N * 700)
  return new Map(
    keys.map((key, i) => {
      const angle = (i / N) * 2 * Math.PI
      return [key, { x: R * Math.cos(angle), y: R * Math.sin(angle) }]
    }),
  )
}

// #1392-refine (item 4) — Fit padding as a fraction of the node bounding box.
// cosmos.gl's default fitView padding leaves large empty margins; a small
// padding makes the settled graph FILL the viewport. Used for both the fresh
// settle fit and the cached-layout fit.
const FIT_PADDING = 0.1

// ---------------------------------------------------------------------------
// Color helpers — cosmos.gl wants packed RGBA Float32Array
//   format: [r, g, b, a, ...] where r/g/b are 0–255 and a is 0–1.
// ---------------------------------------------------------------------------

type RGBA = [number, number, number, number]

// ---------------------------------------------------------------------------
// COLOR-CONVENTION FIX (#1392): cosmos.gl 2.6.4 reads the per-point/per-link
// color attribute as a RAW float vec4 and assigns it straight to the fragment
// color (vertex shader: `shapeColor = color;`). It does NOT divide by 255.
// So colors MUST be uploaded in the 0–1 float range. The previous packers
// produced 0–255 RGB (e.g. sky-500 = [14,165,233]); every channel >1 clamps
// to 1.0 in the shader → every node renders WHITE in every color mode.
// This is the root cause of the all-modes white-out reported in #1392.
//
// We keep the parse/gradient helpers in the human-friendly 0–255 space and
// normalise to 0–1 only at the moment we write into the GPU Float32Array.
// ---------------------------------------------------------------------------

/** Write an RGBA (rgb 0–255, a 0–1) into a packed GPU buffer at quad index i,
 *  normalising rgb to the 0–1 range cosmos.gl's shaders expect. */
function writeNormalizedRGBA(out: Float32Array, i: number, rgba: RGBA): void {
  out[i * 4] = rgba[0] / 255
  out[i * 4 + 1] = rgba[1] / 255
  out[i * 4 + 2] = rgba[2] / 255
  out[i * 4 + 3] = rgba[3]
}

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
  /**
   * #1392-refine (item 5) — click-to-focus N-hop ego graph. When non-null, only
   * these node indices are rendered (everything else hard-hidden) and the camera
   * re-fits to them so it reads as a fresh subgraph of just the related nodes.
   */
  focusedNodeIndices?: number[] | null
  nodeSizingConfig?: NodeSizingConfig
  renderConfig?: RenderConfig
  /** #1392 — clustering dimension: repo (default) / community / module / none. */
  groupBy?: GroupByMode
  /** Group slug — used as cache key namespace for position persistence. */
  group?: string
  /** Pre-loaded settled positions from localStorage (null = cold load). */
  savedLayout?: LayoutCacheEntry | null
  /** Called once when the simulation settles, with the final Float32 positions. */
  onLayoutSaved?: (positions: Float32Array) => void
  /** When true, ignore savedLayout and run a fresh simulation (Re-layout). */
  relayoutRequested?: boolean
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
  focusedNodeIndices,
  nodeSizingConfig,
  renderConfig,
  groupBy = 'repo',
  savedLayout,
  onLayoutSaved,
  relayoutRequested,
}: GraphCanvasProps) => {
  // #1361: merge tunable params with Silk Road defaults
  const simCfg: SimulationConfig = useMemo(
    () => simulationConfig ? { ...SILK_ROAD_DEFAULTS, ...simulationConfig } : SILK_ROAD_DEFAULTS,
    [simulationConfig],
  )

  // Live ref so the mount-only settle-time cap timer reads the current
  // settleTime slider value without re-running the mount effect.
  const simCfgRef = useRef(simCfg)
  simCfgRef.current = simCfg

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
  // #1392-refine: ensures we auto-start the fresh-load simulation exactly once
  // (no manual Play needed). Stays false on the cached-layout path (that path
  // settles instantly without animating).
  const didAutoStartRef = useRef(false)

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

  // #1392-refine (item 2) — base point-size scaling constants. Largest tier node
  // = BASE * MAX_MULT (matches the tier-5 multiplier in useNodeSizingConfig).
  const SIZE_BASE = 120
  const SIZE_MAX_MULT = 3.0

  // Live render-config ref so the zoom handler can read maxPointSize /
  // pointSizeScale / scalePointsOnZoom without re-binding the mount-only handler.
  const rcRef = useRef(rc)
  rcRef.current = rc

  /**
   * #1392-refine (item 2) — compute the effective pointSizeScale so the LARGEST
   * node never exceeds maxPointSize on screen, accounting for zoom.
   *
   * With scalePointsOnZoom:true, rendered_px = size * pointSizeScale * zoom.
   * Without a zoom-aware cap, zooming in inflates EVERY node into a giant
   * overlapping blob (degree differentiation lost in a wall of identical
   * circles). We cap the scale so size_max * scale * zoom <= maxPointSize:
   *   effScale = min(pointSizeScale, maxPointSize / (SIZE_BASE*SIZE_MAX_MULT*zoom))
   * Because the cap multiplies ALL sizes uniformly, the per-degree size buffer
   * still differentiates hubs from leaves — they just stop ballooning together.
   */
  const effectiveScaleForZoom = useCallback((zoom: number): number => {
    const cfg = rcRef.current
    const z = cfg.scalePointsOnZoom ? Math.max(zoom, 0.0001) : 1
    const sizeMax = SIZE_BASE * SIZE_MAX_MULT
    const zoomCap = cfg.maxPointSize / (sizeMax * z)
    return Math.min(cfg.pointSizeScale, zoomCap)
  }, [])

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

  // #1392 — island centers keyed by the active group dimension.
  const groupCenters = useMemo(
    () => buildGroupCenters(nodes, groupBy),
    [nodes, groupBy],
  )

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

    // Member count per island group (drives seed spread radius).
    const grouping = groupBy !== 'none'
    const groupEntityCount = new Map<string, number>()
    for (const n of nodes) {
      const key = groupKeyFor(n, groupBy)
      groupEntityCount.set(key, (groupEntityCount.get(key) ?? 0) + 1)
    }

    const getPercentile = buildDegreePercentileFn(sortedDegrees)

    nodes.forEach((n, i) => {
      const repoIdx = repoToIdx.get(n.repo ?? '') ?? 0
      clusters[i] = clusterIdFor(n, repoIdx, groupBy)
      // #1392 — Per-node pull toward the island center. When a group dimension
      // is active we pull HARD so each group contracts into a tight, DISTINCT
      // island that clearly separates from the others; high-pagerank hubs anchor
      // a little harder so they sit nearer the island core. With grouping off
      // (mode 'none') the cluster id is undefined so this strength is unused.
      const normalizedPR = (n.pagerank ?? 0) / maxPR
      clusterStrength[i] = grouping
        ? 0.45 + normalizedPR * 0.25 // tight islands
        : 0.04 + normalizedPR * 0.06

      sizes[i] = n.kind === 'Process'
        // Process nodes: same base scale (120) so they're visible at fit zoom
        ? 120 + Math.min((n.degree ?? 0) * 0.5, 60)
        : nodeSizingConfig
          ? computeTunedSize(n.degree ?? 0, getPercentile, nodeSizingConfig)
          : computeSize(n.degree ?? 0)

      // #1392 — Seed each node near its ISLAND center (per the active group
      // dimension) with a WIDE spread so the initial state is dispersed (not a
      // tight ball). The cluster force then tightens each island while repulsion
      // keeps the galaxy expanded. With grouping off, seed in a single broad
      // disc and let the free force layout take over.
      const gkey = groupKeyFor(n, groupBy)
      const center = grouping ? groupCenters.get(gkey) : undefined
      const jitterR = Math.max(600, Math.sqrt(groupEntityCount.get(gkey) ?? 1) * 40)
      const angle = Math.random() * 2 * Math.PI
      const r = Math.random() * jitterR
      positions[i * 2] = center ? center.x + r * Math.cos(angle) : (Math.random() - 0.5) * 4000
      positions[i * 2 + 1] = center ? center.y + r * Math.sin(angle) : (Math.random() - 0.5) * 4000
    })

    return { positions, sizes, clusters, clusterStrength }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes, repoToIdx, groupCenters, groupBy, sortedDegrees, nodeSizingConfig])

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
      writeNormalizedRGBA(out, i, rgba)
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
    // #1392: cross-repo edges are the integration "bridges" — make them visibly
    // brighter / higher-opacity than the subtle intra-repo links. The same-repo
    // base opacity comes from the live rc.linkOpacity knob; cross-repo scales UP
    // from it (clamped) and uses a bright sky hue so the islands read as bridged.
    const sameAlpha = highContrast ? Math.min(1, rc.linkOpacity * 2) : rc.linkOpacity
    const crossAlpha = highContrast ? 1.0 : Math.min(1, Math.max(0.55, rc.linkOpacity * 3.5))
    for (let i = 0; i < states.length; i++) {
      let rgba: RGBA
      if (states[i] === 2) {
        rgba = highContrast ? [251, 146, 60, 1.0] : [251, 146, 60, 0.9] // amber — highlighted
      } else if (states[i] === 1) {
        rgba = [56, 189, 248, crossAlpha]  // sky — cross-repo bridge (bright)
      } else {
        // same-repo: subtle slate, driven by live linkOpacity knob (#1380)
        rgba = [100, 116, 139, sameAlpha]
      }
      writeNormalizedRGBA(out, i, rgba)
    }
    return out
  }, [linkData, highContrast, rc])

  const packLinkWidths = useCallback((): Float32Array => {
    const { states } = linkData
    const out = new Float32Array(states.length)
    if (!rc.showLinks) return out // all zeros → edges hidden
    const base = highContrast ? 1.5 : 1.0
    for (let i = 0; i < states.length; i++) {
      // #1380: apply live linkWidthScale knob. #1392: cross-repo bridges (1) and
      // highlighted (2) get a thicker stroke so integration points stand out;
      // intra-repo (0) stay thin.
      const mult = states[i] === 0 ? 0.6 : (states[i] === 1 ? 2.2 : 1.6)
      out[i] = base * mult * rc.linkWidthScale
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
        text: truncateLabel(nodeDisplayLabel(n)),
      })
    }
    setLabelPositions(out)
  }, [topLabelIndices])

  // ---------------------------------------------------------------------------
  // Simulation settle / hard-stop
  // ---------------------------------------------------------------------------
  // #1392-refine — hasSettledRef is AUTHORITATIVE and mutated only by doSettle
  // (→true) and the re-layout / group-by effects (→false). It is intentionally
  // NOT auto-mirrored from the `hasSettled` state on every render: that mirror
  // could transiently reset the ref to a stale `false` between doSettle()'s
  // synchronous ref-set and the state commit, letting a second doSettle slip
  // past the idempotency guard and double-toggle the Play/Pause store (which
  // left the cached/settled graph drifting with the button stuck on "running").
  const hasSettledRef = useRef(false)

  // Stable refs for layout-cache props so the mount-only effect + settle
  // callback can read live values without re-running.
  const onLayoutSavedRef = useRef(onLayoutSaved)
  onLayoutSavedRef.current = onLayoutSaved
  const savedLayoutRef = useRef(savedLayout)
  savedLayoutRef.current = savedLayout
  const relayoutRequestedRef = useRef(relayoutRequested)
  relayoutRequestedRef.current = relayoutRequested

  const doSettle = useCallback(() => {
    // #1392-refine — idempotency guard. doSettle can be reached from multiple
    // sources (cached-layout rAF, onSimulationEnd, the wall-clock cap, re-layout
    // cap). hasSettledRef is mirrored from state and lags a render, so two of
    // these can fire before the re-render and BOTH run — double-toggling the
    // Play/Pause store (onSimulationRunningChange is a flip, not a setter) and
    // leaving the sim visibly running after settle. Set the ref synchronously
    // and bail if we've already settled so the body runs exactly once.
    if (hasSettledRef.current) return
    hasSettledRef.current = true
    if (hardStopTimerRef.current) {
      clearTimeout(hardStopTimerRef.current)
      hardStopTimerRef.current = null
    }
    graphRef.current?.pause()
    setHasSettled(true)
    onSimulationRunningChange?.(false)
    // #1392 — Fit the camera to the actual NODE bounding box (cosmos.gl fitView
    // frames the points, not the square simulation space) so the settled graph
    // FILLS the 16:9 viewport instead of sitting tiny inside the square space.
    // A short animation feels intentional; runs once on settle. #1392-refine
    // (item 4) — explicit small padding so the graph FILLS the viewport instead
    // of sitting tiny inside large empty margins.
    graphRef.current?.fitView(400, FIT_PADDING)
    labelDirtyRef.current = true
    // refresh labels after the fit animation positions are applied
    refreshLabels()
    setTimeout(() => { labelDirtyRef.current = true; refreshLabels() }, 450)
    // Persist settled positions so the next load can skip the simulation.
    const positions = graphRef.current?.getPointPositions()
    if (positions && positions.length > 0 && onLayoutSavedRef.current) {
      onLayoutSavedRef.current(new Float32Array(positions))
    }
  }, [onSimulationRunningChange, refreshLabels])

  const doSettleRef = useRef(doSettle)
  doSettleRef.current = doSettle

  // Live mirror of the user's run/pause intent so post-settle buffer pushes
  // (render()/create() restart cosmos's sim loop when enableSimulation:true)
  // can re-assert the paused state instead of letting the graph drift forever.
  const simulationRunningRef = useRef(simulationRunning)
  simulationRunningRef.current = simulationRunning

  /**
   * #1392-refine — re-assert the settled/paused state after an imperative
   * render()/create(). cosmos.gl restarts its simulation loop on create() when
   * enableSimulation is true; once we've settled (and the user hasn't pressed
   * Play) we must pause again or the cached/settled graph keeps drifting.
   */
  const reassertPauseIfSettled = useCallback(() => {
    if (hasSettledRef.current && simulationRunningRef.current !== true) {
      graphRef.current?.pause()
    }
  }, [])

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
      // Gate BOTH select + pause on hasSettledRef: touching the sim mid-settle
      // interrupts the tick loop and freezes a half-laid-out graph.
      if (hasSettledRef.current) {
        graphRef.current?.selectPointByIndex(index, true)
        graphRef.current?.pause()
      }
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
      pointGreyoutOpacity: (repoFilterActive || !!nodeFilterIndices || !!focusedNodeIndices) ? 0 : 0.18,
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
      // Faster decay (was 6000) so a FRESH layout cools quickly. The actual
      // ceiling on the explode/settle animation is the wall-clock settle-time
      // cap below (simCfg.settleTime), which force-calls doSettle() regardless
      // of decay. Decay 1500 lets the sim mostly settle on its own well within
      // the ~2s cap while still giving islands time to separate.
      simulationDecay: 1500,
      // #1392 — Global cluster-force multiplier on the per-node clusterStrength.
      // Raised so that when "Group by" is active each group contracts into a
      // TIGHT, DISTINCT island (the per-node strength is also raised to ~0.45
      // when grouping). Repulsion below keeps each island internally spread so
      // cores don't collapse to one overplotted dot.
      simulationCluster: 0.5,
      // High repulsion does double duty: it (a) pushes the separate islands
      // APART from each other and (b) spreads nodes WITHIN each island so the
      // core isn't a single saturated point — local density drops and the
      // purple→pink→yellow degree gradient becomes legible across the island.
      simulationRepulsion: simCfg.repulsion,
      // Center pull keeps the settled layout centered in the viewport instead
      // of drifting to the edges. Driven by the live "Center Force" slider.
      simulationCenter: simCfg.center,

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
      // #1392-refine (item 4) — fill the viewport with a small bounding-box
      // padding instead of cosmos.gl's larger default margins.
      fitViewPadding: FIT_PADDING,

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
        // #1392-refine (item 2) — zoom-aware size cap. Re-apply the effective
        // pointSizeScale so the largest node stays <= maxPointSize at this zoom,
        // preventing the "uniform giant overlapping blobs" the owner reported.
        const g = graphRef.current
        if (g && rcRef.current.scalePointsOnZoom) {
          g.setConfig({ pointSizeScale: effectiveScaleForZoom(k) })
        }
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
    if (import.meta.env.DEV) {
      ;(window as unknown as { __getGraph?: () => unknown }).__getGraph = () => graphRef.current
    }

    // If a saved layout exists and no re-layout was requested, pre-seed the
    // engine with the cached positions and settle immediately — the explode/
    // settle animation is SKIPPED entirely (the wall-clock cap below only
    // applies to fresh layouts). Use rAF so cosmos.gl finishes GL init before
    // we feed position data in.
    const hasSavedLayout =
      !relayoutRequestedRef.current && savedLayoutRef.current?.positions != null
    if (hasSavedLayout) {
      requestAnimationFrame(() => {
        const g = graphRef.current
        const entry = savedLayoutRef.current
        if (!g || !entry) return
        g.setPointPositions(entry.positions, true)
        doSettleRef.current()
      })
    }

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

    // Wall-clock settle-time CAP for a FRESH layout: force doSettle() after
    // simCfg.settleTime seconds (default 2.0s, owner-tunable via the "Settle
    // time (s)" slider) even if onSimulationEnd never fired. This is a hard
    // ceiling on the initial → explode → settle animation so it is reliably
    // ≤ the configured time and never drags on. Skipped when a saved layout is
    // restored (that path settles immediately above). The cap is read live
    // from simCfgRef so a fresh re-layout uses the current slider value.
    if (!hasSavedLayout) {
      const capSeconds = simCfgRef.current.settleTime ?? 2.0
      const capMs = Math.max(500, Math.min(6000, capSeconds * 1000))
      hardStopTimerRef.current = setTimeout(() => {
        if (!hasSettledRef.current) doSettleRef.current()
      }, capMs)
    }

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
    // #1392-refine — AUTO-SETTLE ON LOAD (item 1). On the first data upload for a
    // FRESH layout (no cached positions, no re-layout pending) start the force
    // simulation immediately so the graph settles WITHOUT the user pressing Play.
    // The wall-clock settle-time cap (mount effect) auto-pauses + saves positions
    // once it settles. The cached-layout path is skipped here — it settled
    // instantly in the mount effect. start() is also a no-op once hasSettled.
    if (!didAutoStartRef.current && !hasSettledRef.current) {
      const hasSavedLayout =
        !relayoutRequestedRef.current && savedLayoutRef.current?.positions != null
      if (!hasSavedLayout) {
        didAutoStartRef.current = true
        // Kick the force sim. We do NOT notify onSimulationRunningChange here:
        // the camera store already initialises simulationRunning=true and the
        // toolbar treats the callback as a flip (toggleSimulation), not a
        // setter. doSettle() flips it to paused once the layout settles, so
        // calling it here too would double-toggle and desync the Play/Pause
        // button (leaving it stuck showing "running" after settle).
        g.start(1)
      }
    }
    // If we've already settled (e.g. cached-layout load, or a recolor-triggered
    // re-push), create() above restarted cosmos's sim loop — re-pause so the
    // settled graph stays put instead of drifting.
    reassertPauseIfSettled()
    labelDirtyRef.current = true
    if (usePrev) refreshLabels()
  }, [packed, packPointColors, linkData, packLinkColors, packLinkWidths, topLabelIndices, refreshLabels, reassertPauseIfSettled])

  // Recolor when colorMode / theme / hover-selection styling changes.
  useEffect(() => {
    const g = graphRef.current
    if (!g) return
    g.setPointColors(packPointColors())
    g.setLinkColors(packLinkColors())
    g.setLinkWidths(packLinkWidths())
    g.render()
    g.create()
    reassertPauseIfSettled()
  }, [packPointColors, packLinkColors, packLinkWidths, reassertPauseIfSettled])

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
      pointGreyoutOpacity: (repoFilterActive || !!nodeFilterIndices || !!focusedNodeIndices) ? 0 : 0.18,
      linkGreyoutOpacity: repoFilterActive ? 0 : 0.1,
      simulationLinkSpring: simCfg.linkSpring,
      simulationLinkDistance: simCfg.linkDistance,
      simulationFriction: simCfg.friction,
      simulationRepulsion: simCfg.repulsion,
      simulationCenter: simCfg.center,
    })
  }, [isDark, repoFilterActive, nodeFilterIndices, focusedNodeIndices, simCfg])

  // Live settle-time cap: when the "Settle time (s)" slider changes WHILE a
  // fresh layout is still settling, re-arm the wall-clock cap to the new value
  // (relative to mount). If the new cap has already elapsed, settle now. Skips
  // entirely once the layout has settled. settleTime changes never touch the
  // cosmos.gl config (it has no such knob) — they only move this JS timer.
  const mountTimeRef = useRef<number>(Date.now())
  useEffect(() => {
    if (hasSettled) return
    if (savedLayoutRef.current?.positions != null && !relayoutRequestedRef.current) return
    const capMs = Math.max(500, Math.min(6000, (simCfg.settleTime ?? 2.0) * 1000))
    const elapsed = Date.now() - mountTimeRef.current
    if (hardStopTimerRef.current) clearTimeout(hardStopTimerRef.current)
    if (elapsed >= capMs) {
      if (!hasSettledRef.current) doSettleRef.current()
      return
    }
    hardStopTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current()
    }, capMs - elapsed)
  }, [simCfg.settleTime, hasSettled])

  // Re-layout: when relayoutRequested flips true, restart the force sim from
  // the current positions, reset the settle state + mount clock, and re-arm
  // the wall-clock cap so the fresh explode/settle is bounded by settleTime.
  const prevRelayoutRef = useRef(false)
  useEffect(() => {
    if (relayoutRequested && !prevRelayoutRef.current) {
      const g = graphRef.current
      if (g) {
        mountTimeRef.current = Date.now()
        setHasSettled(false)
        hasSettledRef.current = false
        onSimulationRunningChange?.(true)
        g.start(1)
        const capMs = Math.max(500, Math.min(6000, (simCfgRef.current.settleTime ?? 2.0) * 1000))
        if (hardStopTimerRef.current) clearTimeout(hardStopTimerRef.current)
        hardStopTimerRef.current = setTimeout(() => {
          if (!hasSettledRef.current) doSettleRef.current()
        }, capMs)
      }
    }
    prevRelayoutRef.current = !!relayoutRequested
  }, [relayoutRequested, onSimulationRunningChange])

  // #1392 — Re-cluster on "Group by" change. The packed clusters/seeds memo has
  // already recomputed (groupBy is a dep); push the new seed positions + cluster
  // ids and restart the force sim so the islands reform along the new dimension.
  // Bounded by the same settle-time cap. Skips the very first run (mount handles
  // the initial layout).
  const prevGroupByRef = useRef(groupBy)
  useEffect(() => {
    if (prevGroupByRef.current === groupBy) return
    prevGroupByRef.current = groupBy
    const g = graphRef.current
    if (!g) return
    // Re-seed from the fresh group-center positions so islands actually move
    // to their new homes (keeping old positions would leave them mixed).
    g.setPointPositions(packed.positions)
    g.setPointClusters(packed.clusters)
    g.setPointClusterStrength(packed.clusterStrength)
    g.render()
    g.create()
    mountTimeRef.current = Date.now()
    setHasSettled(false)
    hasSettledRef.current = false
    onSimulationRunningChange?.(true)
    g.start(1)
    const capMs = Math.max(500, Math.min(6000, (simCfgRef.current.settleTime ?? 2.0) * 1000))
    if (hardStopTimerRef.current) clearTimeout(hardStopTimerRef.current)
    hardStopTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current()
    }, capMs)
  }, [groupBy, packed, onSimulationRunningChange])

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
      // #1392-refine (item 2) — zoom-aware clamp: cap pointSizeScale so the
      // largest node stays <= maxPointSize at the CURRENT zoom (not just zoom=1).
      // This keeps the Rendering panel's maxPointSize/sizeScale knobs working
      // while preventing zoomed-in nodes from ballooning into uniform blobs.
      const currentZoom = g.getZoomLevel?.() ?? 1
      const clampedScale = effectiveScaleForZoom(currentZoom)
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
      reassertPauseIfSettled()
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
    const noOverrides =
      !activeRepos && effectiveForceIds.size === 0 && !nodeFilterIndices && !focusedNodeIndices
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
    // #1392-refine (item 5) — focus is a HARD restriction applied LAST: the
    // rendered set becomes exactly the N-hop neighborhood, intersected with any
    // active repo/criteria filter so focus never re-reveals filtered-out nodes.
    if (focusedNodeIndices != null) {
      if (effective === null) {
        effective = focusedNodeIndices
      } else {
        const fs = new Set(effective)
        effective = focusedNodeIndices.filter((i) => fs.has(i))
      }
    }
    g.selectPointsByIndices(effective)
  }, [visibleIndices, activeRepos, effectiveForceIds, nodeFilterIndices, focusedNodeIndices, nodes])

  // ---------------------------------------------------------------------------
  // #1392-refine (item 5) — re-fit the camera to the focused subgraph so it
  // FILLS the viewport (reads as a fresh, smaller graph). When focus clears,
  // re-fit back to the full node bounding box. Gated on hasSettled so we never
  // fight the initial settle fit. A short animation makes the transition read.
  // ---------------------------------------------------------------------------
  const prevFocusKeyRef = useRef<string | null>(null)
  useEffect(() => {
    if (!hasSettled) return
    const g = graphRef.current
    if (!g) return
    // Cheap structural key so we only re-fit when the focus set actually changes.
    const key = focusedNodeIndices ? focusedNodeIndices.join(',') : null
    if (prevFocusKeyRef.current === key) return
    prevFocusKeyRef.current = key
    if (focusedNodeIndices && focusedNodeIndices.length > 0) {
      g.fitViewByPointIndices(focusedNodeIndices, 400, FIT_PADDING)
    } else {
      g.fitView(400, FIT_PADDING)
    }
    labelDirtyRef.current = true
    setTimeout(() => { labelDirtyRef.current = true; refreshLabels() }, 450)
  }, [focusedNodeIndices, hasSettled, refreshLabels])

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
