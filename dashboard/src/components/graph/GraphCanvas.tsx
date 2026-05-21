import { useRef, useEffect, useCallback, memo, useMemo, useState } from 'react'
import { Cosmograph } from '@cosmograph/react'
import type { CosmographRef } from '@cosmograph/react'
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

// ---------------------------------------------------------------------------
// Semantic layout helpers (#1072 / #1106 repo-first layout)
// ---------------------------------------------------------------------------

/**
 * Derive a module key from a source_file path.
 * `src/upvate_core/serializers/foo.py` → `upvate_core/serializers`
 * Returns an empty string when source_file is absent.
 */
function moduleKey(sourceFile: string | undefined): string {
  if (!sourceFile) return ''
  const parts = sourceFile.replace(/\\/g, '/').split('/')
  // Drop the filename (last segment); keep up to last 2 directory segments
  const dirs = parts.slice(0, -1)
  return dirs.slice(-2).join('/')
}

/**
 * Stable 16-bit hash of a string.  Produces values in [0, 999].
 */
function hashMod1000(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = ((h << 5) - h + s.charCodeAt(i)) | 0
  }
  return Math.abs(h) % 1000
}

/**
 * #1106 — Repo-first composite cluster id:
 *
 *   repoIdx * 10_000_000  +  community_id * 1000  +  moduleHash % 1000
 *
 * This makes REPO the dominant clustering signal so the force simulation
 * pulls same-repo nodes toward their repo's canvas region first, then
 * sub-clusters them by community and module within that region.
 *
 * The multiplier 10_000_000 ensures no aliasing between repos even when
 * community_id values run into the thousands.
 */
function clusterIdFor(n: GraphNode, repoIdx: number): number {
  const mod = hashMod1000(moduleKey(n.source_file))
  const cid = n.community_id ?? 0
  return repoIdx * 10_000_000 + cid * 1000 + mod
}

/**
 * #1106 — Build a repo → canvas-center map.
 *
 * Repos are sorted deterministically (alphabetically) so positions are
 * stable across re-renders. N repos are placed evenly on a large circle
 * whose radius scales with sqrt(nodeCount) so the islands don't overlap.
 *
 * Returns { repoName → {x, y} } for use in initial position seeding.
 */
function buildRepoCenters(
  nodes: GraphNode[],
): Map<string, { x: number; y: number }> {
  const repos = Array.from(new Set(nodes.map((n) => n.repo ?? ''))).sort()
  const N = repos.length
  if (N === 0) return new Map()
  // Scale radius with graph size so regions don't overlap at 20k+ nodes
  const R = Math.max(1500, Math.sqrt(nodes.length) * 30)
  return new Map(
    repos.map((repo, i) => {
      const angle = (i / N) * 2 * Math.PI
      return [repo, { x: R * Math.cos(angle), y: R * Math.sin(angle) }]
    }),
  )
}

// ---------------------------------------------------------------------------
// Zoom-driven Level-of-Detail (#1107 / #1120 / #1108)
// ---------------------------------------------------------------------------

/**
 * Four zoom bands — macro and overview removed (#1356 density fix).
 *
 * All nodes are always visible regardless of zoom level (degreeMin=0, topN=null).
 * Only label density decreases on zoom-out to reduce clutter.
 *
 *   high     (zoom < 1.0)   — all nodes visible, top-50 labels
 *   mid      (zoom < 2.0)   — all nodes visible, top-30 labels
 *   full     (zoom < 4.0)   — all nodes visible, top-20 labels
 *   detail   (zoom ≥ 4.0)   — all nodes visible, top-15 labels (forensics)
 *
 * Dropping macro/overview means default zoom lands in 'high' so the graph is
 * never depopulated at startup (#1356 — 3 isolated clumps in empty space fix).
 *
 * Designed to NOT restart the force simulation on band change — only the
 * Cosmograph selectPoints() call is fired (pure visibility toggle).
 */
const ZOOM_BANDS = [
  { maxZoom: 1.0,      degreeMin: 0,  topN: null as number | null, label: 'high',   topLabels: 50 },
  { maxZoom: 2.0,      degreeMin: 0,  topN: null as number | null, label: 'mid',    topLabels: 30 },
  { maxZoom: 4.0,      degreeMin: 0,  topN: null as number | null, label: 'full',   topLabels: 20 },
  { maxZoom: Infinity, degreeMin: 0,  topN: null as number | null, label: 'detail', topLabels: 15 },
]

type ZoomBand = typeof ZOOM_BANDS[number]

function pickBand(zoom: number): ZoomBand {
  return ZOOM_BANDS.find((b) => zoom < b.maxZoom) ?? ZOOM_BANDS[ZOOM_BANDS.length - 1]
}

/**
 * Compute LoD-visible indices for a given band.
 *
 * @param nodes      - full node array
 * @param band       - current zoom band
 * @param activeRepos - repo filter (null = no filter)
 * @param forceVisibleIds - node IDs that MUST remain visible regardless of band (#1157 Jarvis hook)
 */
function computeLodIndices(
  nodes: GraphNode[],
  band: ZoomBand,
  activeRepos: Set<string> | null | undefined,
  forceVisibleIds: ReadonlySet<string>,
): number[] | null {
  // #1356: All bands now show all nodes (degreeMin=0, topN=null for all bands).
  // Every band is effectively an unfiltered band — return null when no overrides active.
  const isUnfilteredBand = band.topN === null && band.degreeMin === 0
  if (isUnfilteredBand && !activeRepos && forceVisibleIds.size === 0) {
    return null
  }

  let eligible: number[]

  if (isUnfilteredBand) {
    eligible = nodes.map((_, i) => i)
  } else if (band.topN !== null) {
    // Legacy path: take top-N by degree, then also include degreeMin floor
    // (retained for forward compat if topN bands are ever re-added)
    const sorted = nodes
      .map((n, i) => ({ i, deg: n.degree ?? 0 }))
      .sort((a, b) => b.deg - a.deg)
    const topNSet = new Set(sorted.slice(0, band.topN).map((x) => x.i))
    eligible = nodes
      .map((n, i) => ({ n, i }))
      .filter(({ n, i }) => (n.degree ?? 0) >= band.degreeMin || topNSet.has(i))
      .map(({ i }) => i)
  } else {
    // Legacy path: degree threshold only (no topN cap)
    eligible = nodes
      .map((n, i) => ({ n, i }))
      .filter(({ n }) => (n.degree ?? 0) >= band.degreeMin)
      .map(({ i }) => i)
  }

  // #1157: Force-visible IDs (Jarvis highlighted nodes) must NOT be dropped by LoD.
  const forceIndices: number[] = []
  if (forceVisibleIds.size > 0) {
    nodes.forEach((n, i) => {
      if (forceVisibleIds.has(n.id)) forceIndices.push(i)
    })
  }

  // Merge force-visible into eligible (union, deduplicated)
  const eligibleSet = new Set(eligible)
  for (const fi of forceIndices) eligibleSet.add(fi)
  const merged = Array.from(eligibleSet)

  // Intersect with repo filter if active
  if (activeRepos) {
    const repoSet = activeRepos
    return merged.filter((i) => repoSet.has(nodes[i]?.repo ?? ''))
  }

  return merged
}

// ---------------------------------------------------------------------------
// ZoomBandHUD — band label overlay for power users (#1108)
// ---------------------------------------------------------------------------

/**
 * Tiny HUD chip in the bottom-right corner of the canvas that shows the
 * current zoom band label (macro / overview / high / mid / full / detail).
 *
 * The chip fades in/out on band change with a 200ms CSS transition so
 * it doesn't distract during normal use — just confirms which band is active.
 */
const ZoomBandHUD = memo(function ZoomBandHUD({
  label,
  isDark,
}: {
  label: string
  isDark: boolean
}) {
  return (
    <div
      aria-live="polite"
      aria-label={`Graph zoom band: ${label}`}
      data-testid="zoom-band-hud"
      style={{
        position: 'absolute',
        bottom: 12,
        right: 14,
        pointerEvents: 'none',
        zIndex: 20,
        padding: '2px 7px',
        borderRadius: 5,
        fontSize: 10,
        fontWeight: 600,
        letterSpacing: '0.06em',
        textTransform: 'uppercase',
        userSelect: 'none',
        // Fade-in transition on band change (#1108 smooth transitions)
        transition: 'opacity 200ms ease, background 200ms ease, color 200ms ease',
        background: isDark ? 'rgba(2,6,23,0.65)' : 'rgba(248,250,252,0.80)',
        color: isDark ? 'rgba(148,163,184,0.85)' : 'rgba(51,65,85,0.80)',
        border: isDark ? '1px solid rgba(51,65,85,0.4)' : '1px solid rgba(148,163,184,0.4)',
      }}
    >
      {label}
    </div>
  )
})

// ---------------------------------------------------------------------------
// Hub pulse animation helpers (#1153 — Silk Road aesthetic)
// ---------------------------------------------------------------------------

/**
 * Returns the indices of the top-N degree hubs in the node array.
 * Used to identify which nodes get the post-settle pulse animation.
 */
function topHubIndices(nodes: GraphNode[], n = 12): number[] {
  return nodes
    .map((node, i) => ({ i, deg: node.degree ?? 0 }))
    .sort((a, b) => b.deg - a.deg)
    .slice(0, n)
    .map((x) => x.i)
}

// ---------------------------------------------------------------------------
// Component interface
// ---------------------------------------------------------------------------

export interface GraphCanvasProps {
  nodes: GraphNode[]
  edges: GraphEdge[]
  selectedNodeId: string | null
  hoveredNodeId: string | null
  onNodeClick: (node: GraphNode) => void
  onNodeHover: (node: GraphNode | null) => void
  /** Called when the cursor moves over the canvas — provides screen coords for tooltip */
  onCursorMove?: (x: number, y: number) => void
  /** Called when user clicks empty canvas (no node hit) */
  onEmptyClick?: () => void
  onZoomChange?: (zoom: number) => void
  /** High-contrast mode — wider edges, higher opacity */
  highContrast?: boolean
  /** Current theme — drives canvas background color */
  isDark?: boolean
  /** When true, filter links to cross-repo edges only (#1065) */
  crossRepoOnly?: boolean
  /**
   * When true, the force simulation is paused (nodes frozen).
   * When false/undefined, simulation runs and auto-pauses after first settle.
   * Pass true to resume layout after the user clicks "Resume layout".
   */
  simulationRunning?: boolean
  /** Called when the internal simulation-running state changes (e.g. auto-paused after settle) */
  onSimulationRunningChange?: (running: boolean) => void
  className?: string
  /**
   * #1069: client-side repo filter. When non-null, only nodes whose `repo`
   * is in this set are shown. Non-matching nodes are hidden via Cosmograph
   * selection greyout (pointGreyoutOpacity=0) without triggering a refetch
   * or re-running the force layout.
   *
   * null / undefined = show all repos.
   */
  activeRepos?: Set<string> | null
  /**
   * #1153: 3-way color mode.
   *   'repo'      — per-repo color (default)
   *   'degree'    — Cosmograph's 'connections count' gradient (Silk Road look)
   *   'community' — community_id deterministic palette
   */
  colorMode?: ColorMode
  /**
   * #1157: Node IDs that MUST remain visible regardless of LoD zoom band.
   * Used by the Jarvis MCP highlighting overlay. Pass an empty Set
   * (or omit) when no Jarvis session is active.
   */
  forceVisibleIds?: ReadonlySet<string>
  /**
   * #1157 Phase 2: Edge IDs currently highlighted by the Jarvis MCP overlay.
   * Highlighted edges are rendered with the Jarvis pulse color instead of
   * the standard cross-repo / same-repo color.
   */
  highlightedEdgeIds?: ReadonlySet<string>
  /**
   * #1361: Tunable simulation params — live-applied from the sidebar sliders.
   * Omit to use Silk Road defaults.
   */
  simulationConfig?: SimulationConfig
  /**
   * #1367: Multi-criteria filter — pre-computed array of node indices that pass
   * all active filter criteria. When non-null the canvas shows only these nodes
   * (intersected with the repo filter and LoD band). null = no filter active.
   */
  nodeFilterIndices?: number[] | null
  /**
   * #1366: Minimap — called on every zoom/pan with the current d3 transform
   * so the minimap can draw the viewport indicator.
   */
  onZoomTransform?: (transform: { k: number; x: number; y: number }) => void
  /**
   * #1366: Minimap — called after simulation settles (or on demand)
   * to pass the flat [x0,y0,x1,y1,...] node positions to the minimap.
   */
  onNodePositions?: (positions: Float32Array | number[]) => void
  /**
   * #1360: Tunable node sizing config — base size + per-tier multipliers.
   * When omitted, falls back to the legacy log-scale formula.
   */
  nodeSizingConfig?: NodeSizingConfig
}

/** Truncate long labels at ~30 chars for layout legibility */
function truncateLabel(text: string): string {
  return text.length > 30 ? text.slice(0, 28) + '…' : text
}

// ---------------------------------------------------------------------------
// Cosmograph pointColorStrategy passthrough
// ---------------------------------------------------------------------------

// 'connections count' is the Silk Road degree gradient — purple → pink → yellow.
// This value is passed directly to Cosmograph when colorMode === 'degree'.
// Defined as a constant so it doesn't cause useMemo invalidations.
const DEGREE_COLOR_STRATEGY = 'connections count'

/**
 * GPU-accelerated WebGL force-graph via Cosmograph.
 *
 * Replaces GraphCanvas3D + GraphCanvas2D (#1023).
 * - Receives pre-filtered nodes + edges from useGraphData
 * - Single canvas, 2D force simulation (60fps at 1M+ nodes)
 * - Drop-in prop interface — route component unchanged
 *
 * #1153: Silk Road galaxy params applied for distinct community islands.
 * #1157: Jarvis MCP highlight overlay channel reserved via forceVisibleIds.
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
  onZoomTransform,
  onNodePositions,
  nodeSizingConfig,
}: GraphCanvasProps) => {
  // #1361: merge tunable params with Silk Road defaults so omitted props fall back correctly
  const simCfg: SimulationConfig = useMemo(
    () => simulationConfig ? { ...SILK_ROAD_DEFAULTS, ...simulationConfig } : SILK_ROAD_DEFAULTS,
    [simulationConfig],
  )

  const cosmographRef = useRef<CosmographRef>(undefined)
  const { setGraphRef, setZoomLevel } = useGraphCameraStore()

  // Track whether the first settle has happened so we only auto-pause once.
  const [hasSettled, setHasSettled] = useState(false)

  // #1107: zoom-driven LoD — track current zoom with a small debounce
  // so we don't re-compute visibility on every micro-zoom step.
  const [currentZoom, setCurrentZoom] = useState(0.3)

  // Hard-stop timer ref — cleared on unmount and when sim settles naturally.
  const hardStopTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Pulse animation — runs once after sim settles. cancelPulseRef stops it on unmount.
  const cancelPulseRef = useRef<(() => void) | null>(null)

  // Mirror of nodes so click handler can resolve index → GraphNode synchronously
  const nodesRef = useRef<GraphNode[]>(nodes)
  nodesRef.current = nodes

  // Debounce timer for hover — prevents thrashing on rapid micro-movements
  const hoverDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Track the last hovered index so we can avoid redundant selectPoint calls
  const lastHoverIndexRef = useRef<number | null>(null)

  // #1157: stable empty set for the default forceVisibleIds
  const stableEmptySet = useMemo(() => new Set<string>(), [])
  const effectiveForceIds = forceVisibleIds ?? stableEmptySet

  // #1069: client-side repo filter — compute numeric indices of visible nodes.
  const repoFilterActive = activeRepos != null
  const visibleIndices = useMemo<number[] | null>(() => {
    if (!activeRepos) return null  // no filter — show all
    return nodes
      .map((n, i) => (activeRepos.has(n.repo) ? i : -1))
      .filter((i) => i !== -1)
  }, [nodes, activeRepos])

  // Keep a ref to latest visibleIndices so handleMount can re-apply after mount
  const visibleIndicesRef = useRef<number[] | null>(visibleIndices)
  visibleIndicesRef.current = visibleIndices

  // #1107 / #1157: Zoom-driven LoD — compute which nodes are visible for current zoom band.
  // forceVisibleIds nodes are NEVER filtered out (#1157 Jarvis constraint).
  const currentBand = useMemo(() => pickBand(currentZoom), [currentZoom])

  const lodVisibleIndices = useMemo<number[] | null>(() => {
    return computeLodIndices(nodes, currentBand, activeRepos, effectiveForceIds)
  }, [nodes, currentBand, activeRepos, effectiveForceIds])

  // Apply LoD visibility via Cosmograph's imperative selection API.
  // #1367: when nodeFilterIndices is non-null, intersect it with the LoD band result.
  useEffect(() => {
    const cosmo = cosmographRef.current
    if (!cosmo) return

    // #1356: all bands are now unfiltered (topN=null, degreeMin=0).
    const isUnfilteredBand = currentBand.topN === null && currentBand.degreeMin === 0
    const noOverrides = !activeRepos && effectiveForceIds.size === 0 && !nodeFilterIndices

    if (isUnfilteredBand && noOverrides) {
      cosmo.selectPoints(null)
    } else {
      // Intersect lod indices with nodeFilterIndices when both are present
      let effective: number[] | null = lodVisibleIndices
      if (nodeFilterIndices !== null && nodeFilterIndices !== undefined) {
        if (effective === null) {
          effective = nodeFilterIndices
        } else {
          const filterSet = new Set(nodeFilterIndices)
          effective = effective.filter((i) => filterSet.has(i))
        }
      }
      cosmo.selectPoints(effective)
    }
  }, [lodVisibleIndices, currentBand, activeRepos, effectiveForceIds, nodeFilterIndices])

  // Fallback repo-filter application when band is unfiltered (before LoD fires)
  useEffect(() => {
    const cosmo = cosmographRef.current
    if (!cosmo) return
    const isUnfilteredBand = currentBand.topN === null && currentBand.degreeMin === 0
    if (!isUnfilteredBand) return
    // If nodeFilterIndices active, intersect with repo filter
    if (nodeFilterIndices !== null && nodeFilterIndices !== undefined) {
      if (visibleIndices !== null) {
        const filterSet = new Set(nodeFilterIndices)
        cosmo.selectPoints(visibleIndices.filter((i) => filterSet.has(i)))
      } else {
        cosmo.selectPoints(nodeFilterIndices)
      }
    } else {
      cosmo.selectPoints(visibleIndices)
    }
  }, [visibleIndices, currentBand, nodeFilterIndices])

  // ---------------------------------------------------------------------------
  // Node data with pre-computed cluster + size + position fields
  // ---------------------------------------------------------------------------

  // Legacy log-scale formula — used as fallback when no nodeSizingConfig is provided.
  const computeSize = (d: number): number =>
    2 + Math.log10(d + 1) * 12

  // #1360: Tunable sizing — pre-sort degrees for percentile lookup.
  const sortedDegrees = useMemo(
    () => nodes.map((n) => n.degree ?? 0).sort((a, b) => a - b),
    [nodes],
  )

  const repoToIdx = useMemo(() => {
    const repos = Array.from(new Set(nodes.map((n) => n.repo ?? ''))).sort()
    return new Map(repos.map((r, i) => [r, i]))
  }, [nodes])

  const repoCenters = useMemo(() => buildRepoCenters(nodes), [nodes])

  const cosmographPoints = useMemo(() => {
    let maxPR = 0
    for (const n of nodes) { if ((n.pagerank ?? 0) > maxPR) maxPR = n.pagerank ?? 0 }
    if (maxPR === 0) maxPR = 1

    const repoEntityCount = new Map<string, number>()
    for (const n of nodes) {
      const repo = n.repo ?? ''
      repoEntityCount.set(repo, (repoEntityCount.get(repo) ?? 0) + 1)
    }

    // #1360: build percentile lookup once per node set
    const getPercentile = buildDegreePercentileFn(sortedDegrees)

    return nodes.map((n, i) => {
      const repoIdx = repoToIdx.get(n.repo ?? '') ?? 0
      const cid = clusterIdFor(n, repoIdx)
      const normalizedPR = (n.pagerank ?? 0) / maxPR
      const strength = 0.10 + normalizedPR * 0.08
      // #1127 P3: Process entities keep flat small range (sizing config doesn't apply)
      // #1360: all other kinds use tunable sizing when config is provided
      const __size = n.kind === 'Process'
        ? 4 + Math.min((n.degree ?? 0) * 0.005, 6)
        : nodeSizingConfig
          ? computeTunedSize(n.degree ?? 0, getPercentile, nodeSizingConfig)
          : computeSize(n.degree ?? 0)
      const center = repoCenters.get(n.repo ?? '')
      const jitterR = Math.sqrt(repoEntityCount.get(n.repo ?? '') ?? 1) * 8
      const angle = Math.random() * 2 * Math.PI
      const r = Math.random() * jitterR
      const __x = center ? center.x + r * Math.cos(angle) : 0
      const __y = center ? center.y + r * Math.sin(angle) : 0
      return { ...n, __idx: i, __cluster_id: cid, __cluster_strength: strength, __size, __x, __y }
    })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes, repoToIdx, repoCenters, sortedDegrees, nodeSizingConfig])

  const cosmographLinks = useMemo(() => {
    const idToIdx = new Map(nodes.map((n, i) => [String(n.id), i]))
    const idToRepo = new Map(nodes.map((n) => [String(n.id), n.repo ?? '']))
    return edges
      .map((e, i) => {
        const srcRepo = idToRepo.get(String(e.source)) ?? ''
        const tgtRepo = idToRepo.get(String(e.target)) ?? ''
        const isCrossRepo = srcRepo !== tgtRepo
        // #1157 Phase 2: __linkState encodes both highlight + cross-repo in one column:
        //   2 = Jarvis-highlighted (takes priority)
        //   1 = cross-repo (not highlighted)
        //   0 = same-repo (not highlighted)
        const isHighlighted = highlightedEdgeIds?.has(String(e.id)) ?? false
        const __linkState = isHighlighted ? 2 : (isCrossRepo ? 1 : 0)
        return {
          ...e,
          __idx: i,
          __srcIdx: idToIdx.get(String(e.source)),
          __tgtIdx: idToIdx.get(String(e.target)),
          __crossRepo: isCrossRepo ? 1 : 0,
          __linkState,
        }
      })
      .filter((e) => e.__srcIdx !== undefined && e.__tgtIdx !== undefined)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodes, edges, highlightedEdgeIds])

  const visibleLinks = useMemo(
    () => crossRepoOnly ? cosmographLinks.filter((e) => e.__crossRepo === 1) : cosmographLinks,
    [cosmographLinks, crossRepoOnly],
  )

  const lodVisibleIndicesRef = useRef<number[] | null>(null)
  lodVisibleIndicesRef.current = lodVisibleIndices

  // ---------------------------------------------------------------------------
  // Mount / camera setup
  // ---------------------------------------------------------------------------

  const handleMount = useCallback((instance: NonNullable<CosmographRef>) => {
    cosmographRef.current = instance
    setGraphRef(instance as unknown as Parameters<typeof setGraphRef>[0])

    // #1153: fitViewOnInit=true + fitViewDelay=1000ms for smooth initial fit.
    // Also set initial zoom to overview band after fit completes.
    setTimeout(() => {
      instance.setZoomLevel?.(0.3, 300)  // smooth 300ms zoom transition
      setCurrentZoom(0.3)
    }, 1200)  // after fitViewDelay (1000ms) + buffer

    const lodIndices = lodVisibleIndicesRef.current
    instance.selectPoints(lodIndices)
  }, [setGraphRef])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      setGraphRef(null)
      if (hoverDebounceRef.current) clearTimeout(hoverDebounceRef.current)
      if (hardStopTimerRef.current) clearTimeout(hardStopTimerRef.current)
      if (zoomDebounceRef.current) clearTimeout(zoomDebounceRef.current)
      cancelPulseRef.current?.()
    }
  }, [setGraphRef])

  // ---------------------------------------------------------------------------
  // Hub pulse animation (#1153 — gentle 3-5 second cycle, fades to steady state)
  // ---------------------------------------------------------------------------

  /**
   * After the simulation settles, pulse the top-N degree hubs to make the
   * graph feel alive.  We do 3 pulse cycles (each ~3.5 seconds), then stop.
   * The animation is implemented via Cosmograph's selectPoint / unselectAllPoints
   * so it composes correctly with the base color mode and the Jarvis overlay.
   *
   * The pulse is separate from the base color mode:
   *   - selectPoints() highlights specific indices
   *   - The base pointColorByFn is applied by Cosmograph regardless
   *   - This only triggers greyout of non-selected nodes during the pulse window
   *
   * We skip the pulse in degree-color mode since the gradient already provides
   * visual hierarchy and the greyout would fight the Silk Road look.
   */
  const startHubPulse = useCallback((cosmo: NonNullable<CosmographRef>, hubIndices: number[]) => {
    if (hubIndices.length === 0) return

    let cancelled = false
    let cycleCount = 0
    const MAX_CYCLES = 3

    // Store a cancel fn so unmount can abort the animation
    cancelPulseRef.current = () => { cancelled = true }

    function runCycle() {
      if (cancelled || cycleCount >= MAX_CYCLES) {
        // Fade out — restore full visibility at end of animation
        if (!cancelled) cosmo.unselectAllPoints()
        cancelPulseRef.current = null
        return
      }
      cycleCount++

      // Pulse ON — highlight hubs
      cosmo.selectPoints(hubIndices)
      // Re-pause to prevent selectPoints from bumping alpha
      cosmo.pause()

      // Pulse OFF after 800ms — restore all
      const offTimer = setTimeout(() => {
        if (cancelled) return
        cosmo.unselectAllPoints()
        cosmo.pause()

        // Wait gap before next pulse — ~2.5s between pulses for 3-4s total cycle
        const gapTimer = setTimeout(() => {
          if (!cancelled) runCycle()
        }, 2500)
        cancelPulseRef.current = () => { cancelled = true; clearTimeout(gapTimer) }
      }, 800)

      cancelPulseRef.current = () => { cancelled = true; clearTimeout(offTimer) }
    }

    // Short delay so the camera has finished fitting before pulse starts
    const startTimer = setTimeout(runCycle, 500)
    cancelPulseRef.current = () => { cancelled = true; clearTimeout(startTimer) }
  }, [])

  // ---------------------------------------------------------------------------
  // Simulation settle / hard-stop
  // ---------------------------------------------------------------------------

  const hasSettledRef = useRef(false)
  hasSettledRef.current = hasSettled

  // #1366: ref wrapper so doSettle can always call the latest onNodePositions
  const onNodePositionsRef = useRef(onNodePositions)
  onNodePositionsRef.current = onNodePositions

  const doSettle = useCallback(() => {
    if (hardStopTimerRef.current) {
      clearTimeout(hardStopTimerRef.current)
      hardStopTimerRef.current = null
    }
    cosmographRef.current?.pause()
    setHasSettled(true)
    onSimulationRunningChange?.(false)

    // #1366: emit node positions for the minimap once layout settles
    // Use a small delay so positions are fully committed
    if (onNodePositionsRef.current) {
      setTimeout(() => {
        const positions = cosmographRef.current?.getPointPositions?.()
        if (positions && positions.length > 0) {
          onNodePositionsRef.current?.(positions)
        }
      }, 500)
    }

    // Kick off hub pulse in repo/community modes — skip in degree mode
    // (the Silk Road gradient already provides the "alive" look)
    if (colorMode !== 'degree' && cosmographRef.current) {
      const hubs = topHubIndices(nodesRef.current, 12)
      startHubPulse(cosmographRef.current, hubs)
    }
  }, [onSimulationRunningChange, colorMode, startHubPulse])

  const handleSimulationEnd = useCallback(() => {
    if (!hasSettled) {
      doSettle()
    }
  }, [hasSettled, doSettle])

  useEffect(() => {
    if (hasSettled) return
    hardStopTimerRef.current = window.setTimeout(() => {
      if (!hasSettledRef.current) {
        doSettle()
      }
    }, 8000)
    return () => {
      if (hardStopTimerRef.current) {
        clearTimeout(hardStopTimerRef.current)
        hardStopTimerRef.current = null
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!hasSettled) return
    if (simulationRunning === true) {
      cosmographRef.current?.start()
    } else if (simulationRunning === false) {
      cosmographRef.current?.pause()
    }
  }, [simulationRunning, hasSettled])

  // ---------------------------------------------------------------------------
  // Point color logic — decoupled from selectPoints() (#1157 Jarvis constraint)
  //
  // colorMode='repo'      → pointColorByFn on 'id' column (existing behavior)
  // colorMode='degree'    → pointColorStrategy='connections count' (Cosmograph built-in)
  // colorMode='community' → pointColorByFn on 'community_id' column
  //
  // selectPoints() is NEVER called from inside these accessors — it is only
  // called from the LoD effect, the hover handler, and the hub pulse animation.
  // This ensures the Jarvis highlight overlay (Phase 1) can call selectPoints()
  // independently without fighting the color accessor.
  // ---------------------------------------------------------------------------

  // Repo mode: color by id (so we can match selectedNodeId/hoveredNodeId)
  const pointColorByFnRepo = useCallback((nodeId: string) => {
    if (nodeId === selectedNodeId) return '#38bdf8'   // sky-400 — selected
    if (nodeId === hoveredNodeId)  return '#e2e8f0'   // slate-200 — hovered
    const node = nodesRef.current.find((n) => n.id === nodeId)
    if (!node) return '#64748b'
    if (node.is_centroid) return communityColor(node.community_id ?? 0)
    if (selectedNodeId) return repoColor(node.repo) + '66' // dimmed when another selected
    return repoColor(node.repo)
  }, [selectedNodeId, hoveredNodeId])

  // Community mode: color by community_id (numeric column)
  const pointColorByFnCommunity = useCallback((communityId: unknown) => {
    const cid = typeof communityId === 'number' ? communityId
      : typeof communityId === 'string' ? parseInt(communityId, 10)
      : 0
    return communityColor(isNaN(cid) ? 0 : cid)
  }, [])

  // Link color accessor — handles Jarvis highlight (state=2), cross-repo (state=1), same-repo (state=0)
  // #1157 Phase 2: __linkState=2 → Jarvis pulse color (amber glow), takes priority over cross-repo.
  const linkColorByFn = useCallback((linkState: unknown) => {
    const state = typeof linkState === 'number' ? linkState
      : typeof linkState === 'string' ? parseInt(linkState, 10)
      : 0
    if (state === 2) {
      // Jarvis highlighted edge — amber/orange pulse color
      return highContrast ? 'rgba(251,146,60,1.0)' : 'rgba(251,146,60,0.85)'
    }
    if (state === 1) {
      // Cross-repo edge
      return highContrast ? 'rgba(56,189,248,1.0)' : 'rgba(56,189,248,0.7)'
    }
    return highContrast ? 'rgba(100,116,139,0.5)' : 'rgba(100,116,139,0.15)'
  }, [highContrast])

  // ---------------------------------------------------------------------------
  // Click / hover handlers
  // ---------------------------------------------------------------------------

  const handleClick = useCallback((index: number | undefined) => {
    if (index === undefined) {
      onEmptyClick?.()
      return
    }
    const node = nodesRef.current[index]
    if (!node) return
    if (node.id === selectedNodeId) {
      onEmptyClick?.()
      return
    }
    onNodeClick(node)
  }, [onNodeClick, onEmptyClick, selectedNodeId])

  const handleBackgroundClick = useCallback(() => {
    if (hoverDebounceRef.current) clearTimeout(hoverDebounceRef.current)
    lastHoverIndexRef.current = null
    cosmographRef.current?.unselectAllPoints()
    onNodeHover(null)
    onEmptyClick?.()
  }, [onNodeHover, onEmptyClick])

  const handleMouseMove = useCallback((index: number | undefined) => {
    if (hoverDebounceRef.current) clearTimeout(hoverDebounceRef.current)

    if (index === undefined) {
      hoverDebounceRef.current = setTimeout(() => {
        lastHoverIndexRef.current = null
        cosmographRef.current?.unselectAllPoints()
        if (hasSettledRef.current) cosmographRef.current?.pause()
        onNodeHover(null)
      }, 50)
      return
    }

    if (index === lastHoverIndexRef.current) return

    hoverDebounceRef.current = setTimeout(() => {
      lastHoverIndexRef.current = index
      const node = nodesRef.current[index]
      if (!node) return
      // selectPoint with selectConnectedPoints=true highlights node + 1-degree neighbors.
      // This is decoupled from the base color mode — Jarvis overlay can compose on top.
      cosmographRef.current?.selectPoint(index, false, true)
      if (hasSettledRef.current) cosmographRef.current?.pause()
      onNodeHover(node)
    }, 50)
  }, [onNodeHover])

  const handleWrapperMouseMove = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    onCursorMove?.(e.clientX, e.clientY)
  }, [onCursorMove])

  // Debounce timer for zoom LoD updates
  const zoomDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const handleZoom = useCallback((e: unknown) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const t = (e as any)?.transform
    const k: number = t?.k ?? 1
    const x: number = t?.x ?? 0
    const y: number = t?.y ?? 0
    setZoomLevel(k)
    onZoomChange?.(k)
    // #1366: propagate full transform to minimap
    onZoomTransform?.({ k, x, y })
    if (zoomDebounceRef.current) clearTimeout(zoomDebounceRef.current)
    zoomDebounceRef.current = setTimeout(() => {
      setCurrentZoom(k)
    }, 80)
  }, [setZoomLevel, onZoomChange, onZoomTransform])

  // ---------------------------------------------------------------------------
  // Theme + label styles
  // ---------------------------------------------------------------------------

  const canvasBg = isDark ? '#020617' : '#f8fafc'

  const labelPillStyle = isDark
    ? 'background: rgba(2,6,23,0.72); color: #e2e8f0; font-weight: 500; padding: 1px 5px; border-radius: 4px; font-size: 11px; white-space: nowrap;'
    : 'background: rgba(248,250,252,0.82); color: #1e293b; font-weight: 500; padding: 1px 5px; border-radius: 4px; font-size: 11px; white-space: nowrap;'

  // ---------------------------------------------------------------------------
  // Cosmograph config derived from colorMode
  // ---------------------------------------------------------------------------

  // When colorMode='degree', Cosmograph's built-in strategy drives colors.
  // We must not pass pointColorByFn or pointColorBy in that case — they conflict.
  const isDegreeMode = colorMode === 'degree'
  const isCommunityMode = colorMode === 'community'

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

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

      {/* Hover cursor ring — subtle white ring that follows the cursor on the canvas.
          Implemented as a CSS-animated pseudo-element overlay so it's always on top
          of the WebGL canvas without requiring canvas access. The ring appears only
          when the cursor is inside the canvas div (tracked via onMouseMove).
          pointer-events: none so it doesn't block Cosmograph interactions. */}
      <HoverRing />

      <Cosmograph
        ref={cosmographRef}
        style={{ width: '100%', height: '100%' }}
        onMount={handleMount}

        // ── Data ──────────────────────────────────────────────────────────────
        points={cosmographPoints as unknown as Record<string, unknown>[]}
        pointIdBy="id"
        pointIndexBy="__idx"
        pointIncludeColumns={['__idx', 'id', 'label', 'kind', 'repo', 'community_id', 'pagerank', 'is_centroid', 'centroid_size', 'source_file', 'start_line', 'degree', '__cluster_id', '__cluster_strength', '__size', '__x', '__y']}

        links={visibleLinks as unknown as Record<string, unknown>[]}
        linkSourceBy="source"
        linkSourceIndexBy="__srcIdx"
        linkTargetBy="target"
        linkTargetIndexBy="__tgtIdx"
        linkIncludeColumns={['kind', '__crossRepo', '__linkState']}

        // ── Cluster layout (repo + community + module signal) ────────────────
        // #1356: pointXBy/pointYBy seed positions DROPPED — let the simulation
        // cluster naturally using the Silk Road params. Pre-seeded positions were
        // spreading repos too far apart (contributing to the empty-space problem).
        // Cluster force still applied via __cluster_id so repo islands form naturally.
        pointClusterBy="__cluster_id"
        pointClusterStrengthBy="__cluster_strength"

        // ── Node appearance ────────────────────────────────────────────────
        // Color mode routing — mutually exclusive props:
        //   degree mode:    pointColorStrategy (Cosmograph built-in gradient)
        //   repo mode:      pointColorBy='id'  + pointColorByFn
        //   community mode: pointColorBy='community_id' + pointColorByFn
        {...(isDegreeMode
          ? {
              pointColorStrategy: DEGREE_COLOR_STRATEGY as unknown as undefined,
            }
          : isCommunityMode
          ? {
              pointColorBy: 'community_id',
              pointColorByFn: pointColorByFnCommunity as (value: unknown) => string,
            }
          : {
              pointColorBy: 'id',
              pointColorByFn: pointColorByFnRepo as (value: unknown) => string,
            }
        )}

        // #1153: Silk Road size + scale params
        // #1127: pointSizeBy='__size' preserves Process node cap
        pointSizeBy="__size"
        // #1356: pointSizeScale=1.6 — slightly bigger base than Silk Road original.
        // Combined with pointSizeRange for wide dynamic range.
        pointSizeScale={1.6}
        pointSizeRange={[5, 80]}
        // #1356: scalePointsOnZoom=false — THE FIX. scalePointsOnZoom=true was
        // causing nodes to shrink to sub-pixel size at default zoom with spaceSize=8192,
        // making the graph appear as 3 invisible clumps in empty space.
        scalePointsOnZoom={false}
        pointLabelBy="label"

        // ── Labels ────────────────────────────────────────────────────────
        showLabels={true}
        showTopLabels={true}
        showTopLabelsLimit={currentBand.topLabels}
        showDynamicLabels={true}
        showDynamicLabelsLimit={40}
        showHoveredPointLabel={true}
        showFocusedPointLabel={true}
        pointLabelFontSize={11}
        pointLabelFn={truncateLabel as (value: unknown) => string}
        pointLabelClassName={labelPillStyle}

        // ── Edge appearance ────────────────────────────────────────────────
        linkColorBy="__linkState"
        linkColorByFn={linkColorByFn as (value: unknown) => string}
        linkWidthRange={highContrast ? [1, 3] : [0.5, 2]}
        // #1153: linkWidthScale=0.31752 (Silk Road param) — thinner edges overall,
        // making the Silk Road galaxy look less cluttered.
        linkWidthScale={0.31752}

        // ── Background ────────────────────────────────────────────────────
        backgroundColor={canvasBg}

        // ── Greyout opacity ────────────────────────────────────────────────
        // #1356: all bands are unfiltered, so greyout only applies when repo filter is active.
        pointGreyoutOpacity={(repoFilterActive || !!nodeFilterIndices) ? 0 : 0.15}
        linkGreyoutOpacity={repoFilterActive ? 0 : 0.1}

        // ── Simulation — #1153 Silk Road defaults, #1361 tunable via sidebar sliders ───
        enableSimulation={true}
        preservePointPositionsOnDataUpdate={true}
        // #1361: live-tunable via SimulationControls sidebar (falls back to Silk Road defaults)
        simulationLinkSpring={simCfg.linkSpring}
        simulationLinkDistance={simCfg.linkDistance}
        simulationGravity={simCfg.gravity}
        simulationFriction={simCfg.friction}
        // #1153: simulationDecay=1000 — faster alpha decay for quicker settle.
        simulationDecay={1000}
        // #1153: simulationCluster=0.1 — gentle cluster pull without over-collapse.
        simulationCluster={0.1}
        // Keep repulsion strong so repo islands stay separated (#1114)
        simulationRepulsion={1.5}
        simulationCenter={0.05}

        // ── Space + layout ─────────────────────────────────────────────────
        // #1361: spaceSize is tunable via sidebar (default 4096 — #1356 Silk Road fix)
        spaceSize={simCfg.spaceSize}
        // #1153: rescalePositions=true — Cosmograph normalizes initial positions
        // to fill the canvas; combines with pre-seeded __x/__y for fast convergence.
        rescalePositions={true}
        // #1153: fitViewOnInit=true + fitViewDelay=1000ms — smooth initial fit
        // so the whole graph is visible on load (no jarring jumps).
        fitViewOnInit={true}
        fitViewDelay={1000}

        // ── Selection / interaction ────────────────────────────────────────
        selectPointOnClick="single"
        focusPointOnClick={false}
        resetSelectionOnEmptyCanvasClick={false}

        // ── Simulation events ──────────────────────────────────────────────
        onSimulationEnd={handleSimulationEnd}

        // ── Events ────────────────────────────────────────────────────────
        onClick={handleClick}
        onBackgroundClick={handleBackgroundClick}
        onMouseMove={handleMouseMove}
        onZoom={handleZoom}

        statusIndicatorMode={false}
        disableLogging={true}
      />

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

      {/* #1108: Zoom band HUD — shows current band label for power users */}
      <ZoomBandHUD label={currentBand.label} isDark={isDark} />

      {/*
        #1108: Band transition fade — a thin overlay that briefly pulses opacity
        on zoom-band change to signal the LoD threshold crossing without being
        jarring. Uses a keyed div whose key changes on band label; the CSS
        animation fires on mount. pointer-events:none so it never blocks interaction.
      */}
      <BandTransitionFlash key={currentBand.label} isDark={isDark} />
    </div>
  )
}

// ---------------------------------------------------------------------------
// HoverRing — subtle cursor trail on the canvas (#1153 polish)
// ---------------------------------------------------------------------------

/**
 * A soft white ring that follows the cursor inside the canvas.
 * Implemented as a React component so it re-uses the parent's onMouseMove
 * without needing canvas access. The ring is always pointer-events:none
 * so it never blocks Cosmograph interactions.
 *
 * CSS custom property --hx / --hy are set via JS on mousemove.
 * The ring uses CSS transitions for smooth trailing behavior.
 */
const HoverRing = memo(function HoverRing() {
  const ringRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const parent = ringRef.current?.parentElement
    if (!parent) return

    let rafId: number | null = null

    const onMove = (e: MouseEvent) => {
      if (rafId !== null) return
      rafId = requestAnimationFrame(() => {
        rafId = null
        const rect = parent.getBoundingClientRect()
        const x = e.clientX - rect.left
        const y = e.clientY - rect.top
        if (ringRef.current) {
          ringRef.current.style.left = `${x}px`
          ringRef.current.style.top = `${y}px`
          ringRef.current.style.opacity = '1'
        }
      })
    }

    const onLeave = () => {
      if (ringRef.current) ringRef.current.style.opacity = '0'
    }

    parent.addEventListener('mousemove', onMove)
    parent.addEventListener('mouseleave', onLeave)
    return () => {
      parent.removeEventListener('mousemove', onMove)
      parent.removeEventListener('mouseleave', onLeave)
      if (rafId !== null) cancelAnimationFrame(rafId)
    }
  }, [])

  return (
    <div
      ref={ringRef}
      aria-hidden
      style={{
        position: 'absolute',
        width: 28,
        height: 28,
        marginLeft: -14,
        marginTop: -14,
        borderRadius: '50%',
        border: '1.5px solid rgba(226,232,240,0.35)',
        boxShadow: '0 0 6px rgba(148,163,184,0.2)',
        pointerEvents: 'none',
        opacity: 0,
        transition: 'left 60ms linear, top 60ms linear, opacity 200ms ease',
        zIndex: 10,
        transform: 'translateZ(0)',  // GPU layer hint
      }}
    />
  )
})

// ---------------------------------------------------------------------------
// BandTransitionFlash — brief fade pulse on zoom-band change (#1108)
// ---------------------------------------------------------------------------

/**
 * A full-canvas overlay that plays a very subtle 200ms fade-in/out animation
 * whenever the zoom band changes. The key prop on this component is set to
 * `currentBand.label` in the parent, so React remounts it on every band
 * transition — causing the CSS animation to replay.
 *
 * The flash is intentionally dim (max opacity 0.04) so it reads as a smooth
 * transition signal rather than a disruptive flash. pointer-events:none.
 */
const BandTransitionFlash = memo(function BandTransitionFlash({
  isDark,
}: {
  isDark: boolean
}) {
  return (
    <div
      aria-hidden
      data-testid="band-transition-flash"
      style={{
        position: 'absolute',
        inset: 0,
        pointerEvents: 'none',
        zIndex: 15,
        background: isDark ? 'rgba(148,163,184,1)' : 'rgba(51,65,85,1)',
        // keyframe: fade from 0.04 → 0 over 200ms (band transition feel)
        animation: 'lodBandFlash 200ms ease-out forwards',
      }}
    />
  )
})

// Inject the keyframe once into the document. Using a module-level var
// so we only inject once across all renders.
let _flashKeyframeInjected = false
if (typeof document !== 'undefined' && !_flashKeyframeInjected) {
  _flashKeyframeInjected = true
  const style = document.createElement('style')
  style.textContent = `
    @keyframes lodBandFlash {
      0%   { opacity: 0.04; }
      100% { opacity: 0; }
    }
  `
  document.head.appendChild(style)
}

export const GraphCanvas = memo(GraphCanvasInner)
