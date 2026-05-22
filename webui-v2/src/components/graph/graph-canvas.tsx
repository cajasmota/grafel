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
  CROSS_REPO_EDGE,
  SAME_REPO_EDGE,
} from "@/lib/graph-colors";
import { saveLayout, loadLayout } from "@/lib/graph-layout-cache";
import type {
  ColorMode,
  GroupByMode,
  SimulationConfig,
  NodeSizingConfig,
  RenderConfig,
} from "@/store/use-graph-store";

const SPACE_SIZE = 32768;
// Fix #1532-3 / #1548-2: a small padding makes the settled graph FILL the
// canvas instead of floating small in the middle. A touch more than 0.04 so
// the outermost clusters/labels aren't clipped at the viewport edge.
const FIT_PADDING = 0.08;

// ── group / cluster helpers ──────────────────────────────────────────────────

function moduleKey(sourceFile: string): string {
  if (!sourceFile) return "";
  const parts = sourceFile.replace(/\\/g, "/").split("/");
  return parts.slice(0, -1).slice(-2).join("/");
}

function hashMod1000(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = ((h << 5) - h + s.charCodeAt(i)) | 0;
  return Math.abs(h) % 1000;
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

function clusterIdFor(n: GraphNode, repoIdx: number, mode: GroupByMode): number | undefined {
  if (mode === "none") return undefined;
  if (mode === "repo") {
    const mod = hashMod1000(moduleKey(n.sourceFile));
    const cid = n.communityId ?? 0;
    return repoIdx * 10_000_000 + cid * 1000 + mod;
  }
  const k = groupKeyFor(n, mode);
  return hashMod1000(k) + hashMod1000(k + "#") * 1000;
}

function buildGroupCenters(
  nodes: GraphNode[],
  mode: GroupByMode,
): Map<string, { x: number; y: number }> {
  if (mode === "none") return new Map();
  const keys = Array.from(new Set(nodes.map((n) => groupKeyFor(n, mode)))).sort();
  const N = keys.length;
  if (N === 0) return new Map();
  // Fix #1558-2: the ring radius still scaled with the GROUP COUNT (N*150) which,
  // on a many-module monorepo, flung the clusters into a wide hollow ring with an
  // empty middle. The ring is now only a SEEDING hint — keep it tight so the
  // initial spokes start close to center, then let the link-spring + center
  // gravity pull connected modules inward to fill the canvas. Scale with node
  // count (not group count), so adding more groups no longer expands the ring.
  const R = Math.min(1400, Math.max(450, Math.sqrt(nodes.length) * 28));
  return new Map(
    keys.map((key, i) => {
      const angle = (i / N) * 2 * Math.PI;
      return [key, { x: R * Math.cos(angle), y: R * Math.sin(angle) }];
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
  className?: string;
}

// Labels (Fix #1532-5) are zoom/LOD-gated: a small always-on set of the
// highest-degree hubs (plus the hovered node), with progressively more labels
// revealed as the user zooms in — readable, not cluttered.
const LABEL_BASE_COUNT = 12; // shown at the default (zoomed-out) level
const LABEL_MAX_COUNT = 160; // ceiling once fully zoomed in
// Fix #1548-1: during continuous pan/zoom we drive labels from the camera every
// frame (rAF). Label compute is heavy, so while interacting we render only the
// top-N hubs; the full zoom-gated set is restored on interaction-end.
const LABEL_MOTION_CAP = 24;
const truncate = (s: string) => (s.length > 30 ? s.slice(0, 28) + "…" : s);

/**
 * Imperative handle (Fix #1548-3): the parent snapshots the camera (zoom + pan)
 * on focus-enter and restores it on focus-exit, so the user returns to exactly
 * the view they left.
 */
export interface GraphCanvasHandle {
  snapshotCamera: () => void;
  restoreCamera: () => void;
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
  const hoveredRef = useRef<string | null>(hoveredNodeId);
  hoveredRef.current = hoveredNodeId;
  const relayoutRef = useRef(relayoutNonce);

  const idToIdx = useMemo(() => {
    const m = new Map<string, number>();
    nodes.forEach((n, i) => m.set(n.id, i));
    return m;
  }, [nodes]);

  const nodeIds = useMemo(() => nodes.map((n) => n.id), [nodes]);

  const repoToIdx = useMemo(() => {
    const repos = Array.from(new Set(nodes.map((n) => n.repo ?? ""))).sort();
    return new Map(repos.map((r, i) => [r, i]));
  }, [nodes]);

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

    const grouping = groupBy !== "none";
    const groupCount = new Map<string, number>();
    for (const n of nodes) {
      const k = groupKeyFor(n, groupBy);
      groupCount.set(k, (groupCount.get(k) ?? 0) + 1);
    }

    nodes.forEach((n, i) => {
      const repoIdx = repoToIdx.get(n.repo ?? "") ?? 0;
      clusters[i] = clusterIdFor(n, repoIdx, groupBy);
      const normPR = (n.pageRank ?? 0) / maxPR;
      // Fix #1558-2: lower the per-node cluster pull so it nudges (rather than
      // pins) nodes to their group center; the cross-module link-spring then wins
      // where it matters, drawing connected modules together (was 0.45 + .25).
      clusterStrength[i] = grouping ? 0.22 + normPR * 0.15 : 0.04 + normPR * 0.06;

      // log-scaled size by degree (graph.md: radius scales with PageRank/degree),
      // hard-capped at maxMultiplier × base so high-degree hubs never bloom into
      // overlapping blobs. (Fix #1532-4)
      const raw =
        nodeSizing.baseSize + Math.log10((n.degree ?? 0) + 1) * nodeSizing.degreeScale;
      sizes[i] = Math.min(raw, nodeSizing.baseSize * nodeSizing.maxMultiplier);

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

    return { positions, sizes, clusters, clusterStrength };
  }, [nodes, repoToIdx, groupCenters, groupBy, nodeSizing]);

  // Node colors — re-read pastel scale from tokens.css so theme flows through.
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
      writeNormalizedRGBA(out, i, rgba);
    }
    return out;
  }, [nodes, colorMode, repoToIdx, moduleToIdx, degreePercentile]);

  // Links — packed [src,tgt] + cross-repo state.
  const linkData = useMemo(() => {
    const idToRepo = new Map(nodes.map((n) => [n.id, n.repo ?? ""]));
    const src: number[] = [];
    const tgt: number[] = [];
    const states: number[] = []; // 1 cross-repo, 0 same-repo
    for (const e of edges) {
      const s = idToIdx.get(e.source);
      const t = idToIdx.get(e.target);
      if (s === undefined || t === undefined) continue;
      const cross = idToRepo.get(e.source) !== idToRepo.get(e.target);
      src.push(s);
      tgt.push(t);
      states.push(cross ? 1 : 0);
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
    const sameA = render.linkOpacity;
    const crossA = Math.min(1, Math.max(0.55, render.linkOpacity * 3.5));
    for (let i = 0; i < states.length; i++) {
      const base = states[i] === 1 ? CROSS_REPO_EDGE : SAME_REPO_EDGE;
      const rgba: RGBA = [base[0], base[1], base[2], states[i] === 1 ? crossA : sameA];
      writeNormalizedRGBA(out, i, rgba);
    }
    return out;
  }, [linkData, render.linkOpacity]);

  const packLinkWidths = useCallback((): Float32Array => {
    const { states } = linkData;
    const out = new Float32Array(states.length);
    if (!render.showLinks) return out;
    // Fix #1558-1: links must stay visible at EVERY zoom level — especially the
    // long cross-module "bridge" links between islands, which previously thinned
    // to sub-pixel and vanished when zoomed out. `scaleLinksOnZoom` is off so the
    // width is a constant on-screen pixel value; we floor every link at a
    // perceptible minimum (cross-module links a touch heavier so the bridges read
    // first) and never let linkWidthScale push them below that floor.
    const MIN_SAME = 1.1;
    const MIN_CROSS = 2.4;
    for (let i = 0; i < states.length; i++) {
      const cross = states[i] === 1;
      const base = cross ? 2.6 : 1.0;
      const floor = cross ? MIN_CROSS : MIN_SAME;
      out[i] = Math.max(floor, base * render.linkWidthScale);
    }
    return out;
  }, [linkData, render.showLinks, render.linkWidthScale]);

  // Nodes ranked by degree (descending) — the highest-degree hubs are labelled
  // first; the count revealed grows with zoom level. (Fix #1532-5)
  const rankedByDegree = useMemo(
    () =>
      nodes
        .map((n, i) => ({ i, d: n.degree ?? 0 }))
        .sort((a, b) => b.d - a.d)
        .map((x) => x.i),
    [nodes],
  );
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

  const refreshLabels = useCallback((motion = false) => {
    const g = graphRef.current;
    const layer = labelLayerRef.current;
    if (!g || !layer) return;
    const positions = g.getPointPositions();
    if (!positions || positions.length === 0) return;
    const w = containerRef.current?.clientWidth ?? 0;
    const h = containerRef.current?.clientHeight ?? 0;

    // Zoom-gated count: at the fitted level show only the top hubs; reveal more
    // (up to a ceiling) the further the user zooms in.
    let zoom = 1;
    try {
      zoom = g.getZoomLevel() || 1;
    } catch {
      /* engine not ready */
    }
    const factor = Math.max(1, Math.pow(Math.max(zoom, 0.0001) * 3.2, 1.4));
    // Fix #1548-1: while panning/zooming, cap to the top hubs so the per-frame
    // label compute stays cheap and tracks the nodes smoothly with no lag.
    const idleCount = Math.min(
      LABEL_MAX_COUNT,
      rankedByDegree.length,
      Math.round(LABEL_BASE_COUNT * factor),
    );
    const count = motion ? Math.min(idleCount, LABEL_MOTION_CAP) : idleCount;

    const shown = new Set<number>(rankedByDegree.slice(0, count));
    // Always include the hovered node, even if it's a low-degree leaf.
    const hovId = hoveredRef.current;
    const hovIdx = hovId != null ? idToIdx.get(hovId) : undefined;
    if (hovIdx !== undefined) shown.add(hovIdx);

    const frag: string[] = [];
    for (const idx of shown) {
      const n = nodesRef.current[idx];
      if (!n) continue;
      const px = positions[idx * 2];
      const py = positions[idx * 2 + 1];
      if (px === undefined || py === undefined) continue;
      const [sx, sy] = g.spaceToScreenPosition([px, py]);
      if (sx < -50 || sy < -50 || sx > w + 50 || sy > h + 50) continue;
      frag.push(labelSpan(sx, sy, n.label, idx === hovIdx));
    }
    layer.innerHTML = frag.join("");
  }, [rankedByDegree, idToIdx, isDark]);

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
  const rafRef = useRef<number | null>(null);
  const motionLoop = useCallback(() => {
    if (!interactingRef.current) {
      rafRef.current = null;
      return;
    }
    refreshLabelsRef.current(true); // motion=true → capped count, cheap
    rafRef.current = requestAnimationFrame(motionLoop);
  }, []);
  const startInteraction = useCallback(() => {
    interactingRef.current = true;
    if (rafRef.current === null) rafRef.current = requestAnimationFrame(motionLoop);
  }, [motionLoop]);
  const endInteraction = useCallback(() => {
    interactingRef.current = false;
    if (rafRef.current !== null) {
      cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
    // Restore the full zoom-gated label set now that motion has stopped.
    requestAnimationFrame(() => refreshLabelsRef.current(false));
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
    const wasSettled = hasSettledRef.current;
    const frozen = wasSettled ? new Float32Array(g.getPointPositions()) : null;
    g.unpause();
    if (indices && indices.length > 0) g.fitViewByPointIndices(indices, 0, FIT_PADDING);
    else g.fitView(0, FIT_PADDING);
    if (frozen) {
      g.setPointPositions(frozen, true);
      g.pause();
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
    const positions = g.getPointPositions();
    if (positions && positions.length > 0) {
      saveLayout(group, nodeIds, new Float32Array(positions));
    }

    // Fix #1548-2: cosmos.gl `fitView` is a no-op while the render loop is
    // paused — that was why the settled graph floated off-center and didn't
    // FILL the viewport. We freeze the geometry (re-pin the settled positions)
    // and pause the physics FIRST, then use the fitNow helper which briefly
    // resumes the render loop to apply an INSTANT fit and pauses again — so the
    // fit lands deterministically on the final geometry with no drift.
    g.setPointPositions(new Float32Array(positions), true);
    g.pause();

    if (suppressFitRef.current) {
      // Exiting focus: skip the fit (restoreCamera owns the view).
      suppressFitRef.current = false;
      scheduleLabels();
      onSettledRef.current();
      return;
    }

    fitNowRef.current();
    scheduleLabels();
    // One more fit on the next frames in case the canvas size settled late.
    setTimeout(() => {
      fitNowRef.current();
      scheduleLabels();
    }, 200);
    onSettledRef.current();
  }, [group, nodeIds, scheduleLabels]);
  const doSettleRef = useRef(doSettle);
  doSettleRef.current = doSettle;

  // ── mount the engine ONCE ─────────────────────────────────────────────────
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const g = new Graph(container, {
      backgroundColor: isDark ? "#020617" : "#f8fafc",
      spaceSize: SPACE_SIZE,
      pixelRatio: Math.min(window.devicePixelRatio, 1.5),
      scalePointsOnZoom: render.scalePointsOnZoom,
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
      // Fix #1548-2: cosmos.gl fades links by their ON-SCREEN length
      // (linkVisibilityDistanceRange, in px) and caps far-link alpha at
      // linkVisibilityMinTransparency. The defaults ([50,150] / 0.25) made
      // every link nearly invisible at the fitted (zoomed-out) level — links
      // only "appeared" after zooming/settling. Widen the visibility floor and
      // raise the min transparency so links read clearly from the first paint
      // at any zoom.
      linkVisibilityDistanceRange: [1, 10000],
      linkVisibilityMinTransparency: 0.8,
      renderHoveredPointRing: true,
      hoveredPointRingColor: isDark ? "#e2e8f0" : "#1e293b",
      pointSamplingDistance: 120,
      enableSimulation: true,
      simulationLinkSpring: simulation.linkSpring,
      simulationLinkDistance: Math.max(simulation.linkDistance, 8),
      // Fix #1558-2: more gravity (was 0.02) reinforces the center pull so
      // disconnected islands drift inward instead of sitting on the rim, filling
      // the canvas rather than leaving an empty middle.
      simulationGravity: 0.35,
      simulationFriction: simulation.friction,
      // Fix #1558-2: a slower cool-down (was 1500) keeps the simulation active
      // long enough for the strong center + gravity to pull the islands inward
      // before it freezes — otherwise it ended early at the spread state and the
      // graph settled as a hollow ring. The ≤settleTime wall-clock cap still
      // bounds total settle time.
      simulationDecay: 3000,
      // Fix #1558-2: the cluster force pinned every node hard to its ring center,
      // overriding the cross-module link-spring and locking in the hollow ring.
      // Soften it so groups still coalesce locally but connected modules are free
      // to be pulled together by their shared edges (was 0.5).
      simulationCluster: 0.2,
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
        if (!interactingRef.current) scheduleLabelsLive();
      },
      onZoomEnd: () => endInteractionRef.current(),
      onDragStart: () => startInteractionRef.current(),
      onDragEnd: () => endInteractionRef.current(),
    });
    graphRef.current = g;
    if (import.meta.env.DEV) (window as unknown as { __ag?: Graph }).__ag = g;

    const saved = loadLayout(group, nodeIds);
    if (saved) {
      requestAnimationFrame(() => {
        const gg = graphRef.current;
        if (!gg) return;
        gg.setPointPositions(saved.positions, true);
        doSettleRef.current();
      });
    } else {
      const capMs = Math.max(500, Math.min(6000, (simRef.current.settleTime ?? 2.0) * 1000));
      // Fix #1558-2: a single alpha=1 pass from the (spread) seed cools down
      // before the strong center/gravity finish pulling the islands inward — the
      // graph froze as a hollow ring. Re-heat the simulation once partway through
      // the settle window so it keeps converging on the now-partially-collapsed
      // positions, reliably reaching the canvas-filling layout (this mirrors the
      // manual second `start()` that was needed to converge). Still bounded by the
      // settleTime cap below.
      reheatedRef.current = false;
      reheatTimerRef.current = setTimeout(() => {
        const gg = graphRef.current;
        reheatedRef.current = true;
        if (gg && !hasSettledRef.current) gg.start(1);
      }, Math.round(capMs * 0.45));
      capTimerRef.current = setTimeout(() => {
        if (!hasSettledRef.current) doSettleRef.current();
      }, capMs);
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
    const prev = g.getPointPositions();
    if (hasSettledRef.current && prev.length === packed.positions.length) {
      g.setPointPositions(new Float32Array(prev), true);
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
      if (!saved) {
        didAutoStartRef.current = true;
        g.start(1);
      }
    }
    if (hasSettledRef.current) g.pause();
    scheduleLabels();
  }, [packed, packPointColors, linkData, packLinkColors, packLinkWidths, group, nodeIds, scheduleLabels]);

  // ── recolor on theme / colorMode ────────────────────────────────────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    g.setPointColors(packPointColors());
    g.setLinkColors(packLinkColors());
    g.setLinkWidths(packLinkWidths());
    g.setConfig({ backgroundColor: isDark ? "#020617" : "#f8fafc" });
    g.render();
    g.create();
    if (hasSettledRef.current) g.pause();
  }, [packPointColors, packLinkColors, packLinkWidths, isDark]);

  // ── live render/sim config ──────────────────────────────────────────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    g.setConfig({
      pointOpacity: render.pointOpacity,
      pointSizeScale: render.pointSizeScale,
      scalePointsOnZoom: render.scalePointsOnZoom,
      linkWidthScale: render.showLinks ? render.linkWidthScale : 0,
      simulationLinkSpring: simulation.linkSpring,
      simulationLinkDistance: Math.max(simulation.linkDistance, 8),
      simulationFriction: simulation.friction,
      simulationRepulsion: simulation.repulsion,
      simulationCenter: simulation.center,
    });
    g.setLinkColors(packLinkColors());
    g.setLinkWidths(packLinkWidths());
    g.render();
    if (hasSettledRef.current) g.pause();
  }, [render, simulation, packLinkColors, packLinkWidths]);

  // ── re-layout request (Reset / re-layout) ───────────────────────────────────
  useEffect(() => {
    if (relayoutRef.current === relayoutNonce) return;
    relayoutRef.current = relayoutNonce;
    const g = graphRef.current;
    if (!g) return;
    hasSettledRef.current = false;
    mountTimeRef.current = Date.now();
    g.setPointPositions(packed.positions);
    g.setPointClusters(packed.clusters);
    g.setPointClusterStrength(packed.clusterStrength);
    g.render();
    g.create();
    g.start(1);
    const capMs = Math.max(500, Math.min(6000, (simRef.current.settleTime ?? 2.0) * 1000));
    if (capTimerRef.current) clearTimeout(capTimerRef.current);
    capTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current();
    }, capMs);
  }, [relayoutNonce, packed]);

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
    hasSettledRef.current = false;
    didAutoStartRef.current = true;
    mountTimeRef.current = Date.now();
    const saved = loadLayout(group, nodeIds);
    if (saved && saved.positions.length === packed.positions.length) {
      g.setPointPositions(saved.positions, true);
      g.render();
      g.create();
      doSettleRef.current();
      return;
    }
    g.setPointPositions(packed.positions);
    g.setPointClusters(packed.clusters);
    g.setPointClusterStrength(packed.clusterStrength);
    g.render();
    g.create();
    g.start(1);
    const capMs = Math.max(500, Math.min(6000, (simRef.current.settleTime ?? 2.0) * 1000));
    if (capTimerRef.current) clearTimeout(capTimerRef.current);
    capTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current();
    }, capMs);
  }, [group, packed, nodeIds]);

  // ── re-cluster on group-by change ───────────────────────────────────────────
  const prevGroupByRef = useRef(groupBy);
  useEffect(() => {
    if (prevGroupByRef.current === groupBy) return;
    prevGroupByRef.current = groupBy;
    const g = graphRef.current;
    if (!g) return;
    hasSettledRef.current = false;
    mountTimeRef.current = Date.now();
    g.setPointPositions(packed.positions);
    g.setPointClusters(packed.clusters);
    g.setPointClusterStrength(packed.clusterStrength);
    g.render();
    g.create();
    g.start(1);
    const capMs = Math.max(500, Math.min(6000, (simRef.current.settleTime ?? 2.0) * 1000));
    if (capTimerRef.current) clearTimeout(capTimerRef.current);
    capTimerRef.current = setTimeout(() => {
      if (!hasSettledRef.current) doSettleRef.current();
    }, capMs);
  }, [groupBy, packed]);

  // ── visibility / repo + community selection ──────────────────────────────────
  // Fix #1548-3: ego focus is no longer a greyout — the parent passes a pre-built
  // ego SUB-graph as nodes/edges, so here we only handle repo filter (hard-hide)
  // and community focus (soft-dim).
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    const repoActive = activeRepos != null;
    const communityActive = focusedCommunityId != null;

    if (!repoActive && !communityActive) {
      g.selectPointsByIndices(null);
      g.setConfig({ pointGreyoutOpacity: 0.15 });
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
    g.setConfig({ pointGreyoutOpacity: repoActive ? 0 : 0.18 });
  }, [nodes, activeRepos, focusedCommunityId]);

  // ── refresh labels when the hovered node changes (Fix #1532-5) ───────────────
  useEffect(() => {
    scheduleLabels();
  }, [hoveredNodeId, scheduleLabels]);

  // Once node data is available, kick a few delayed label refreshes so the
  // labels appear after the engine has positions — the mount-time settle may
  // have scheduled a refresh while the node list was still empty. (Fix #1532-5)
  useEffect(() => {
    if (rankedByDegree.length === 0) return;
    const t1 = setTimeout(scheduleLabels, 300);
    const t2 = setTimeout(scheduleLabels, 1200);
    const t3 = setTimeout(scheduleLabels, 2600);
    return () => {
      clearTimeout(t1);
      clearTimeout(t2);
      clearTimeout(t3);
    };
  }, [rankedByDegree, scheduleLabels]);

  // ── Fix #1548-3: camera snapshot / restore for ego focus enter / exit ─────────
  // cosmos.gl exposes no public pan setter, so we capture (a) the zoom level and
  // (b) the space coordinate currently at the viewport center. To restore we
  // re-center on that point (degenerate fitViewByPointPositions box) then set the
  // recorded zoom — together that reproduces the prior pan + zoom exactly.
  const cameraSnapRef = useRef<{ zoom: number; center: [number, number] } | null>(null);
  useImperativeHandle(
    ref,
    () => ({
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
          const frozen = new Float32Array(gg.getPointPositions());
          // Camera ops only take while the render loop runs (see fitNow); resume
          // briefly, re-center + restore zoom, re-pin geometry, then pause.
          gg.unpause();
          gg.fitViewByPointPositions([cx, cy, cx, cy], 0);
          gg.setZoomLevel(snap.zoom, 0);
          gg.setPointPositions(frozen, true);
          gg.pause();
          refreshLabelsRef.current(false);
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
      refreshLabelsRef.current(false);
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

  return (
    <div className={`relative h-full w-full ${className}`} role="img" aria-label="Dependency graph">
      <div ref={containerRef} className="h-full w-full" />
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
