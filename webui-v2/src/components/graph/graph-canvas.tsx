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

import { useRef, useEffect, useMemo, useCallback, memo } from "react";
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
const FIT_PADDING = 0.1;

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
  const R = Math.max(3000, Math.sqrt(nodes.length) * 50, N * 700);
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
  /** N-hop ego focus — hard-restrict to these ids (null = full graph). */
  focusNodeIds: Set<string> | null;
  /** changes to this nonce force a fresh re-layout (skip cache). */
  relayoutNonce: number;
  onNodeClick: (node: GraphNode | null) => void;
  onNodeHover: (node: GraphNode | null) => void;
  onSettled: () => void;
  className?: string;
}

const LABEL_COUNT = 24;
const truncate = (s: string) => (s.length > 30 ? s.slice(0, 28) + "…" : s);

function GraphCanvasInner({
  group,
  nodes,
  edges,
  selectedNodeId,
  isDark,
  colorMode,
  groupBy,
  simulation,
  nodeSizing,
  render,
  activeRepos,
  focusedCommunityId,
  focusNodeIds,
  relayoutNonce,
  onNodeClick,
  onNodeHover,
  onSettled,
  className = "",
}: GraphCanvasProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const graphRef = useRef<Graph | null>(null);
  const hasSettledRef = useRef(false);
  const capTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
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
      clusterStrength[i] = grouping ? 0.45 + normPR * 0.25 : 0.04 + normPR * 0.06;

      // log-scaled size by degree (graph.md: radius scales with PageRank/degree).
      sizes[i] =
        nodeSizing.baseSize + Math.log10((n.degree ?? 0) + 1) * nodeSizing.degreeScale;

      const gkey = groupKeyFor(n, groupBy);
      const center = grouping ? groupCenters.get(gkey) : undefined;
      const jitterR = Math.max(600, Math.sqrt(groupCount.get(gkey) ?? 1) * 40);
      const angle = Math.random() * 2 * Math.PI;
      const r = Math.random() * jitterR;
      positions[i * 2] = center ? center.x + r * Math.cos(angle) : (Math.random() - 0.5) * 4000;
      positions[i * 2 + 1] = center
        ? center.y + r * Math.sin(angle)
        : (Math.random() - 0.5) * 4000;
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
      } else {
        rgba = pastelAt(fill, (repoToIdx.get(n.repo ?? "") ?? 0) + 1);
      }
      writeNormalizedRGBA(out, i, rgba);
    }
    return out;
  }, [nodes, colorMode, repoToIdx, degreePercentile]);

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
    for (let i = 0; i < states.length; i++) {
      out[i] = (states[i] === 1 ? 2.2 : 0.6) * render.linkWidthScale;
    }
    return out;
  }, [linkData, render.showLinks, render.linkWidthScale]);

  // Top-N labels overlay.
  const topLabelIdx = useMemo(
    () =>
      nodes
        .map((n, i) => ({ i, d: n.degree ?? 0 }))
        .sort((a, b) => b.d - a.d)
        .slice(0, LABEL_COUNT)
        .map((x) => x.i),
    [nodes],
  );
  const labelLayerRef = useRef<HTMLDivElement>(null);
  const labelRafRef = useRef<number | null>(null);

  const refreshLabels = useCallback(() => {
    const g = graphRef.current;
    const layer = labelLayerRef.current;
    if (!g || !layer) return;
    const positions = g.getPointPositions();
    if (!positions || positions.length === 0) return;
    const w = containerRef.current?.clientWidth ?? 0;
    const h = containerRef.current?.clientHeight ?? 0;
    const frag: string[] = [];
    for (const idx of topLabelIdx) {
      const n = nodesRef.current[idx];
      if (!n) continue;
      const px = positions[idx * 2];
      const py = positions[idx * 2 + 1];
      if (px === undefined || py === undefined) continue;
      const [sx, sy] = g.spaceToScreenPosition([px, py]);
      if (sx < -50 || sy < -50 || sx > w + 50 || sy > h + 50) continue;
      frag.push(
        `<span style="position:absolute;left:${sx}px;top:${
          sy - 14
        }px;transform:translate(-50%,-100%);white-space:nowrap;font-size:11px;font-weight:500;padding:1px 5px;border-radius:4px;background:${
          isDark ? "rgba(2,6,23,0.72)" : "rgba(248,250,252,0.85)"
        };color:${isDark ? "#e2e8f0" : "#1e293b"}">${truncate(n.label)
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")}</span>`,
      );
    }
    layer.innerHTML = frag.join("");
  }, [topLabelIdx, isDark]);

  const scheduleLabels = useCallback(() => {
    if (labelRafRef.current !== null) return;
    labelRafRef.current = requestAnimationFrame(() => {
      labelRafRef.current = null;
      refreshLabels();
    });
  }, [refreshLabels]);

  const doSettle = useCallback(() => {
    if (hasSettledRef.current) return;
    hasSettledRef.current = true;
    if (capTimerRef.current) {
      clearTimeout(capTimerRef.current);
      capTimerRef.current = null;
    }
    const g = graphRef.current;
    g?.pause();
    g?.fitView(400, FIT_PADDING);
    scheduleLabels();
    setTimeout(scheduleLabels, 450);
    const positions = g?.getPointPositions();
    if (positions && positions.length > 0) {
      saveLayout(group, nodeIds, new Float32Array(positions));
    }
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
      renderHoveredPointRing: true,
      hoveredPointRingColor: isDark ? "#e2e8f0" : "#1e293b",
      pointSamplingDistance: 120,
      enableSimulation: true,
      simulationLinkSpring: simulation.linkSpring,
      simulationLinkDistance: Math.max(simulation.linkDistance, 8),
      simulationGravity: 0.02,
      simulationFriction: simulation.friction,
      simulationDecay: 1500,
      simulationCluster: 0.5,
      simulationRepulsion: simulation.repulsion,
      simulationCenter: simulation.center,
      rescalePositions: true,
      fitViewOnInit: true,
      fitViewDelay: 3500,
      fitViewPadding: FIT_PADDING,
      onSimulationEnd: () => {
        if (!hasSettledRef.current) doSettleRef.current();
      },
      onSimulationTick: scheduleLabels,
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
      onZoom: scheduleLabels,
    });
    graphRef.current = g;

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
      capTimerRef.current = setTimeout(() => {
        if (!hasSettledRef.current) doSettleRef.current();
      }, capMs);
    }

    return () => {
      if (capTimerRef.current) clearTimeout(capTimerRef.current);
      if (labelRafRef.current !== null) cancelAnimationFrame(labelRafRef.current);
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

  // ── visibility / focus selection ─────────────────────────────────────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g) return;
    const repoActive = activeRepos != null;
    const focusActive = focusNodeIds != null;
    const communityActive = focusedCommunityId != null;

    if (!repoActive && !focusActive && !communityActive) {
      g.selectPointsByIndices(null);
      g.setConfig({ pointGreyoutOpacity: 0.15 });
      return;
    }
    let effective: number[] = nodes
      .map((n, i) => {
        if (repoActive && !activeRepos!.has(n.repo)) return -1;
        if (communityActive && n.communityId !== focusedCommunityId) return -1;
        if (focusActive && !focusNodeIds!.has(n.id)) return -1;
        return i;
      })
      .filter((i) => i !== -1);
    g.selectPointsByIndices(effective);
    // hard-hide for repo/focus filters; soft-dim for community focus.
    g.setConfig({ pointGreyoutOpacity: repoActive || focusActive ? 0 : 0.18 });
  }, [nodes, activeRepos, focusNodeIds, focusedCommunityId]);

  // ── re-fit to focus ego-graph ────────────────────────────────────────────────
  useEffect(() => {
    const g = graphRef.current;
    if (!g || !hasSettledRef.current) return;
    if (focusNodeIds && focusNodeIds.size > 0) {
      const indices = Array.from(focusNodeIds)
        .map((id) => idToIdx.get(id))
        .filter((i): i is number => i !== undefined);
      if (indices.length > 0) g.fitViewByPointIndices(indices, 400, FIT_PADDING);
    } else {
      g.fitView(400, FIT_PADDING);
    }
    setTimeout(scheduleLabels, 450);
  }, [focusNodeIds, idToIdx, scheduleLabels]);

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

export const GraphCanvas = memo(GraphCanvasInner);

// Re-export so callers can resolve the slate fallback consistently.
export { parseColor };
