/* ============================================================
   components/graph/graph-canvas.tsx — the WebGL dependency graph canvas.

   GPU-accelerated force-directed graph via the MIT-licensed cosmos.gl
   engine (@cosmos.gl/graph — NOT @cosmograph, which is non-commercial).
   Reimplemented clean for WebUI v2, porting the hard-won lessons from the
   legacy dashboard GraphCanvas:

     • setPointColors / setLinkColors take RGB 0..1, NOT 0–255 — every
       channel > 1 clamps to white in the shader (writeNormalizedRGBA).
     • bounded square spaceSize + fit-camera-to-node-bbox so the settled
       graph FILLS the viewport.
     • cluster force → repo / community / module islands; bright sky
       cross-repo "bridge" edges.
     • layout-cache: settled positions persisted in localStorage → instant
       reload, the explode/settle animation is skipped.
     • ≤2s settle cap (wall-clock) + live knob.
     • click-to-focus N-hop ego-graph (hard-restricts the rendered set +
       re-fits the camera).
     • live tuning (sim / sizing / render) applied via setConfig without
       re-instantiating the engine.

   This component is imperative-by-design: it builds Float32 buffers itself
   and pushes them to the engine; it never recreates the Graph on data change.
   ============================================================ */

import {
  useRef,
  useEffect,
  useMemo,
  useCallback,
  useImperativeHandle,
  forwardRef,
  memo,
} from "react";
import { Graph } from "@cosmos.gl/graph";
import type { GraphNode, GraphEdge } from "@/data/types";
import {
  type RGBA,
  parseColor,
  writeNormalizedRGBA,
  readPastelScale,
  pastelAt,
  degreeColor,
  linkPalette,
  packedLinkAlpha,
  lerpRGBA,
  JARVIS_GLOW,
} from "@/lib/graph-colors";
import { saveLayout, loadLayout, isDegenerateLayout, isLayoutHealthy } from "@/lib/graph-layout-cache";
import { isRenderableGraph } from "@/lib/graph-render-guard";
import {
  autoBasePx,
  type ColorMode,
  type GroupByMode,
  type SimulationConfig,
  type NodeSizingConfig,
  type RenderConfig,
} from "@/store/use-graph-store";

// #1157: a module-stable empty set so an unset highlightedNodeIds prop never
// creates a new identity each render.
const EMPTY_HIGHLIGHT: ReadonlySet<string> = new Set();

// #4643 — FREEZE FIX. A replay/glow step can carry a huge result set (the
// reported repro matched 17,939 nodes). Glowing/dimming all of them in one
// synchronous pass on a 12.8k-node WebGL canvas blocks the main thread. We
// CAP the glow+dim set to the nodes actually RENDERED/in-view, never more than
// GLOW_CAP, and the recolor only ever rewrites these ≤GLOW_CAP indices per
// frame (the base buffers are written once, off the hot path). The same bounded
// set drives the dim-focus selection so both stay non-blocking.
const GLOW_CAP = 200;

const SPACE_SIZE = 32768;
// Fix #1532-3 / #1548-2: a small padding makes the settled graph FILL the
// canvas instead of floating small in the middle. A touch more than 0.04 so
// the outermost clusters/labels aren't clipped at the viewport edge.
const FIT_PADDING = 0.08;

// Fix #1562: a hard finite bound for any position we hand back to cosmos.gl.
// cosmos derives a spaceSize / bounds buffer from the point bbox; if a single
// coordinate is NaN/Infinity the bbox blows up and `new Float32Array(...)`
// throws "Array buffer allocation failed". We clamp every coordinate to a
// generous-but-finite range so the engine can never size a buffer from
// Infinity, regardless of how the simulation behaved.
const POS_CLAMP = SPACE_SIZE; // ±32768 — far beyond any sane settled layout

/**
 * Fix #1562: copy positions into a Float32Array, replacing any non-finite
 * coordinate (NaN / ±Infinity) with a clamped value and clamping every
 * coordinate to ±POS_CLAMP. Returns the sanitized array plus whether anything
 * had to be repaired (so callers can decide to skip a fit on garbage geometry).
 */
function sanitizePositions(
  positions: ArrayLike<number> | null | undefined,
): { array: Float32Array; repaired: boolean } {
  const len = positions?.length ?? 0;
  const out = new Float32Array(len);
  let repaired = false;
  for (let i = 0; i < len; i++) {
    let v = positions![i];
    if (!Number.isFinite(v)) {
      v = 0;
      repaired = true;
    } else if (v > POS_CLAMP) {
      v = POS_CLAMP;
      repaired = true;
    } else if (v < -POS_CLAMP) {
      v = -POS_CLAMP;
      repaired = true;
    }
    out[i] = v;
  }
  return { array: out, repaired };
}

// ── group / cluster helpers ──────────────────────────────────────────────────

function moduleKey(sourceFile: string): string {
  if (!sourceFile) return "";
  const parts = sourceFile.replace(/\\/g, "/").split("/");
  return parts.slice(0, -1).slice(-2).join("/");
}

function groupKeyFor(n: GraphNode, mode: GroupByMode): string {
  switch (mode) {
    case "repo":
      return n.repo ?? "";
    case "community":
      return `c:${n.communityId ?? -1}`;
    case "module":
      return moduleKey(n.sourceFile) || `repo:${n.repo ?? ""}`;
    default:
      return "";
  }
}

/**
 * Fix #1562: a STABLE STRING key for the cluster a node belongs to (or undefined
 * for "none"). This is intentionally NOT a numeric id: cosmos.gl treats the
 * numeric values passed to `setPointClusters` as DENSE indices and allocates a
 * cluster FBO of size `ceil(sqrt(maxClusterId + 1))²`. The previous code packed
 * sparse hash ids up to ~repoIdx*10_000_000 (≈1.9e8 on a ~20-repo group), so
 * cosmos tried to allocate a ~13784×13784×4-float texture (multiple GB) →
 * "RangeError: Array buffer allocation failed". Returning a key here lets the
 * caller remap to contiguous 0-based indices, so the cluster texture is sized to
 * the ACTUAL number of clusters (tens), not the magnitude of a hash. (This,
 * not a NaN/position divergence, is the true cause of #1562: 232 nodes had a
 * smaller repoIdx range and stayed under the allocation limit; 1316 nodes across
 * ~20 repos pushed maxClusterId — and the texture — past what could be alloc'd.)
 */
function clusterKeyFor(n: GraphNode, repoIdx: number, mode: GroupByMode): string | undefined {
  if (mode === "none") return undefined;
  if (mode === "repo") {
    // Sub-cluster a repo by module + community so a big repo still reads as
    // several islands — but as a string key, remapped to a dense index later.
    const mod = moduleKey(n.sourceFile);
    const cid = n.communityId ?? 0;
    return `r:${repoIdx}|c:${cid}|m:${mod}`;
  }
  return groupKeyFor(n, mode);
}

// Fix #1566: a tiny deterministic hash → [0,1) so cluster seed offsets are
// stable across re-packs (no jitter on every render) without storing state.
function hash01(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  // map to [0,1)
  return ((h >>> 0) % 100000) / 100000;
}

function buildGroupCenters(
  nodes: GraphNode[],
  mode: GroupByMode,
): Map<string, { x: number; y: number }> {
  if (mode === "none") return new Map();
  const keys = Array.from(new Set(nodes.map((n) => groupKeyFor(n, mode)))).sort();
  const N = keys.length;
  if (N === 0) return new Map();
  // Fix #1566: the REAL cause of the hollow ring. Every prior fix kept seeding
  // cluster centroids on a CIRCLE (R*cos/sin(angle)); cosmos's cluster force
  // then pulled each cluster back toward its ring slot, so connected modules
  // could never migrate next to each other — the middle stayed empty no matter
  // how strong the link-spring was. We now DROP the radial ring entirely and
  // seed every cluster centroid in a SMALL random blob near the center (stable
  // hash offsets). With the cluster force softened to color-only strength
  // (see simulationCluster + clusterStrength), the link-spring + center gravity
  // now DOMINATE placement: connected clusters pull adjacent and fill the
  // middle, while same-group nodes still read together by color.
  const SEED_R = Math.min(700, Math.max(260, Math.sqrt(nodes.length) * 14));
  return new Map(
    keys.map((key) => {
      // Two independent hashes → an angle + radius inside a centered disc.
      const a = hash01(key) * 2 * Math.PI;
      const r = Math.sqrt(hash01(key + "·r")) * SEED_R;
      return [key, { x: r * Math.cos(a), y: r * Math.sin(a) }];
    }),
  );
}

// ── component ────────────────────────────────────────────────────────────────

export interface GraphCanvasProps {
  group: string;
  nodes: GraphNode[];
  edges: GraphEdge[];
  selectedNodeId: string | null;
  hoveredNodeId: string | null;
  isDark: boolean;
  colorMode: ColorMode;
  groupBy: GroupByMode;
  simulation: SimulationConfig;
  nodeSizing: NodeSizingConfig;
  render: RenderConfig;
  /** repo filter — null = all. */
  activeRepos: Set<string> | null;
  /** community focus — dims non-members (null = none). */
  focusedCommunityId: number | null;
  /**
   * Fix #1548-3: true when the parent is rendering an ego SUB-graph (nodes/edges
   * already pre-filtered to the ≤5-hop neighborhood). The canvas re-layouts +
   * fits this smaller set so it fills the viewport.
   */
  isFocusView: boolean;
  /** changes to this nonce force a fresh re-layout (skip cache). */
  relayoutNonce: number;
  onNodeClick: (node: GraphNode | null) => void;
  onNodeHover: (node: GraphNode | null) => void;
  onSettled: () => void;
  /**
   * #1157 Jarvis: node IDs the most recent MCP tool call touched. The canvas
   * runs a transient GLOW/PULSE on these nodes (and any edge whose both
   * endpoints are in the set) that decays to nothing — a real-time view of the
   * agent working through the graph. Empty set = no glow.
   */
  highlightedNodeIds?: ReadonlySet<string>;
  /**
   * #1157 Jarvis: monotonic counter bumped on every fresh highlight. A change
   * (re)starts the glow rAF loop from full intensity, so back-to-back MCP
   * queries each re-pulse even if the node set overlaps.
   */
  highlightEpoch?: number;
  /**
   * #4643 — reports how many nodes were actually glowed vs how many the step
   * matched, after the in-view/GLOW_CAP cap. Lets the panel render
   * "glowing N of M" when a large result set is sampled. `shown === matched`
   * means nothing was capped.
   */
  onGlowCap?: (shown: number, matched: number) => void;
  className?: string;
}

// Fix #1564-3: labels are HOVER-ONLY. The old always-on zoom/hub-gated label
// layer cluttered the canvas; now we show a label ONLY for the hovered node and
// its direct neighbors while hovered, so the canvas stays clean. The hover
// label still tracks smoothly during pan/zoom via the existing rAF loop.
const truncate = (s: string) => (s.length > 30 ? s.slice(0, 28) + "…" : s);

/**
 * Imperative handle (Fix #1548-3): the parent snapshots the camera (zoom + pan)
 * on focus-enter and restores it on focus-exit, so the user returns to exactly
 * the view they left.
 */
export interface GraphCanvasHandle {
  snapshotCamera: () => void;
  restoreCamera: () => void;
  /**
   * #1932: resolve a node id to its CURRENT screen (px) position relative to
   * the canvas container. Returns null when the id is unknown or the engine
   * isn't ready yet. Used by the JARVIS SVG overlay to draw chevrons + the
   * MCP-replay comet without re-implementing the cosmos.gl camera math.
   */
  getNodeScreenPosition: (id: string) => { x: number; y: number } | null;
  /**
   * #1932: list every edge in the current graph as `{src,tgt,bridge}` where
   * `bridge` is true when the edge crosses two repos (the dashed + distinct-
   * accent tier). The overlay renders chevrons + bridge styling from this.
   */
  getEdgeList: () => ReadonlyArray<{ src: string; tgt: string; bridge: boolean }>;
  /** #1932: true when the named edge crosses repos (bridge). */
  isBridgeEdge: (src: string, tgt: string) => boolean;
}

function GraphCanvasInner(
  {
    group,
    nodes,
    edges,
    selectedNodeId,
    hoveredNodeId,
    isDark,
    colorMode,
    groupBy,
    simulation,
    nodeSizing,
    render,
    activeRepos,
    focusedCommunityId,
    isFocusView,
    relayoutNonce,
    onNodeClick,
    onNodeHover,
    onSettled,
    highlightedNodeIds,
    highlightEpoch = 0,
    onGlowCap,
    className = "",
  }: GraphCanvasProps,
  ref: React.Ref<GraphCanvasHandle>,
) {
  const containerRef = useRef<HTMLDivElement>(null);
  const graphRef = useRef<Graph | null>(null);
  const hasSettledRef = useRef(false);
  const capTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Fix #1558-2: timer for the mid-settle re-heat (see mount effect) + a flag so
  // the first early onSimulationEnd doesn't freeze a still-spread hollow ring
  // before the re-heat has had a chance to finish converging.
  const reheatTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reheatedRef = useRef(false);
  const didAutoStartRef = useRef(false);
  const mountTimeRef = useRef(Date.now());

  // Live refs so the mount-only handlers read current values.
  const nodesRef = useRef(nodes);
  nodesRef.current = nodes;
  const selectedRef = useRef<string | null>(selectedNodeId);
  selectedRef.current = selectedNodeId;
  const onNodeClickRef = useRef(onNodeClick);
  onNodeClickRef.current = onNodeClick;
  const onNodeHoverRef = useRef(onNodeHover);
  onNodeHoverRef.current = onNodeHover;
  const onSettledRef = useRef(onSettled);
  onSettledRef.current = onSettled;
  const simRef = useRef(simulation);
  simRef.current = simulation;
  // Fix #1607: live render config in a ref so the mount-only zoom handler reads
  // the current pointSizeScale / maxPointSize when computing the zoom response.
  const renderRef = useRef(render);
  renderRef.current = render;
  const hoveredRef = useRef<string | null>(hoveredNodeId);
  hoveredRef.current = hoveredNodeId;
  const relayoutRef = useRef(relayoutNonce);

  const idToIdx = useMemo(() => {
    const m = new Map<string, number>();
    nodes.forEach((n, i) => m.set(n.id, i));
    return m;
  }, [nodes]);

  const nodeIds = useMemo(() => nodes.map((n) => n.id), [nodes]);

  // #4605 — can this node set be fed to the WebGL engine at all? An EMPTY graph
  // (0 points) makes cosmos size its point/cluster textures as 0×0, and regl
  // throws `(regl) invalid texture shape` from `clusterTexture`/`by.create`,
  // tripping the app error boundary. This happens on a deep-link to a SYNTHETIC /
  // unresolved `?node=<id>` whose ego filter yields no nodes. When false we skip
  // every engine data-push / `create()` and render a graceful empty-state below.
  const renderable = useMemo(() => isRenderableGraph(nodes.length, edges.length), [nodes.length, edges.length]);
  const renderableRef = useRef(renderable);
  renderableRef.current = renderable;

  const repoToIdx = useMemo(() => {
    const repos = Array.from(new Set(nodes.map((n) => n.repo ?? ""))).sort();
    return new Map(repos.map((r, i) => [r, i]));
  }, [nodes]);

  // Fix #1567-1: a group is MULTI-REPO when it spans 2+ distinct repos (e.g.
  // upvate = 3 islands). In that case the cross-REPO island↔island links are the
  // structure the user wants to trace, so they become the EMPHASIZED tier and the
  // cross-MODULE-within-a-repo links drop to SUBTLE. In a single-repo monorepo
  // there are no cross-repo edges, so cross-MODULE stays the emphasized tier
  // (the #1569 behavior). Detected from the actual repo count.
  const isMultiRepo = useMemo(() => repoToIdx.size >= 2, [repoToIdx]);

  const groupCenters = useMemo(() => buildGroupCenters(nodes, groupBy), [nodes, groupBy]);

  // Stable 1-based module color index (alphabetical) for the "module" color
  // mode — gives a monorepo's packages distinct colors. (Fix #1532-1)
  const moduleToIdx = useMemo(() => {
    const mods = Array.from(new Set(nodes.map((n) => moduleKey(n.sourceFile)))).sort();
    return new Map(mods.map((m, i) => [m, i]));
  }, [nodes]);

  // Degree percentile fn (so the degree gradient spreads across the long tail).
  const sortedDegrees = useMemo(
    () => nodes.map((n) => n.degree ?? 0).sort((a, b) => a - b),
    [nodes],
  );
  const degreePercentile = useCallback(
    (d: number): number => {
      const arr = sortedDegrees;
      if (arr.length === 0) return 0;
      let lo = 0;
      let hi = arr.length;
      while (lo < hi) {
        const mid = (lo + hi) >> 1;
        if (arr[mid] <= d) lo = mid + 1;
        else hi = mid;
      }
      return lo / arr.length;
    },
    [sortedDegrees],
  );

  // #4467 — RENDERED degree + a single "primary neighbor" per node, derived from
  // the CURRENT edge set (after the edge-kind filter). Rendered degree drives the
  // low-degree de-emphasis (opacity) honestly w.r.t. the user's edge-kind toggles;
  // primaryNeighbor lets us SEED a degree-1 node next to its one neighbor so the
  // force layout settles it adjacent instead of flinging it to the orphan rim.
  const renderedDegree = useMemo(() => {
    const deg = new Map<string, number>();
    const primary = new Map<string, string>();
    for (const e of edges) {
      deg.set(e.source, (deg.get(e.source) ?? 0) + 1);
      deg.set(e.target, (deg.get(e.target) ?? 0) + 1);
      // First-seen neighbor is the "primary" anchor for a would-be leaf.
      if (!primary.has(e.source)) primary.set(e.source, e.target);
      if (!primary.has(e.target)) primary.set(e.target, e.source);
    }
    return { deg, primary };
  }, [edges]);

  // ── packed buffers ──────────────────────────────────────────────────────────
  const packed = useMemo(() => {
    const count = nodes.length;
    const positions = new Float32Array(count * 2);
    const sizes = new Float32Array(count);
    const clusters: (number | undefined)[] = new Array(count);
    const clusterStrength = new Float32Array(count);

    let maxPR = 0;
    for (const n of nodes) if ((n.pageRank ?? 0) > maxPR) maxPR = n.pageRank ?? 0;
    if (maxPR === 0) maxPR = 1;

    // Fix #1607: the per-node SIZE buffer is now authored directly in SCREEN
    // PIXELS at neutral zoom (pointSizeScale is 1.0). The base pixel size is
    // computed AUTOMATICALLY from the node count (autoBasePx: big graph → small
    // dots, small graph → bigger discs) so the defaults look right on both 19k and
    // 1.3k graphs with no tuning. The user baseSize knob is a MULTIPLIER around
    // that auto base (1.0 = auto). The zoom RESPONSE (sublinear growth on zoom-in +
    // a px cap) is applied separately by the zoom-driven pointSizeScale updater, so
    // these are the base px at zoom = 1.
    const autoBase = autoBasePx(count);
    const effectiveBase = Math.max(0.5, autoBase * nodeSizing.baseSize);

    const grouping = groupBy !== "none";
    const groupCount = new Map<string, number>();
    for (const n of nodes) {
      const k = groupKeyFor(n, groupBy);
      groupCount.set(k, (groupCount.get(k) ?? 0) + 1);
    }

    // Fix #1562: remap cluster KEYS to DENSE, contiguous 0-based indices. cosmos.gl
    // sizes the cluster force texture as ceil(sqrt(maxIndex+1))² — so the indices
    // must be small and packed, never a sparse hash. This keeps the texture sized
    // to the real cluster count (tens) instead of a hash magnitude (hundreds of
    // millions), which is what blew the Float32Array allocation.
    const clusterKeyToIdx = new Map<string, number>();

    nodes.forEach((n, i) => {
      const repoIdx = repoToIdx.get(n.repo ?? "") ?? 0;
      const ckey = clusterKeyFor(n, repoIdx, groupBy);
      let cidx: number | undefined;
      if (ckey !== undefined) {
        cidx = clusterKeyToIdx.get(ckey);
        if (cidx === undefined) {
          cidx = clusterKeyToIdx.size;
          clusterKeyToIdx.set(ckey, cidx);
        }
      }
      clusters[i] = cidx;
      const normPR = (n.pageRank ?? 0) / maxPR;
      // Fix #1566: keep clustering for COLOR/cohesion only — drop the per-node
      // cluster pull to a WEAK nudge so it merely keeps same-group nodes loosely
      // together, while the link-spring + center gravity own the macro layout.
      // A strong cluster pull (0.22+) is what re-formed the ring by yanking each
      // group back toward its seeded centroid; at this strength connectivity
      // wins and connected groups migrate adjacent. (was 0.22 + normPR*0.15)
      clusterStrength[i] = grouping ? 0.06 + normPR * 0.06 : 0.03 + normPR * 0.04;

      // Fix #1607: gentle log-scaled degree boost in MULTIPLES of the base px,
      // hard-capped at maxMultiplier × base so a high-degree hub is at most ~2.2×
      // a regular node — slightly larger, never a blob. degreeScale is a small
      // unitless factor on log10(degree+1).
      const degBoost = 1 + Math.log10((n.degree ?? 0) + 1) * nodeSizing.degreeScale;
      const mult = Math.min(degBoost, nodeSizing.maxMultiplier);
      sizes[i] = effectiveBase * mult;

      const gkey = groupKeyFor(n, groupBy);
      const center = grouping ? groupCenters.get(gkey) : undefined;
      // Fix #1558-2: seed nodes TIGHTLY around their group center (and seed the
      // ungrouped fallback near the middle, not across a 4000-wide field). Within
      // the ≤2s settle cap the strong center/gravity + link-spring then pull the
      // already-compact start into a canvas-filling mass — no hollow ring.
      const jitterR = Math.max(280, Math.sqrt(groupCount.get(gkey) ?? 1) * 26);
      const angle = Math.random() * 2 * Math.PI;
      const r = Math.random() * jitterR;
      positions[i * 2] = center ? center.x + r * Math.cos(angle) : (Math.random() - 0.5) * 1600;
      positions[i * 2 + 1] = center
        ? center.y + r * Math.sin(angle)
        : (Math.random() - 0.5) * 1600;
    });

    // #4467 — ANCHOR low-degree leaves next to their single neighbor. After the
    // group-center seeding above, re-seed any node with rendered degree ≤ 1 right
    // on top of its primary neighbor's seed (plus a tiny jitter). The strong
    // link-spring then keeps that leaf tucked beside its parent instead of letting
    // global repulsion fling it to the periphery — which is what made degree-1
    // DTO/type/config nodes read as a misleading "orphan ring." A true zero-degree
    // node (no primary neighbor) is left at its group-center seed. Runs in a second
    // pass so every potential anchor already has a base position.
    for (let i = 0; i < count; i++) {
      const n = nodes[i];
      if ((renderedDegree.deg.get(n.id) ?? 0) > 1) continue;
      const nbId = renderedDegree.primary.get(n.id);
      if (nbId === undefined) continue; // true orphan — keep its group-center seed
      const j = idToIdx.get(nbId);
      if (j === undefined) continue;
      // Tight jitter so coincident leaves don't fully overlap, but stay glued to
      // the neighbor (well inside the link-spring's rest distance).
      const a = Math.random() * 2 * Math.PI;
      const r = Math.random() * 24;
      positions[i * 2] = positions[j * 2] + r * Math.cos(a);
      positions[i * 2 + 1] = positions[j * 2 + 1] + r * Math.sin(a);
    }

    // Fix #1607: the largest base-px size in the buffer (a hub). The zoom-driven
    // size updater uses this to set pointSizeScale so the LARGEST node is exactly
    // capped at render.maxPointSize px when zoomed in — preventing any blob.
    let maxSizePx = 0;
    for (let i = 0; i < count; i++) if (sizes[i] > maxSizePx) maxSizePx = sizes[i];
    if (maxSizePx <= 0) maxSizePx = 1;

    return { positions, sizes, clusters, clusterStrength, maxSizePx };
  }, [nodes, repoToIdx, groupCenters, groupBy, nodeSizing, renderedDegree, idToIdx]);

  // Node colors — re-read pastel scale from tokens.css so theme flows through.
  // #4467 — ALSO encode rendered degree into per-node OPACITY: degree-1 leaves
  // (DTO members, types, config) fade back so they recede instead of dominating
  // the view as a bright "orphan ring," while well-connected structure stays at
  // full opacity. The size buffer already boosts hubs (degBoost); opacity is the
  // complementary lever that DIMS the low end. Uses the rendered degree (after the
  // edge-kind filter) so it's consistent with the min-degree filter + badge.
  const packPointColors = useCallback((): Float32Array => {
    const { fill } = readPastelScale();
    const count = nodes.length;
    const out = new Float32Array(count * 4);
    for (let i = 0; i < count; i++) {
      const n = nodes[i];
      let rgba: RGBA;
      if (colorMode === "degree") {
        const t = Math.pow(degreePercentile(n.degree ?? 0), 0.7);
        rgba = degreeColor(t);
      } else if (colorMode === "community") {
        rgba = pastelAt(fill, (n.communityId ?? 0) + 1);
      } else if (colorMode === "module") {
        rgba = pastelAt(fill, (moduleToIdx.get(moduleKey(n.sourceFile)) ?? 0) + 1);
      } else {
        rgba = pastelAt(fill, (repoToIdx.get(n.repo ?? "") ?? 0) + 1);
      }
      // #4467 — degree-based opacity, clamped to a gentle band so low-degree nodes
      // recede but never vanish: deg 0 → 0.45×, deg 1 → ~0.6×, deg ≥ 6 → 1.0×.
      // Multiplies the palette alpha (the global pointOpacity is applied on top by
      // cosmos), so coloring is unchanged — only the perceived emphasis shifts.
      const rdeg = renderedDegree.deg.get(n.id) ?? 0;
      const degAlpha = Math.min(1, 0.45 + 0.55 * (Math.log10(rdeg + 1) / Math.log10(7)));
      writeNormalizedRGBA(out, i, [rgba[0], rgba[1], rgba[2], rgba[3] * degAlpha]);
    }
    return out;
  }, [nodes, colorMode, repoToIdx, moduleToIdx, degreePercentile, renderedDegree]);

  // Links — packed [src,tgt] + an edge CLASS per link (#1564-2):
  //   2 = cross-repo (bridge), 1 = cross-module (same repo, diff module),
  //   0 = intra-module. The class drives both color (theme-aware, #1564-1) and
  //   width/opacity so the inter-module/inter-repo wiring visually stands out
  //   while intra-module edges fade back. We determine cross-vs-intra from the
  //   repo + module of each edge's two endpoints.
  const linkData = useMemo(() => {
    const idToRepo = new Map(nodes.map((n) => [n.id, n.repo ?? ""]));
    const idToModule = new Map(nodes.map((n) => [n.id, moduleKey(n.sourceFile)]));
    const src: number[] = [];
    const tgt: number[] = [];
    const states: number[] = []; // 2 cross-repo, 1 cross-module, 0 intra-module
    for (const e of edges) {
      const s = idToIdx.get(e.source);
      const t = idToIdx.get(e.target);
      if (s === undefined || t === undefined) continue;
      const crossRepo = idToRepo.get(e.source) !== idToRepo.get(e.target);
      const crossModule = idToModule.get(e.source) !== idToModule.get(e.target);
      src.push(s);
      tgt.push(t);
      states.push(crossRepo ? 2 : crossModule ? 1 : 0);
    }
    const links = new Float32Array(src.length * 2);
    for (let i = 0; i < src.length; i++) {
      links[i * 2] = src[i];
      links[i * 2 + 1] = tgt[i];
    }
    return { links, states };
  }, [nodes, edges, idToIdx]);

  const packLinkColors = useCallback((): Float32Array => {
    const { states } = linkData;
    const out = new Float32Array(states.length * 4);
    // Fix #1564-1: theme-aware palette (lighter on dark, darker on light), with
    // a distinct bright bridge color in BOTH themes. Re-packed on theme change
    // (isDark is a dep), so the dark/light toggle flows through live.
    const pal = linkPalette(isDark);
    // Fix #1567-1: make the emphasis tier REPO-AWARE. We compute three opacity
    // tiers — faded / subtle / emphasized — then map each edge STATE onto a tier
    // depending on whether the group is multi-repo:
    //   • multi-repo:  cross-REPO (st 2) = emphasized, cross-MODULE (st 1) =
    //                  subtle, intra-module (st 0) = faded. So in upvate the
    //                  island↔island bridges light up, NOT the in-repo wiring.
    //   • single-repo: no st-2 edges; cross-MODULE (st 1) = emphasized,
    //                  intra-module (st 0) = faded (the #1569 behavior).
    //
    // Fix #1599: the #1566/#1567 emphasis was tuned with ZERO real cross-repo
    // edges present, so the "make cross-repo pop" path was never exercised against
    // live data. Now upvate serves 376 cross-repo edges out of 37k — but the old
    // tier gaps were so tight (faded≈0.36 / subtle≈0.51 / emph≈0.63) that those
    // 376 bridges were lost in the 37k-edge mesh. Because the cross-repo bridges
    // are RARE (376 of 37k), they can safely be near-opaque + bright WITHOUT
    // becoming spaghetti — there simply aren't enough of them to dominate. So when
    // the group is MULTI-REPO we open the gap hard: the emphasized (cross-repo)
    // tier goes near-full opacity while intra-repo (cross-module + intra-module)
    // edges fade well back, so the inter-cluster connections visibly stand out.
    // For a single-repo monorepo the cross-MODULE tier is the rare/structural one,
    // so it gets the emphasized treatment but with a gentler gap (cross-module is
    // far more common than cross-repo, so over-brightening it would re-introduce
    // spaghetti).
    // Fix #4852: tier alphas + the combined-alpha floor now live in one pure,
    // unit-tested place (graph-colors.packedLinkAlpha). `packedLinkAlpha` returns
    // the value to bake into color.a such that, AFTER the shader multiplies by the
    // linkOpacity uniform, the rendered alpha is max(tierAlpha × linkOpacity,
    // LINK_ALPHA_FLOOR) — so the doubled linkOpacity multiply (tier already scaled
    // by linkOpacity, then × the uniform again) can no longer compound a faded
    // link to invisibility at low slider settings. The faded/subtle/emphasized
    // RELATIONSHIPS (the #1567/#1599 emphasis tuning) are preserved.
    const base = render.linkOpacity;
    const fadedA = packedLinkAlpha("faded", base, isMultiRepo);
    const subtleA = packedLinkAlpha("subtle", base, isMultiRepo);
    const emphA = packedLinkAlpha("emphasized", base, isMultiRepo);
    for (let i = 0; i < states.length; i++) {
      const st = states[i];
      // tier: 0 faded, 1 subtle, 2 emphasized — repo-aware.
      let tier: 0 | 1 | 2;
      let c: RGBA;
      if (st === 2) {
        // Cross-repo edges now blend in as ordinary links (user ask): drop the
        // distinct bright-sky bridge COLOR and the emphasized opacity tier so a
        // cross-repo link is visually identical to a normal cross-module link.
        // (Width already unifies st2 with st1 — see #2108 in packLinkWidths.)
        tier = isMultiRepo ? 1 : 2;
        c = pal.crossModule;
      } else if (st === 1) {
        // cross-module — emphasized in a single-repo monorepo, subtle when
        // cross-repo links exist (so they don't compete with the bridges).
        tier = isMultiRepo ? 1 : 2;
        c = pal.crossModule;
      } else {
        tier = 0;
        c = pal.intraModule;
      }
      const a = tier === 2 ? emphA : tier === 1 ? subtleA : fadedA;
      const rgba: RGBA = [c[0], c[1], c[2], a];
      writeNormalizedRGBA(out, i, rgba);
    }
    return out;
  }, [linkData, render.linkOpacity, isDark, isMultiRepo]);

  const packLinkWidths = useCallback((): Float32Array => {
    const { states } = linkData;
    const out = new Float32Array(states.length);
    if (!render.showLinks) return out;
    // Fix #1558-1: links stay visible at EVERY zoom level (constant on-screen
    // pixel width, scaleLinksOnZoom off). Fix #1566: keep cross widths NEAR the
    // intra width — only a hair thicker — so they're traceable, not spaghetti.
    // Fix #1567-1: width follows the same REPO-AWARE tier as color. The
    // emphasized tier (cross-repo on multi-repo groups; cross-module on a single
    // repo) gets the thickest hair; the subtle tier sits between; intra is
    // thinnest. So the island bridges read as the distinct tier, not the
    // in-repo wiring.
    // Fix #1599: on a MULTI-REPO group the rare cross-repo bridges get a
    // distinctly thicker hair so they read as the structural tier.
    //
    // Fix #2108: GRAPH VIEW — remove the cross-repo width override so all link
    // tiers use uniform widths derived purely from the cross-module / intra-module
    // split. The cross-repo emphasis (thicker + distinct accent) is preserved on
    // the flows / paths / topology surfaces (different components). In the graph
    // view the dense WebGL canvas makes thick cross-repo ropes visually dominant
    // even when the count is small; uniform widths keep the hairline aesthetic.
    const W_FADED = 0.8;
    const W_SUBTLE = 1.0;
    const W_EMPH = 1.3;
    const FLOOR_FADED = 0.8;
    const FLOOR_SUBTLE = 1.0;
    const FLOOR_EMPH = 1.2;

    // Fix #2110: ZOOM-COMPENSATED link width. At very high zoom (close-up, 5-10
    // nodes visible) links should thin out to fine connecting threads so they
    // don't dominate the view. At very low zoom (full-canvas density cloud) links
    // should stay at full user-scale so they're visible against the dense field.
    //
    //   zoom_compensation(z):
    //     z < 0.3         → 1.0   (full scale, zoomed out / dense cloud)
    //     0.3 … 1.5       → smooth taper 1.0 → 0.5  (mid-range)
    //     z > 1.5         → 0.4   (thin threads; nodes dominate at close-up)
    //
    // The existing Grow-nodes-on-zoom toggle (applyZoomSizing) reuses the live
    // zoom via currentZoomRef — we read the same ref here.
    const z = currentZoomRef.current;
    let zoomComp: number;
    if (z < 0.3) {
      zoomComp = 1.0;
    } else if (z <= 1.5) {
      // Linear taper from 1.0 at z=0.3 to 0.5 at z=1.5
      zoomComp = 1.0 - 0.5 * ((z - 0.3) / 1.2);
    } else {
      zoomComp = 0.4;
    }

    for (let i = 0; i < states.length; i++) {
      const st = states[i];
      // Fix #2108: treat cross-repo (st 2) the same tier as cross-module (st 1).
      // Width only distinguishes intra-module (thin) vs cross-module/repo (slightly
      // thicker). Colour still distinguishes the full three tiers via packLinkColors.
      const tier: 0 | 1 | 2 = st === 0 ? 0 : st === 1 ? (isMultiRepo ? 1 : 2) : (isMultiRepo ? 1 : 2);
      const base = tier === 2 ? W_EMPH : tier === 1 ? W_SUBTLE : W_FADED;
      const floor = tier === 2 ? FLOOR_EMPH : tier === 1 ? FLOOR_SUBTLE : FLOOR_FADED;
      // Fix #2110: apply zoom compensation on top of user scale. The floor is
      // intentionally NOT compensated so links never fully disappear at any zoom.
      out[i] = Math.max(floor, base * render.linkWidthScale * zoomComp);
    }
    return out;
  }, [linkData, render.showLinks, render.linkWidthScale, isMultiRepo]);

  // Fix #1564-3: index-level adjacency so a hovered node can also label its
  // DIRECT neighbors (built from the same packed link buffer). Bidirectional.
  const neighborIdx = useMemo(() => {
    const m = new Map<number, Set<number>>();
    const { links } = linkData;
    for (let i = 0; i < links.length; i += 2) {
      const a = links[i];
      const b = links[i + 1];
      if (!m.has(a)) m.set(a, new Set());
      if (!m.has(b)) m.set(b, new Set());
      m.get(a)!.add(b);
      m.get(b)!.add(a);
    }
    return m;
  }, [linkData]);
  const labelLayerRef = useRef<HTMLDivElement>(null);

  const escapeLabel = (s: string) =>
    truncate(s).replace(/&/g, "&amp;").replace(/</g, "&lt;");

  const labelSpan = (sx: number, sy: number, text: string, strong: boolean) =>
    `<span style="position:absolute;left:${sx}px;top:${
      sy - 14
    }px;transform:translate(-50%,-100%);white-space:nowrap;font-size:${
      strong ? 12 : 11
    }px;font-weight:${strong ? 600 : 500};padding:1px 5px;border-radius:4px;background:${
      isDark ? "rgba(2,6,23,0.72)" : "rgba(248,250,252,0.9)"
    };color:${isDark ? "#e2e8f0" : "#1e293b"}${
      strong ? `;outline:1px solid ${isDark ? "#38bdf8" : "#0284c7"}` : ""
    }">${escapeLabel(text)}</span>`;

  // Fix #1564-3: HOVER-ONLY labels. Render a label only for the hovered node
  // (drawn strong) plus its direct neighbors (drawn quiet) while hovering;
  // nothing otherwise → a clean canvas. Still re-projects from the live camera
  // so the label tracks the node during pan/zoom (the rAF loop calls this).
  const refreshLabels = useCallback(() => {
    const g = graphRef.current;
    const layer = labelLayerRef.current;
    if (!g || !layer) return;
    const hovId = hoveredRef.current;
    const hovIdx = hovId != null ? idToIdx.get(hovId) : undefined;
    if (hovIdx === undefined) {
      // Not hovering → clear the layer (clean canvas).
      if (layer.innerHTML !== "") layer.innerHTML = "";
      return;
    }
    const positions = g.getPointPositions();
    if (!positions || positions.length === 0) return;
    const w = containerRef.current?.clientWidth ?? 0;
    const h = containerRef.current?.clientHeight ?? 0;

    // The hovered node, then its direct neighbors (quiet) so the local
    // structure is readable without cluttering the whole canvas.
    const shown: { idx: number; strong: boolean }[] = [{ idx: hovIdx, strong: true }];
    for (const nb of neighborIdx.get(hovIdx) ?? []) shown.push({ idx: nb, strong: false });

    const frag: string[] = [];
    for (const { idx, strong } of shown) {
      const n = nodesRef.current[idx];
      if (!n) continue;
      const px = positions[idx * 2];
      const py = positions[idx * 2 + 1];
      if (px === undefined || py === undefined) continue;
      const [sx, sy] = g.spaceToScreenPosition([px, py]);
      if (sx < -50 || sy < -50 || sx > w + 50 || sy > h + 50) continue;
      frag.push(labelSpan(sx, sy, n.label, strong));
    }
    layer.innerHTML = frag.join("");
  }, [neighborIdx, idToIdx, isDark]);

  // Always invoke the LATEST refreshLabels via a ref: the mount-only engine
  // handlers (onZoom / onSimulationTick) and a stable scheduleLabels would
  // otherwise capture the FIRST-render closure, whose node list was still empty
  // — so labels never rendered. (Fix #1532-5)
  const refreshLabelsRef = useRef(refreshLabels);
  refreshLabelsRef.current = refreshLabels;

  // Schedule a debounced label refresh on a short timeout. We deliberately do
  // NOT use requestAnimationFrame: once the engine is paused (settled) it stops
  // painting, so rAF gets starved and never fires — labels would never appear.
  // A stable single-timer (clear-then-set) guarantees the refresh runs and is
  // coalesced across the many tick/zoom events. (Fix #1532-5)
  const labelTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const scheduleLabels = useCallback(() => {
    if (labelTimerRef.current !== null) clearTimeout(labelTimerRef.current);
    labelTimerRef.current = setTimeout(() => {
      labelTimerRef.current = null;
      refreshLabelsRef.current();
    }, 48);
  }, []);
  // scheduleLabels is stable (no deps), so engine handlers can use it directly.
  const scheduleLabelsLive = scheduleLabels;

  // ── Fix #1548-1: per-frame label tracking during pan / zoom ──────────────────
  // The settled engine stops painting, so onSimulationTick never fires while the
  // user pans — the old coalesced setTimeout meant labels FROZE during drag and
  // only snapped on mouse-release. Instead, while the user is interacting we run
  // our own rAF loop and re-project the (capped) label set from the cosmos.gl
  // camera every frame, so labels track the nodes smoothly with no lag.
  const interactingRef = useRef(false);
  // #4654: STICKY "the user has manually touched the graph" flag. Unlike
  // interactingRef (which flips back to false on interaction end), this latches
  // true on the first pan / zoom / drag / select and never resets. The auto-settle
  // verify-then-retry controller checks it so it only ever auto-corrects the
  // INITIAL collapsed render — once the user takes control we never re-fire.
  const userInteractedRef = useRef(false);
  const rafRef = useRef<number | null>(null);
  const motionLoop = useCallback(() => {
    if (!interactingRef.current) {
      rafRef.current = null;
      return;
    }
    // Fix #1564-3: only the hovered node + neighbors are ever labelled, so the
    // per-frame work is tiny — the hover label tracks smoothly during pan/zoom.
    refreshLabelsRef.current();
    // Fix #1607: keep point sizing in lock-step with the zoom every frame during
    // a continuous pinch/wheel/drag so the sublinear growth + cap track smoothly.
    applyZoomSizingRef.current();
    // Fix #2110: also re-pack link widths with current zoom compensation so the
    // zoom-aware link width tracks smoothly during continuous pan/pinch.
    applyZoomLinkWidthsRef.current();
    rafRef.current = requestAnimationFrame(motionLoop);
  }, []);
  const startInteraction = useCallback(() => {
    interactingRef.current = true;
    // #4654: latch — the user is now driving the camera; stop any auto-correct.
    userInteractedRef.current = true;
    if (rafRef.current === null) rafRef.current = requestAnimationFrame(motionLoop);
  }, [motionLoop]);
  const endInteraction = useCallback(() => {
    interactingRef.current = false;
    if (rafRef.current !== null) {
      cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
    // Re-project the hover label now that motion has stopped.
    requestAnimationFrame(() => refreshLabelsRef.current());
  }, []);
  const startInteractionRef = useRef(startInteraction);
  startInteractionRef.current = startInteraction;
  const endInteractionRef = useRef(endInteraction);
  endInteractionRef.current = endInteraction;

  // Fix #1548-2: cosmos.gl `fitView` only takes effect while the render loop is
  // running; once the graph is paused (settled) it is a silent no-op. This
  // helper guarantees a camera fit at any time: briefly resume the loop, fit
  // instantly, re-pin the geometry so the physics can't drift, then pause again.
  const fitNow = useCallback((indices?: number[]) => {
    const g = graphRef.current;
    if (!g) return;
    // Fix #1562: this was the crash site. cosmos.gl's fitView builds a bounds
    // buffer from the point positions; if the simulation diverged to
    // NaN/Infinity, that buffer is sized from Infinity and
    // `new Float32Array(...)` throws "Array buffer allocation failed", taking
    // down the whole canvas. Sanitize positions to finite/clamped values BEFORE
    // any fit, and wrap the (still GPU-side) fit in a try/catch so a buffer
    // failure degrades gracefully instead of crashing.
    const wasSettled = hasSettledRef.current;
    const raw = g.getPointPositions();
    const { array: clean, repaired } = sanitizePositions(raw);
    try {
      // If geometry was non-finite, re-pin the clamped positions first so the
      // engine's own bbox is sane before we ask it to fit to them.
      if (repaired) g.setPointPositions(clean, true);
      const frozen = wasSettled ? clean : null;
      g.unpause();
      if (indices && indices.length > 0) g.fitViewByPointIndices(indices, 0, FIT_PADDING);
      else g.fitView(0, FIT_PADDING);
      if (frozen) {
        g.setPointPositions(frozen, true);
        g.pause();
      }
    } catch (err) {
      // Recoverable: re-pin clamped geometry and pause so the canvas keeps the
      // last good frame rather than tearing down. (Fix #1562)
      console.error("[graph-canvas] fitNow failed; recovering with clamped geometry", err);
      try {
        g.setPointPositions(clean, true);
        g.pause();
      } catch {
        /* engine unrecoverable — leave as-is rather than crash */
      }
    }
  }, []);
  const fitNowRef = useRef(fitNow);
  fitNowRef.current = fitNow;

  // Fix #1548-3: when EXITING focus we restore the snapshotted camera, so the
  // settle handler must NOT auto-fit (which would clobber the restore).
  const suppressFitRef = useRef(false);
  const doSettle = useCallback(() => {
    if (hasSettledRef.current) return;
    hasSettledRef.current = true;
    if (capTimerRef.current) {
      clearTimeout(capTimerRef.current);
      capTimerRef.current = null;
    }
    const g = graphRef.current;
    if (!g) return;

    // Persist the settled positions (the layout cache) BEFORE we touch the
    // camera — saving the geometry, not the view transform.
    // Fix #1562: sanitize first — never persist (or re-pin) NaN/Infinity, or the
    // next reload would load a poisoned layout cache and crash again. If the
    // simulation diverged we clamp every coordinate to a finite range; the
    // clamped layout still settles cleanly.
    const { array: positions, repaired } = sanitizePositions(g.getPointPositions());
    if (repaired) {
      console.warn("[graph-canvas] non-finite positions at settle; clamped before fit/cache");
    }
    // Fix #1567-2: ONLY snapshot the layout cache once the settle has a GOOD
    // spread. The cap timer can fire while the sim is still mid-collapse (an
    // over-contracted blob); persisting that made RELOAD render the bad layout
    // while Reset (re-run sim) looked right. Skip the cache write on a degenerate
    // (collapsed / tiny-bbox) layout so the next load re-settles instead of
    // restoring junk. (We never load a degenerate cache either — see load paths.)
    if (positions.length > 0 && !isDegenerateLayout(positions)) {
      saveLayout(group, nodeIds, positions);
    }

    // Fix #1548-2: cosmos.gl `fitView` is a no-op while the render loop is
    // paused — that was why the settled graph floated off-center and didn't
    // FILL the viewport. We freeze the geometry (re-pin the settled positions)
    // and pause the physics FIRST, then use the fitNow helper which briefly
    // resumes the render loop to apply an INSTANT fit and pauses again — so the
    // fit lands deterministically on the final geometry with no drift.
    g.setPointPositions(positions, true);
    g.pause();

    if (suppressFitRef.current) {
      // Exiting focus: skip the fit (restoreCamera owns the view).
      suppressFitRef.current = false;
      scheduleLabels();
      onSettledRef.current();
      return;
    }

    fitNowRef.current();
    // Fix #1607: the fit sets the fitted zoom level → recompute the zoom-driven
    // point size so the very first painted (fitted) frame already has perceptible,
    // non-overlapping nodes with no manual tuning.
    applyZoomSizingRef.current(true);
    // Fix #2110: also apply zoom-compensated link widths at initial settle so the
    // very first paint already reflects the fitted zoom level.
    applyZoomLinkWidthsRef.current(true);
    scheduleLabels();
    // One more fit on the next frames in case the canvas size settled late.
    setTimeout(() => {
      fitNowRef.current();
      applyZoomSizingRef.current(true);
      applyZoomLinkWidthsRef.current(true);
      scheduleLabels();
    }, 200);
    onSettledRef.current();
  }, [group, nodeIds, scheduleLabels]);
  const doSettleRef = useRef(doSettle);
  doSettleRef.current = doSettle;

  // Fix #1581: keep the latest packed buffers in a ref so the UNIFIED settle
  // routine below can be a stable (dep-free) callback shared by every fresh-settle
  // entry point (first load, Reset/Re-layout, group / group-by change, ego
  // re-layout) without each one re-deriving the kick logic.
  const packedRef = useRef(packed);
  packedRef.current = packed;

  // ── Fix #1607: SUBLINEAR, capped zoom-driven point sizing ────────────────────
  // cosmos's built-in scalePointsOnZoom is LINEAR: zoom in → size×zoom (blobs that
  // overlap), zoom out → tiny dots. That single linear law can't be perceptible
  // when zoomed out AND non-overlapping when zoomed in. We turn cosmos's law OFF
  // and drive `pointSizeScale` ourselves from the LIVE zoom level with a SUBLINEAR
  // response and a hard pixel cap:
  //
  //   factor(z) = clamp( z^ZOOM_EXP , MIN_FACTOR , capFactor )
  //
  //   • z^0.5 (sqrt) grows nodes GENTLY as you zoom in — they get bigger so they
  //     read as discs, but far slower than the geometry spreads apart, so they
  //     never catch up and overlap into blobs.
  //   • MIN_FACTOR keeps nodes a perceptible fraction of their base size when
  //     zoomed all the way OUT (visible dots, not invisible lines).
  //   • capFactor is derived from render.maxPointSize: it's the factor at which
  //     the LARGEST node (packed.maxSizePx, a hub) hits exactly maxPointSize px, so
  //     no node ever blobs past the cap no matter how far the user zooms in.
  //
  // The on-screen px of a node ≈ baseSizePx × pointSizeScale (scalePointsOnZoom is
  // off, so cosmos does NOT multiply by zoom again — we own the whole zoom curve).
  // Fix #2110: track the live zoom level so packLinkWidths can apply zoom
  // compensation. Updated on every onZoom event (mount-only handler reads via ref).
  const currentZoomRef = useRef(1);

  const ZOOM_EXP = 0.5; // sqrt → sublinear growth
  const MIN_FACTOR = 0.62; // floor: zoomed-out dots stay perceptible
  const lastZoomScaleRef = useRef(-1);
  // Fix #2110: a ref-stable callback to re-pack + push link widths at the
  // current zoom level. Called from onZoom and the rAF motion loop alongside
  // applyZoomSizing — same zoom-listener reuse pattern the node-size updater uses.
  const packLinkWidthsRef = useRef(packLinkWidths);
  packLinkWidthsRef.current = packLinkWidths;
  const lastZoomWidthRef = useRef(-1);
  const applyZoomLinkWidths = useCallback((force = false) => {
    const g = graphRef.current;
    if (!g) return;
    let z = 1;
    try {
      z = g.getZoomLevel() || 1;
    } catch {
      z = 1;
    }
    if (!Number.isFinite(z) || z <= 0) z = 1;
    // Only re-pack when zoom moved enough to change the compensation tier.
    if (!force && Math.abs(z - lastZoomWidthRef.current) < 0.05) return;
    lastZoomWidthRef.current = z;
    currentZoomRef.current = z;
    if (!renderRef.current.showLinks) return;
    g.setLinkWidths(packLinkWidthsRef.current());
    if (hasSettledRef.current) g.render();
  }, []);
  const applyZoomLinkWidthsRef = useRef(applyZoomLinkWidths);
  applyZoomLinkWidthsRef.current = applyZoomLinkWidths;

  const applyZoomSizing = useCallback((force = false) => {
    const g = graphRef.current;
    if (!g) return;
    let z = 1;
    try {
      z = g.getZoomLevel() || 1;
    } catch {
      z = 1;
    }
    if (!Number.isFinite(z) || z <= 0) z = 1;
    const r = renderRef.current;
    const baseScale = r.pointSizeScale; // user "Point size scale" knob (default 1.0)
    const maxPx = Math.max(2, r.maxPointSize);
    const maxBasePx = packedRef.current.maxSizePx; // largest hub base px @ zoom 1
    // capFactor: the scale at which the largest node reaches maxPx on screen.
    const capFactor = maxPx / (maxBasePx * baseScale);
    // Fix #1607: the "Grow nodes on zoom" toggle (render.scalePointsOnZoom) chooses
    // the zoom law. ON (default) = sublinear growth z^0.5 so nodes read as discs
    // when zoomed in without overlapping; OFF = constant on-screen size (factor 1),
    // i.e. nodes keep a fixed pixel size at every zoom. Both stay px-capped.
    const raw = renderRef.current.scalePointsOnZoom ? Math.pow(z, ZOOM_EXP) : 1;
    const factor = Math.max(MIN_FACTOR, Math.min(capFactor, raw));
    const next = baseScale * factor;
    // Skip redundant setConfig churn if the scale hasn't meaningfully moved.
    if (!force && Math.abs(next - lastZoomScaleRef.current) < 1e-3) return;
    lastZoomScaleRef.current = next;
    g.setConfig({ pointSizeScale: next });
    // While settled the loop is paused; nudge a single repaint so the new size
    // lands without re-running the simulation.
    if (hasSettledRef.current) g.render();
  }, []);
  const applyZoomSizingRef = useRef(applyZoomSizing);
  applyZoomSizingRef.current = applyZoomSizing;

  // Fix #1581: THE single source of truth for "run a fresh settle". Previously the
  // first-load no-cache path (mount effect) and the Reset/Re-layout path used
  // DIFFERENT kick routines: the mount path scheduled a mid-settle re-heat (a
  // second alpha pass partway through the window) gated by reheatedRef +
  // onSimulationEnd, while Reset just did `g.start(1)` + a lone cap timer with NO
  // re-heat. So a fresh page LOAD converged via the re-heat to the good spread,
  // but a Reset (and the diverging older logic) could freeze before the islands
  // finished pulling in — reload and Reset did not match. We now route BOTH
  // through this one function so a fresh load is byte-for-byte the SAME settle the
  // Reset button runs: reseed from packed.positions → start → mid-settle re-heat →
  // hard cap → doSettle (fit + cache). Reload === Reset by construction.
  const kickFreshSettle = useCallback(() => {
    const g = graphRef.current;
    if (!g) return;
    // #4605 — don't seed/start the engine on an empty graph (0-sized texture →
    // regl `invalid texture shape`). The empty-state overlay is shown instead.
    if (!renderableRef.current) return;
    const p = packedRef.current;
    hasSettledRef.current = false;
    didAutoStartRef.current = true;
    reheatedRef.current = false;
    mountTimeRef.current = Date.now();
    if (capTimerRef.current) {
      clearTimeout(capTimerRef.current);
      capTimerRef.current = null;
    }
    if (reheatTimerRef.current) {
      clearTimeout(reheatTimerRef.current);
      reheatTimerRef.current = null;
    }
    g.setPointPositions(p.positions);
    g.setPointClusters(p.clusters);
    g.setPointClusterStrength(p.clusterStrength);
    g.render();
    g.create();
    g.start(1);
    const capMs = Math.max(500, Math.min(6000, (simRef.current.settleTime ?? 2.0) * 1000));
    // Mid-settle re-heat: a single alpha pass partway through cools down before the
    // strong center/gravity finish pulling the islands inward (the hollow-ring
    // failure). Re-heat once at ~45% so it keeps converging to the canvas-filling
    // spread, then the cap is the hard backstop. onSimulationEnd ignores the first
    // (pre-re-heat) cool-down via reheatedRef so it never freezes a half-spread.
    reheatTimerRef.current = setTimeout(() => {
      const gg = graphRef.current;
      reheatedRef.current = true;
      if (gg && !hasSettledRef.current) gg.start(1);
    }, Math.round(capMs * 0.45));
    capTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current();
    }, capMs);
  }, []);
  const kickFreshSettleRef = useRef(kickFreshSettle);
  kickFreshSettleRef.current = kickFreshSettle;

  // ── mount the engine ONCE ─────────────────────────────────────────────────
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const g = new Graph(container, {
      backgroundColor: isDark ? "#020617" : "#f8fafc",
      spaceSize: SPACE_SIZE,
      pixelRatio: Math.min(window.devicePixelRatio, 1.5),
      // Fix #1607: ALWAYS off — our applyZoomSizing updater is the sole zoom→size
      // law (sublinear + px-capped). cosmos's built-in linear law must not stack.
      scalePointsOnZoom: false,
      pointSizeScale: render.pointSizeScale,
      pointOpacity: render.pointOpacity,
      pointGreyoutOpacity: 0.15,
      linkGreyoutOpacity: render.linkOpacity * 0.5,
      linkWidthScale: render.showLinks ? render.linkWidthScale : 0,
      // Fix #1558-1: keep links at a CONSTANT on-screen pixel width regardless of
      // zoom. With this off, the floored widths from packLinkWidths hold at every
      // zoom level, so the long inter-island bridge links never thin out and
      // disappear when the user zooms all the way out.
      scaleLinksOnZoom: false,
      // ─── HOW THE LINK-VISIBILITY KNOBS WORK (cosmos.gl link frag shader) ───
      // The shader computes, per link, per frame:
      //
      //   visFactor = max(
      //     linkVisibilityMinTransparency,
      //     map(linkDistPx, range[1], range[0], 0, 1)   // linear, clamped 0..1
      //   )
      //   finalAlpha = color.a * linkOpacity * visFactor   // (× greyout if dimmed)
      //
      // where `linkDistPx` is the link's length in ON-SCREEN PIXELS (it grows as
      // you zoom IN, shrinks as you zoom OUT, because scaleLinksOnZoom is off so
      // only the *positions* scale, not the width). `map(linkDistPx, hi, lo, 0, 1)`
      // therefore returns ≈1 for SHORT on-screen links (zoomed out) and falls
      // toward 0 for LONG on-screen links (zoomed in), then is floored at
      // linkVisibilityMinTransparency.
      //
      // Fix #1548-2: the cosmos defaults ([50,150] / 0.25) made every link nearly
      // invisible at the fitted (zoomed-out) level — links only "appeared" after
      // zooming/settling — so the floor was raised to 0.8 and the range widened.
      //
      // Fix #4852: that map() is the entire source of the "edges dim when zoomed
      // IN, over-strong when zoomed OUT" inconsistency: zooming in lengthens links
      // in px → visFactor sinks toward the floor; zooming out shortens them →
      // visFactor rides at ~1.0. The previous [1,10000]/0.8 still left a 0.8→1.0
      // (≈20%) brightness swing tied purely to zoom. We do NOT want any
      // zoom-coupled alpha here — link emphasis is already fully governed by the
      // per-link `color.a` tiers (packLinkColors) and the master `linkOpacity`
      // slider, both zoom-independent. So pin the visibility factor to a CONSTANT
      // by setting MinTransparency = 1.0: `max(1.0, map(...))` clamps visFactor to
      // 1.0 at every zoom (map() is ≤1 for any on-screen length within the range),
      // making finalAlpha = color.a * linkOpacity, independent of zoom. The
      // distance range is kept wide so the (now-inert) map() can never dip below 1
      // within the visible pixel range. Net effect: identical, predictable edge
      // legibility from the fitted zoom-out to deep zoom-in — no near-invisible
      // links zoomed in, no harsh over-strong mat zoomed out.
      linkVisibilityDistanceRange: [1, 10000],
      linkVisibilityMinTransparency: 1.0,
      renderHoveredPointRing: true,
      hoveredPointRingColor: isDark ? "#e2e8f0" : "#1e293b",
      pointSamplingDistance: 120,
      enableSimulation: true,
      simulationLinkSpring: simulation.linkSpring,
      simulationLinkDistance: Math.max(simulation.linkDistance, 8),
      // Fix #1562: the #1558 gravity (0.35) stacked with the strong link-spring,
      // center and cluster forces against near-zero repulsion, so on a large
      // graph the net inward pull overwhelmed everything and positions diverged
      // to NaN. A modest gravity still pulls disconnected islands inward to fill
      // the canvas, but with the restored repulsion the net force now stays
      // bounded. (was 0.35; original pre-#1558 was 0.02)
      simulationGravity: 0.2,
      simulationFriction: simulation.friction,
      // Fix #1562: a moderate cool-down. #1558 pushed this to 3000 to give the
      // strong forces time to converge from the hollow ring; with the rebalanced
      // (bounded) forces a 2000 decay converges without keeping a borderline
      // simulation alive long enough to wander into a divergent state. The
      // ≤settleTime wall-clock cap still bounds total settle time.
      simulationDecay: 2000,
      // Fix #1566: weak GLOBAL cluster force — clustering is now essentially
      // color-only. A moderate value (0.2) still re-formed the ring by holding
      // each cluster near its seeded slot; at this low strength the link-spring
      // + center own the macro layout and connected clusters sit adjacent,
      // filling the center. Bounded — no destabilization at 1316 nodes. (was 0.2)
      simulationCluster: 0.05,
      simulationRepulsion: simulation.repulsion,
      simulationCenter: simulation.center,
      rescalePositions: true,
      // Fix #1558-2: let OUR doSettle own the final fit (after the mid-settle
      // re-heat has converged). cosmos's own fitViewOnInit fired at a fixed delay
      // mid-reheat and captured the still-spread layout, so the graph looked like
      // it floated/ringed even though it later converged. Disable it here.
      fitViewOnInit: false,
      fitViewPadding: FIT_PADDING,
      onSimulationEnd: () => {
        // Fix #1558-2: ignore the FIRST early cool-down (before the mid-settle
        // re-heat) so we don't freeze a still-spread hollow ring. After the
        // re-heat has run, the second cool-down settles the converged layout. The
        // cap timer is the hard backstop either way.
        if (!hasSettledRef.current && reheatedRef.current) doSettleRef.current();
      },
      onSimulationTick: scheduleLabelsLive,
      onClick: (index?: number) => {
        userInteractedRef.current = true; // #4654: user took control → no auto-correct
        if (index === undefined) {
          onNodeClickRef.current(null);
          return;
        }
        const n = nodesRef.current[index];
        if (!n) return;
        if (n.id === selectedRef.current) onNodeClickRef.current(null);
        else onNodeClickRef.current(n);
      },
      onBackgroundClick: () => {
        graphRef.current?.unselectPoints();
        onNodeHoverRef.current(null);
        onNodeClickRef.current(null);
      },
      onMouseMove: (index?: number) => {
        if (index === undefined) {
          onNodeHoverRef.current(null);
          return;
        }
        const n = nodesRef.current[index];
        if (n) onNodeHoverRef.current(n);
      },
      // Fix #1548-1: pan AND wheel-zoom both flow through the d3-zoom behavior,
      // so onZoomStart/onZoomEnd bracket every camera move. Start the per-frame
      // rAF label loop on start, stop it on end. onZoom still nudges a refresh
      // for the (rare) single-shot zoom that doesn't emit start/end.
      onZoomStart: () => startInteractionRef.current(),
      onZoom: () => {
        // Fix #1607: drive the sublinear, capped point size off every zoom event
        // (covers the single-shot wheel zoom that doesn't bracket start/end). The
        // rAF motion loop handles continuous zoom/pan; this catches the rest.
        applyZoomSizingRef.current();
        // Fix #2110: also re-pack link widths with updated zoom compensation on
        // single-shot wheel zoom events not bracketed by start/end.
        applyZoomLinkWidthsRef.current();
        if (!interactingRef.current) scheduleLabelsLive();
      },
      onZoomEnd: () => endInteractionRef.current(),
      onDragStart: () => startInteractionRef.current(),
      onDragEnd: () => endInteractionRef.current(),
    });
    graphRef.current = g;
    if (import.meta.env.DEV) (window as unknown as { __ag?: Graph }).__ag = g;

    const saved = loadLayout(group, nodeIds);
    // Fix #1562: a layout cached BEFORE this fix could contain NaN/Infinity from
    // a diverged settle; loading it would crash on the first fit. Only trust a
    // fully-finite cache; otherwise fall through to a fresh (now-stable) layout.
    // Fix #1567-2 / #1581: ALSO reject a degenerate (collapsed) OR over-contracted
    // cache via isLayoutHealthy, and the cache is now version-scoped so a layout
    // baked by retired force defaults is a guaranteed MISS. On any reject we run
    // the EXACT same fresh settle the Reset button runs (kickFreshSettle) so the
    // reload converges to the good spread — reload === Reset.
    // #4605 — if we mounted onto an EMPTY graph (deep-link to an unresolved
    // `?node=<id>`), do NOT seed or settle the engine: a 0-point buffer sizes the
    // textures 0×0 and regl throws `invalid texture shape`. The empty-state
    // overlay is rendered instead; the data-push effect re-seeds + settles if/when
    // a renderable node set arrives (e.g. on focus exit). The cleanup below still
    // registers so the engine is destroyed on unmount.
    if (!renderableRef.current) {
      // fall through to register cleanup; skip only the seed/settle.
    } else if (saved && isLayoutHealthy(saved.positions, nodeIds.length * 2)) {
      requestAnimationFrame(() => {
        const gg = graphRef.current;
        if (!gg) return;
        gg.setPointPositions(saved.positions, true);
        doSettleRef.current();
      });
    } else {
      // No valid cache → fresh settle, identical to Reset/Re-layout. Claim the
      // auto-start synchronously so the data-push effect (which runs right after
      // this mount effect in the same commit) doesn't kick its own competing
      // g.start(1); the unified kick (deferred a frame so buffers are uploaded)
      // owns the settle.
      didAutoStartRef.current = true;
      requestAnimationFrame(() => kickFreshSettleRef.current());
    }

    return () => {
      if (capTimerRef.current) clearTimeout(capTimerRef.current);
      if (reheatTimerRef.current) clearTimeout(reheatTimerRef.current);
      if (labelTimerRef.current !== null) clearTimeout(labelTimerRef.current);
      if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
      interactingRef.current = false;
      g.destroy();
      graphRef.current = null;
    };
    // mount-only — do NOT recreate on data/config change
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ── data push (positions / colors / sizes / clusters / links) ───────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    // #4605 — an EMPTY node set sizes cosmos's point/cluster textures as 0×0 and
    // regl throws `invalid texture shape` from `create()`. Never push a zero-point
    // buffer; the empty-state overlay (below) is shown instead.
    if (!renderable) return;
    const prev = g.getPointPositions();
    if (hasSettledRef.current && prev.length === packed.positions.length) {
      // Fix #1562: re-pin the settled geometry, but sanitize first so a diverged
      // layout never gets pushed back into the engine.
      g.setPointPositions(sanitizePositions(prev).array, true);
    } else {
      g.setPointPositions(packed.positions);
    }
    g.setPointSizes(packed.sizes);
    g.setPointClusters(packed.clusters);
    g.setPointClusterStrength(packed.clusterStrength);
    g.setPointColors(packPointColors());
    g.setLinks(linkData.links);
    g.setLinkColors(packLinkColors());
    g.setLinkWidths(packLinkWidths());
    g.render();
    g.create();
    if (!didAutoStartRef.current && !hasSettledRef.current) {
      const saved = loadLayout(group, nodeIds);
      // Fix #1581: an unhealthy/missing cache → run the UNIFIED fresh settle (the
      // same routine Reset uses) instead of a bare g.start(1) here. This is a
      // safety net: the mount effect normally claims the auto-start, but if the
      // first data push beat it (or arrived after a node-set change), route through
      // kickFreshSettle so reload still === Reset. A healthy cache is left to the
      // mount/group handlers to pin + settle.
      if (!saved || !isLayoutHealthy(saved.positions, nodeIds.length * 2)) {
        kickFreshSettleRef.current();
      }
    }
    if (hasSettledRef.current) g.pause();
    scheduleLabels();
  }, [renderable, packed, packPointColors, linkData, packLinkColors, packLinkWidths, group, nodeIds, scheduleLabels]);

  // ── recolor on theme / colorMode ────────────────────────────────────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    if (!renderable) return; // #4605 — no engine writes on an empty graph
    g.setPointColors(packPointColors());
    g.setLinkColors(packLinkColors());
    g.setLinkWidths(packLinkWidths());
    g.setConfig({ backgroundColor: isDark ? "#020617" : "#f8fafc" });
    g.render();
    g.create();
    if (hasSettledRef.current) g.pause();
  }, [renderable, packPointColors, packLinkColors, packLinkWidths, isDark]);

  // ── live render/sim config ──────────────────────────────────────────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    if (!renderable) return; // #4605 — no engine writes on an empty graph
    g.setConfig({
      pointOpacity: render.pointOpacity,
      // Fix #1607: do NOT set pointSizeScale here — the zoom-driven updater owns it
      // (base scale × sublinear-capped zoom factor). We re-run that updater below so
      // a knob change (Point size scale / Max point size) re-derives the live scale
      // at the current zoom. We also force scalePointsOnZoom OFF: our updater is the
      // single source of the zoom→size law, and cosmos's linear law must not stack.
      scalePointsOnZoom: false,
      linkWidthScale: render.showLinks ? render.linkWidthScale : 0,
      simulationLinkSpring: simulation.linkSpring,
      simulationLinkDistance: Math.max(simulation.linkDistance, 8),
      simulationFriction: simulation.friction,
      simulationRepulsion: simulation.repulsion,
      simulationCenter: simulation.center,
    });
    // Fix #1607: re-derive pointSizeScale from the live zoom (force, since the knob
    // may have changed maxPointSize / base scale).
    applyZoomSizingRef.current(true);
    g.setLinkColors(packLinkColors());
    g.setLinkWidths(packLinkWidths());
    g.render();
    if (hasSettledRef.current) g.pause();
  }, [renderable, render, simulation, packLinkColors, packLinkWidths]);

  // ── re-layout request (Reset / re-layout) ───────────────────────────────────
  // Fix #1581: this IS the canonical fresh-settle now; it (and first load) both
  // call kickFreshSettle so the two paths are identical.
  useEffect(() => {
    if (relayoutRef.current === relayoutNonce) return;
    relayoutRef.current = relayoutNonce;
    if (!graphRef.current) return;
    kickFreshSettleRef.current();
  }, [relayoutNonce, packed]);

  // ── #4641: AUTO-SETTLE on first data load (the recurring "explode"/clumped
  //    bug, #4492/#2107). On the very first render of a NON-EMPTY renderable
  //    graph the canvas used to arrive clumped — the seed positions paused
  //    before the force layout spread them — and the user had to click Reset.
  //    Reset runs kickFreshSettle; so do we, exactly once, the first time a
  //    renderable graph appears WITHOUT a healthy cached layout. With a healthy
  //    cache we intentionally pin+fit the saved spread instead (instant reload),
  //    which the mount/group handlers already own. This is the single guaranteed
  //    entry point so the graph arrives already spread with no manual Reset; it
  //    is idempotent (autoSettledRef), respects the crash-guard (renderable),
  //    and never double-fires against the mount/group/relayout settles (which all
  //    set didAutoStartRef / hasSettledRef that we check before kicking). ──────
  //
  // #4654: the #4641/#4645 one-shot `autoSettledRef` kick fired on the FIRST
  // effect tick — before the cosmos engine had a seeded, renderable frame and
  // before the data/positions were actually present — so it settled a half-loaded
  // state and the graph still arrived as a tight ball needing a manual Reset. The
  // fix is a VERIFY-THEN-RETRY controller, not a one-shot:
  //
  //   1. READY GATE. We only kick once the graph is genuinely ready: renderable
  //      (crash-guard), the engine exists, AND the LIVE point buffer is non-empty
  //      (g.getPointPositions() has data → first cosmos frame seeded). This is what
  //      the prior fix was missing — it raced the data/position load.
  //   2. KICK. We run the EXACT routine the Reset button runs (kickFreshSettle:
  //      reseed → start → mid-settle re-heat → cap → doSettle + fit + cache).
  //   3. VERIFY + RETRY. A beat after each settle we read the LIVE positions and
  //      ask isDegenerateLayout() — the same collapsed/over-contracted-ball check
  //      the cache-trust path uses. If the layout still looks collapsed we re-run
  //      the kick. Bounded to AUTO_RESET_MAX_RETRIES (2) spaced over a few seconds,
  //      then we stop for good. This is the user's "simulate pressing Reset after a
  //      few seconds", implemented as a self-checking, self-terminating loop.
  //
  // It will NOT loop forever (hard retry + total-time bound), will NOT fight the
  // empty/deep-link crash-guard (gated on `renderable`/live positions, never seeds
  // an empty graph), and will NOT re-fire after the user pans/zooms/selects
  // (userInteractedRef latch). Once the graph is genuinely spread, or the budget is
  // spent, or the user takes control, the controller is permanently disarmed.
  const autoCorrectDoneRef = useRef(false);
  const AUTO_RESET_MAX_RETRIES = 2;
  const AUTO_RESET_VERIFY_DELAY_MS = 1500;
  useEffect(() => {
    if (autoCorrectDoneRef.current) return;
    if (!renderable) return; // crash-guard: never seed/start an empty graph
    const g = graphRef.current;
    if (!g) return;

    // A healthy cache is owned by the mount/group handlers (pin + fit) — the saved
    // spread is restored instantly, no fresh settle needed. Disarm.
    const saved = loadLayout(group, nodeIds);
    if (saved && isLayoutHealthy(saved.positions, nodeIds.length * 2)) {
      autoCorrectDoneRef.current = true;
      return;
    }

    let retries = 0;
    let cancelled = false;
    let verifyTimer: ReturnType<typeof setTimeout> | null = null;

    // READY GATE: the engine must have a seeded, non-empty live point buffer before
    // we kick, otherwise we'd settle a half-loaded state (the #4654 race). Poll a
    // few animation frames until the first cosmos frame has positions, then start.
    const liveReady = (): boolean => {
      const gg = graphRef.current;
      if (!gg || !renderableRef.current) return false;
      try {
        const live = gg.getPointPositions();
        return !!live && live.length >= packedRef.current.positions.length && live.length > 0;
      } catch {
        return false;
      }
    };

    // Has another settle path already produced a GOOD spread? Read live positions
    // and reuse the same collapsed/over-contracted check the cache path trusts.
    const looksCollapsed = (): boolean => {
      const gg = graphRef.current;
      if (!gg) return false;
      try {
        const live = gg.getPointPositions();
        if (!live || live.length === 0) return false;
        return isDegenerateLayout(new Float32Array(live));
      } catch {
        return false;
      }
    };

    const disarm = () => {
      autoCorrectDoneRef.current = true;
      cancelled = true;
      if (verifyTimer) clearTimeout(verifyTimer);
    };

    // Verify the result a beat after a kick; re-fire if still collapsed, up to the
    // retry bound. Self-terminating: stops on spread, on budget, or on user input.
    const scheduleVerify = () => {
      verifyTimer = setTimeout(() => {
        if (cancelled) return;
        if (userInteractedRef.current) return disarm(); // user took over
        if (!renderableRef.current) return disarm();
        if (!looksCollapsed()) return disarm(); // genuinely spread → done
        if (retries >= AUTO_RESET_MAX_RETRIES) return disarm(); // budget spent
        retries += 1;
        kickFreshSettleRef.current();
        scheduleVerify();
      }, AUTO_RESET_VERIFY_DELAY_MS);
    };

    // Wait (up to ~1.5s of frames) for the live buffer to be ready, then kick once
    // and start the verify-retry loop. If the buffer never arrives we never kick —
    // the crash-guard / data-push paths own that case.
    let waited = 0;
    const WAIT_MAX_MS = 1500;
    const FRAME_MS = 80;
    const waitForReady = () => {
      if (cancelled || autoCorrectDoneRef.current) return;
      if (userInteractedRef.current) return disarm();
      // If another path (mount no-cache kick) already settled a GOOD spread, leave
      // it alone; only adopt the loop if there's no settle in flight or it collapsed.
      if (hasSettledRef.current && !looksCollapsed()) return disarm();
      if (!liveReady()) {
        waited += FRAME_MS;
        if (waited >= WAIT_MAX_MS) return; // give up waiting; other paths own it
        verifyTimer = setTimeout(waitForReady, FRAME_MS);
        return;
      }
      // Ready. If a settle is already mid-flight (didAutoStartRef) we don't kick a
      // competing one — we just verify its result and retry only if it collapses.
      if (!didAutoStartRef.current && !hasSettledRef.current) {
        kickFreshSettleRef.current();
      }
      scheduleVerify();
    };
    waitForReady();

    return () => {
      cancelled = true;
      if (verifyTimer) clearTimeout(verifyTimer);
    };
  }, [renderable, group, nodeIds]);

  // ── re-layout when the node SET changes (Fix #1548-3 ego enter/exit) ─────────
  // Entering/leaving focus swaps `group` (…::ego) and the node set. The settled
  // engine would otherwise just re-pin the new (scattered) positions and pause —
  // so explicitly reset the settle flag and run a fresh layout for the new set.
  const prevGroupRef = useRef(group);
  useEffect(() => {
    if (prevGroupRef.current === group) return;
    prevGroupRef.current = group;
    const g = graphRef.current;
    if (!g) return;
    if (!renderable) return; // #4605 — empty ego set → empty-state, no settle
    hasSettledRef.current = false;
    didAutoStartRef.current = true;
    mountTimeRef.current = Date.now();
    const saved = loadLayout(group, nodeIds);
    // Fix #1562/#1567-2/#1581: reuse the cache only when it's HEALTHY (finite, not
    // collapsed, not over-contracted) AND version-current; otherwise run the
    // unified fresh settle (== Reset).
    if (saved && isLayoutHealthy(saved.positions, packed.positions.length)) {
      g.setPointPositions(saved.positions, true);
      g.render();
      g.create();
      doSettleRef.current();
      return;
    }
    kickFreshSettleRef.current();
  }, [renderable, group, packed, nodeIds]);

  // ── re-cluster on group-by change ───────────────────────────────────────────
  const prevGroupByRef = useRef(groupBy);
  useEffect(() => {
    if (prevGroupByRef.current === groupBy) return;
    prevGroupByRef.current = groupBy;
    if (!graphRef.current) return;
    // Fix #1581: a group-by change re-clusters → run the unified fresh settle.
    kickFreshSettleRef.current();
  }, [groupBy, packed]);

  // ── visibility / hover spotlight + repo + community selection ─────────────────
  // Fix #1548-3: ego focus is no longer a greyout — the parent passes a pre-built
  // ego SUB-graph as nodes/edges, so here we only handle repo filter (hard-hide),
  // community focus (soft-dim), and the hover spotlight.
  //
  // Fix #1567-3: HOVER SPOTLIGHT. On node hover we DIM the whole graph and keep
  // only the hovered node + its DIRECT neighbors (and the links among them) at
  // full opacity. cosmos.gl's selectPointsByIndices does exactly this: selected
  // points stay full, everything else greys to pointGreyoutOpacity and their links
  // to linkGreyoutOpacity — and a link is only kept bright when BOTH endpoints are
  // selected, so the in-neighborhood links light up while the rest recede. Hover
  // takes priority while hovering; on un-hover we fall back to the repo/community
  // selection state (so the spotlight cleanly restores). This is a config/select
  // call only (no re-layout), so it's smooth + rAF-free.
  const applySelection = useCallback(() => {
    const g = graphRef.current;
    if (!g) return;
    const hovId = hoveredNodeId;
    const hovIdx = hovId != null ? idToIdx.get(hovId) : undefined;

    // Hover spotlight wins while a node is hovered.
    if (hovIdx !== undefined) {
      const sel = new Set<number>([hovIdx]);
      for (const nb of neighborIdx.get(hovIdx) ?? []) sel.add(nb);
      g.selectPointsByIndices(Array.from(sel));
      // Fix #1567-3: dim non-neighbor NODES hard so the neighborhood reads as a
      // spotlight (still faintly visible for context).
      // Fix #1580: also FADE the non-neighbor LINKS to near-zero. cosmos.gl keeps a
      // link bright only when BOTH endpoints are selected, and greys every other
      // link to linkGreyoutOpacity. The default greyout (linkOpacity*0.5) left the
      // whole link mesh visible — the dimmed graph looked like a skeleton of
      // threads. Drop linkGreyoutOpacity to ~0 so only the hovered node + its
      // direct neighbors AND the links AMONG them stay bright; everything else
      // recedes, cleanly isolating the neighborhood. Restored on un-hover below.
      g.setConfig({
        pointGreyoutOpacity: isDark ? 0.08 : 0.1,
        linkGreyoutOpacity: 0,
      });
      g.render();
      return;
    }

    // #4643 — replay/glow dim-focus. When a replay step (or a per-row replay)
    // is active and the user isn't hovering, dim everything except the step's
    // CAPPED node set (+ neighbors) — same spotlight mechanism as hover, driven
    // by replayFocusIdxRef. This is cleared (set emptied) when the glow ends.
    const replayFocus = replayFocusIdxRef.current;
    if (replayFocus.size > 0) {
      g.selectPointsByIndices(Array.from(replayFocus));
      g.setConfig({
        pointGreyoutOpacity: isDark ? 0.08 : 0.1,
        linkGreyoutOpacity: 0,
      });
      g.render();
      return;
    }

    // Fix #1580: not hovering → restore the default link greyout so the
    // repo/community selection (and the un-hovered full graph) shows links again.
    const restoredLinkGreyout = render.linkOpacity * 0.5;
    const repoActive = activeRepos != null;
    const communityActive = focusedCommunityId != null;
    if (!repoActive && !communityActive) {
      g.selectPointsByIndices(null);
      g.setConfig({ pointGreyoutOpacity: 0.15, linkGreyoutOpacity: restoredLinkGreyout });
      g.render();
      return;
    }
    const effective: number[] = nodes
      .map((n, i) => {
        if (repoActive && !activeRepos!.has(n.repo)) return -1;
        if (communityActive && n.communityId !== focusedCommunityId) return -1;
        return i;
      })
      .filter((i) => i !== -1);
    g.selectPointsByIndices(effective);
    g.setConfig({
      pointGreyoutOpacity: repoActive ? 0 : 0.18,
      linkGreyoutOpacity: restoredLinkGreyout,
    });
    g.render();
  }, [
    nodes,
    activeRepos,
    focusedCommunityId,
    hoveredNodeId,
    idToIdx,
    neighborIdx,
    isDark,
    render.linkOpacity,
  ]);

  // #4643 — live ref so the glow rAF effect (mount-stable, epoch-keyed deps)
  // can re-apply the selection/dim-focus without taking applySelection as a dep
  // (which would re-run the glow effect on every hover change).
  const applySelectionRef = useRef(applySelection);
  applySelectionRef.current = applySelection;

  useEffect(() => {
    applySelection();
  }, [applySelection]);

  // ── refresh the hover label when the hovered node changes (Fix #1564-3) ──────
  // This is now the PRIMARY label trigger: a label appears when a node is
  // hovered and is cleared when hover leaves. The settle/tick/zoom schedulers
  // simply keep the (single) hover label projected on the right pixel.
  useEffect(() => {
    scheduleLabels();
  }, [hoveredNodeId, scheduleLabels]);

  // ── #1157 Jarvis: transient GLOW/PULSE on MCP-touched nodes + edges ──────────
  // When the MCP server queries/returns graph entities, the activity SSE stream
  // (useGraphHighlight) hands us the touched node IDs + a bumped `highlightEpoch`.
  // We run a short rAF loop that OVERWRITES only the affected entries in the GPU
  // color/size buffers with a decaying amber pulse, then restores the base
  // buffers when the pulse ends. This is a pure transient overlay — it never
  // re-layouts, re-clusters, or moves a single node, so it stays performant on
  // the 20k-node upvate graph (only the touched indices are rewritten each frame;
  // a typical MCP result touches a handful to a few dozen nodes).
  //
  // Edges: WebUI v2 edges have no synthetic id, so an edge glows when BOTH its
  // endpoints are in the highlighted node set (resolved against linkData.links,
  // which is index-based and parallel to the packed link color buffer).
  const glowRafRef = useRef<number | null>(null);
  // #4643 — the CAPPED, in-view node-index set that the current glow is driving.
  // Shared with the dim-focus selection (applySelection) so both the glow and
  // the "dim everything else" behaviour operate on the SAME bounded set. Empty
  // = no replay focus active.
  const replayFocusIdxRef = useRef<Set<number>>(new Set());
  // #4643 — live ref to the cap reporter so the epoch-keyed glow effect can
  // report "glowing N of M" without taking onGlowCap as a dep.
  const onGlowCapRef = useRef(onGlowCap);
  onGlowCapRef.current = onGlowCap;
  // Stable empty fallback so an undefined prop doesn't thrash the effect deps.
  const highlightSet = highlightedNodeIds ?? EMPTY_HIGHLIGHT;
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    if (!renderableRef.current) return; // #4605 — no glow writes on an empty graph

    // Cancel any in-flight glow loop (a new epoch supersedes the previous pulse).
    if (glowRafRef.current !== null) {
      cancelAnimationFrame(glowRafRef.current);
      glowRafRef.current = null;
    }

    // #4643 — Resolve the affected NODE indices, but CAP to the nodes actually
    // RENDERED / in-view (≤ GLOW_CAP). Without this a step with ~18k matches
    // would build an 18k-index list and rewrite all of them every rAF frame →
    // synchronous main-thread hang. We project each candidate to screen space
    // and keep only those inside the viewport, stopping once we hit the cap.
    // `matchedTotal` is tracked so the caller can render "glowing N of M".
    const w = containerRef.current?.clientWidth ?? 0;
    const h = containerRef.current?.clientHeight ?? 0;
    const positions = g.getPointPositions();
    const haveViewport = w > 0 && h > 0 && !!positions && positions.length > 0;

    let matchedTotal = 0;
    const inView: number[] = [];
    const overflow: number[] = []; // resolved-but-offscreen, used only if no in-view hits
    for (const id of highlightSet) {
      const i = idToIdx.get(id);
      if (i === undefined) continue;
      matchedTotal++;
      // Once both buckets are full there's nothing more to collect — but keep
      // looping (cheap map lookups only) so matchedTotal reflects the true M for
      // the "glowing N of M" badge. The expensive projection below is skipped.
      if (inView.length >= GLOW_CAP) continue;
      if (!haveViewport) {
        inView.push(i);
        continue;
      }
      // Both the in-view target and the offscreen fallback are full → skip the
      // (relatively costly) screen projection; just keep counting matchedTotal.
      if (overflow.length >= GLOW_CAP) continue;
      const px = positions![i * 2];
      const py = positions![i * 2 + 1];
      if (px === undefined || py === undefined) continue;
      const [sx, sy] = g.spaceToScreenPosition([px, py]);
      if (sx >= -50 && sy >= -50 && sx <= w + 50 && sy <= h + 50) {
        inView.push(i);
      } else if (overflow.length < GLOW_CAP) {
        overflow.push(i);
      }
    }
    // If nothing matched is currently on-screen (e.g. the result is off in a
    // far cluster), fall back to a capped sample of the resolved matches so the
    // glow is never silently empty — still bounded by GLOW_CAP.
    const nodeIdxs: number[] = inView.length > 0 ? inView : overflow.slice(0, GLOW_CAP);
    if (matchedTotal > nodeIdxs.length) {
      onGlowCapRef.current?.(nodeIdxs.length, matchedTotal);
    } else {
      onGlowCapRef.current?.(nodeIdxs.length, nodeIdxs.length);
    }

    // Resolve the affected EDGE (link) indices: an edge glows when both its
    // endpoints are highlighted. linkData.links is a flat [src,tgt,...] index
    // buffer parallel to the per-link color quads.
    const edgeIdxs: number[] = [];
    if (nodeIdxs.length > 0) {
      const hiIdx = new Set(nodeIdxs);
      const { links } = linkData;
      for (let li = 0, e = 0; li < links.length; li += 2, e++) {
        if (hiIdx.has(links[li]) && hiIdx.has(links[li + 1])) edgeIdxs.push(e);
      }
    }

    // Nothing to glow (decay finished, or no entity in the current view) →
    // restore the base buffers so any prior pulse is fully cleared, then drop
    // the dim-focus (restore full opacity via the normal selection state).
    if (nodeIdxs.length === 0) {
      g.setPointColors(packPointColors());
      g.setPointSizes(packed.sizes);
      g.setLinkColors(packLinkColors());
      g.render();
      if (replayFocusIdxRef.current.size > 0) {
        replayFocusIdxRef.current = new Set();
        applySelectionRef.current?.();
      }
      return;
    }

    // #4643 — DIM non-step nodes during the glow. Reuse the hover-spotlight
    // mechanism (cosmos.gl selectPointsByIndices + pointGreyoutOpacity) but
    // drive it from the CAPPED step set (+ immediate neighbors) instead of the
    // hovered node. Everything outside this bounded set dims; the step's nodes
    // and their neighbors stay bright so the glow stands out of the hairball.
    // Restored to the normal selection state when the glow ends (above / below).
    {
      const focus = new Set<number>(nodeIdxs);
      for (const i of nodeIdxs) {
        const nbs = neighborIdx.get(i);
        if (!nbs) continue;
        for (const nb of nbs) focus.add(nb);
      }
      replayFocusIdxRef.current = focus;
      applySelectionRef.current?.();
    }

    // Snapshot the BASE buffers once; each frame we re-derive from these so the
    // pulse blends from the node's real color/size (theme + colorMode aware).
    const baseColors = packPointColors();
    const baseSizes = packed.sizes;
    const baseLinkColors = packLinkColors();
    const glowColors = new Float32Array(baseColors);
    const glowSizes = new Float32Array(baseSizes);
    const glowLinkColors = new Float32Array(baseLinkColors);

    const start = performance.now();
    // GLOW_MS mirrors useGraphHighlight.DECAY_MS (~1.8s) — kept local so the
    // canvas has no import cycle with the hook.
    const GLOW_MS = 1800;

    const frame = (now: number) => {
      const gg = graphRef.current;
      if (!gg) return;
      const t = Math.min(1, (now - start) / GLOW_MS);
      // Decay envelope: bright instantly, ease out to 0. A gentle 2-beat pulse
      // rides on top so the glow "breathes" while it fades (the Jarvis feel).
      const decay = 1 - t * t; // ease-out quadratic → 0 at end
      const pulse = 0.78 + 0.22 * Math.cos(t * Math.PI * 4); // ~2 beats over the life
      const intensity = decay * pulse; // 0..1

      // ── nodes: blend base color → amber, and scale size up ──────────────────
      for (const i of nodeIdxs) {
        const o = i * 4;
        const base: RGBA = [
          baseColors[o] * 255,
          baseColors[o + 1] * 255,
          baseColors[o + 2] * 255,
          baseColors[o + 3],
        ];
        // Blend toward amber, force full opacity at the peak so the node pops
        // even if its base alpha (pointOpacity) is low.
        const blended = lerpRGBA(base, JARVIS_GLOW, intensity);
        const a = base[3] + (1 - base[3]) * intensity;
        writeNormalizedRGBA(glowColors, i, [blended[0], blended[1], blended[2], a]);
        // Scale the node up to 2.4× at peak for a visible "halo" bloom.
        glowSizes[i] = baseSizes[i] * (1 + 1.4 * intensity);
      }

      // ── edges: blend base link color → amber + lift alpha ───────────────────
      for (const e of edgeIdxs) {
        const o = e * 4;
        const base: RGBA = [
          baseLinkColors[o] * 255,
          baseLinkColors[o + 1] * 255,
          baseLinkColors[o + 2] * 255,
          baseLinkColors[o + 3],
        ];
        const blended = lerpRGBA(base, JARVIS_GLOW, intensity);
        const a = Math.max(base[3], 0.35) + (1 - Math.max(base[3], 0.35)) * intensity;
        writeNormalizedRGBA(glowLinkColors, e, [blended[0], blended[1], blended[2], a]);
      }

      gg.setPointColors(glowColors);
      gg.setPointSizes(glowSizes);
      if (edgeIdxs.length > 0) gg.setLinkColors(glowLinkColors);
      gg.render();

      if (t < 1) {
        glowRafRef.current = requestAnimationFrame(frame);
      } else {
        // Pulse done → restore the exact base buffers (clean slate) and drop
        // the dim-focus so the full graph returns to normal opacity.
        gg.setPointColors(baseColors);
        gg.setPointSizes(baseSizes);
        gg.setLinkColors(baseLinkColors);
        gg.render();
        glowRafRef.current = null;
        if (replayFocusIdxRef.current.size > 0) {
          replayFocusIdxRef.current = new Set();
          applySelectionRef.current?.();
        }
      }
    };
    glowRafRef.current = requestAnimationFrame(frame);

    return () => {
      if (glowRafRef.current !== null) {
        cancelAnimationFrame(glowRafRef.current);
        glowRafRef.current = null;
      }
      // #4643 — a new epoch supersedes this glow; the next effect run reasserts
      // the focus set from its own (capped) nodes, and a final empty highlight
      // (decay → EMPTY) restores opacity. We intentionally do NOT clear the
      // dim-focus here so consecutive replay steps stay dimmed continuously
      // instead of flashing back to full opacity between steps.
    };
    // Re-run on each fresh highlight (epoch) — NOT on every set identity churn.
    // packPointColors/packLinkColors/packed are stable between data/theme pushes;
    // when those change the data-push / recolor effects re-assert the base
    // buffers, and the next epoch restarts the glow from the new base.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [highlightEpoch]);

  // ── Fix #1548-3: camera snapshot / restore for ego focus enter / exit ─────────
  // cosmos.gl exposes no public pan setter, so we capture (a) the zoom level and
  // (b) the space coordinate currently at the viewport center. To restore we
  // re-center on that point (degenerate fitViewByPointPositions box) then set the
  // recorded zoom — together that reproduces the prior pan + zoom exactly.
  // #1932: live refs so the imperative handle (mount-only `[]` deps) can read
  // the current id→idx map, edge states, and link buffer without re-creating
  // the handle on every render.
  const idToIdxRef = useRef(idToIdx);
  idToIdxRef.current = idToIdx;
  const linkDataRef = useRef(linkData);
  linkDataRef.current = linkData;
  const nodeIdsForHandleRef = useRef(nodeIds);
  nodeIdsForHandleRef.current = nodeIds;

  const cameraSnapRef = useRef<{ zoom: number; center: [number, number] } | null>(null);
  useImperativeHandle(
    ref,
    () => ({
      getNodeScreenPosition: (id: string) => {
        const g = graphRef.current;
        if (!g) return null;
        const idx = idToIdxRef.current.get(id);
        if (idx === undefined) return null;
        try {
          const positions = g.getPointPositions();
          const px = positions[idx * 2];
          const py = positions[idx * 2 + 1];
          if (px === undefined || py === undefined) return null;
          if (!Number.isFinite(px) || !Number.isFinite(py)) return null;
          const [sx, sy] = g.spaceToScreenPosition([px, py]);
          if (!Number.isFinite(sx) || !Number.isFinite(sy)) return null;
          return { x: sx, y: sy };
        } catch {
          return null;
        }
      },
      getEdgeList: () => {
        const { links, states } = linkDataRef.current;
        const ids = nodeIdsForHandleRef.current;
        const out: { src: string; tgt: string; bridge: boolean }[] = [];
        for (let i = 0, e = 0; i < links.length; i += 2, e++) {
          const a = links[i];
          const b = links[i + 1];
          const src = ids[a];
          const tgt = ids[b];
          if (!src || !tgt) continue;
          out.push({ src, tgt, bridge: states[e] === 2 });
        }
        return out;
      },
      isBridgeEdge: (src: string, tgt: string) => {
        const idToIdxNow = idToIdxRef.current;
        const a = idToIdxNow.get(src);
        const b = idToIdxNow.get(tgt);
        if (a === undefined || b === undefined) return false;
        const { links, states } = linkDataRef.current;
        for (let i = 0, e = 0; i < links.length; i += 2, e++) {
          if (
            (links[i] === a && links[i + 1] === b) ||
            (links[i] === b && links[i + 1] === a)
          ) {
            return states[e] === 2;
          }
        }
        return false;
      },
      snapshotCamera: () => {
        const g = graphRef.current;
        const el = containerRef.current;
        if (!g || !el) return;
        try {
          const center = g.screenToSpacePosition([el.clientWidth / 2, el.clientHeight / 2]);
          cameraSnapRef.current = { zoom: g.getZoomLevel() || 1, center };
        } catch {
          cameraSnapRef.current = null;
        }
      },
      restoreCamera: () => {
        const snap = cameraSnapRef.current;
        const g = graphRef.current;
        if (!snap || !g) return;
        // Tell the imminent re-layout's settle handler to NOT auto-fit.
        suppressFitRef.current = true;
        const [cx, cy] = snap.center;
        const apply = () => {
          const gg = graphRef.current;
          if (!gg) return;
          // Fix #1562: sanitize geometry + clamp the restore target so a diverged
          // layout can't size a bounds buffer from Infinity; wrap the camera ops
          // so a buffer failure degrades gracefully.
          const { array: frozen } = sanitizePositions(gg.getPointPositions());
          const sx = Number.isFinite(cx) ? Math.max(-POS_CLAMP, Math.min(POS_CLAMP, cx)) : 0;
          const sy = Number.isFinite(cy) ? Math.max(-POS_CLAMP, Math.min(POS_CLAMP, cy)) : 0;
          try {
            // Camera ops only take while the render loop runs (see fitNow); resume
            // briefly, re-center + restore zoom, re-pin geometry, then pause.
            gg.unpause();
            gg.fitViewByPointPositions([sx, sy, sx, sy], 0);
            gg.setZoomLevel(snap.zoom, 0);
            gg.setPointPositions(frozen, true);
            gg.pause();
            refreshLabelsRef.current();
          } catch (err) {
            console.error("[graph-canvas] restoreCamera failed; recovering", err);
            try {
              gg.setPointPositions(frozen, true);
              gg.pause();
            } catch {
              /* unrecoverable */
            }
          }
        };
        // Apply after the full-graph positions have been pushed back. A short
        // delay lets the group-change re-layout effect commit the cached layout
        // first; we re-assert once more so the restore reliably wins.
        setTimeout(apply, 80);
        setTimeout(apply, 260);
      },
    }),
    [],
  );

  // ── fit the ego sub-graph once it settles (Fix #1548-3) ──────────────────────
  // When entering focus the node set shrank, so the data effect kicked a fresh
  // layout; ensure the camera fits the (now small) sub-graph so it FILLS the
  // viewport and far neighbors are reachable.
  const prevFocusViewRef = useRef(isFocusView);
  useEffect(() => {
    const entered = isFocusView && !prevFocusViewRef.current;
    prevFocusViewRef.current = isFocusView;
    if (!entered) return;
    const g = graphRef.current;
    if (!g) return;
    // The re-layout settles via the cap timer; fit a few times as positions land.
    const fit = () => {
      fitNowRef.current();
      refreshLabelsRef.current();
    };
    const t1 = setTimeout(fit, 350);
    const t2 = setTimeout(fit, 1100);
    const t3 = setTimeout(fit, 2200);
    return () => {
      clearTimeout(t1);
      clearTimeout(t2);
      clearTimeout(t3);
    };
  }, [isFocusView]);

  // ── Fix #1564-4: re-layout the ego sub-graph LIVE when the hops slider moves ──
  // While already in focus, dragging the hops slider swaps the ego node SET (but
  // `group` stays `…::ego`, so the group-change effect doesn't fire and the
  // settled engine would just re-pin the new scattered seed and pause). When the
  // node count changes inside focus, reset the settle flag, run a fresh layout
  // over the new set, and re-fit so the expanded/contracted sub-graph fills the
  // viewport.
  const prevEgoCountRef = useRef(nodes.length);
  useEffect(() => {
    const changed = prevEgoCountRef.current !== nodes.length;
    prevEgoCountRef.current = nodes.length;
    if (!isFocusView || !changed) return;
    const g = graphRef.current;
    if (!g) return;
    // Fix #1581: unified fresh settle for the new ego node set.
    kickFreshSettleRef.current();
    const capMs = Math.max(500, Math.min(6000, (simRef.current.settleTime ?? 2.0) * 1000));
    const fit = () => {
      fitNowRef.current();
      refreshLabelsRef.current();
    };
    const t1 = setTimeout(fit, 350);
    const t2 = setTimeout(fit, capMs + 200);
    return () => {
      clearTimeout(t1);
      clearTimeout(t2);
    };
  }, [nodes.length, isFocusView, packed]);

  return (
    <div className={`relative h-full w-full ${className}`} role="img" aria-label="Dependency graph">
      <div ref={containerRef} className="h-full w-full" />
      {/* #4605 — graceful empty-state when a deep-linked / focused node resolves to
          NO renderable nodes (synthetic or unknown id). We never feed this empty
          set to the WebGL engine (which would crash with `invalid texture shape`);
          we show this instead. */}
      {!renderable && (
        <div className="pointer-events-none absolute inset-0 z-20 grid place-items-center">
          <div className="pointer-events-auto max-w-sm rounded-lg border border-border bg-bg/80 px-6 py-5 text-center backdrop-blur">
            <div className="text-sm font-medium text-fg">Nothing to render for this node</div>
            <p className="mt-1 text-xs text-fg-muted">
              This node could not be resolved in the current graph, or it has no
              connections to display. Clear the selection or pick another node.
            </p>
          </div>
        </div>
      )}
      <div ref={labelLayerRef} aria-hidden className="pointer-events-none absolute inset-0 z-10" />
      {/* vignette for perceived depth */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background: isDark
            ? "radial-gradient(ellipse at 50% 50%, transparent 55%, rgba(2,6,23,0.55) 100%)"
            : "radial-gradient(ellipse at 50% 50%, transparent 55%, rgba(226,232,240,0.45) 100%)",
        }}
      />
    </div>
  );
}

export const GraphCanvas = memo(forwardRef(GraphCanvasInner));

// Re-export so callers can resolve the slate fallback consistently.
export { parseColor };
