/* ============================================================
   components/graph/graph-jarvis-overlay.tsx — JARVIS SVG overlay (#1932).

   The cosmos.gl main graph is WebGL — it has no per-edge per-frame animation
   primitive and no native SVG layer. To get the JARVIS telemetry feel
   (#1932) we float an SVG layer above the WebGL canvas and re-project edge
   endpoints from the cosmograph camera (via graphCanvas.spaceToScreenPosition,
   exposed on the GraphCanvasHandle) every frame while motion is in flight.

   Surfaces (12 features, see #1932):
     • Chevrons   — static directional arrowheads on bridge edges + edges
                    incident to currently-highlighted nodes. Rendering every
                    chevron on a 37k-edge graph would tank the frame rate, so
                    we render the "interesting" subset that gives direction at
                    a glance without burning the GPU.
     • Trail tint — edges between consecutive MCP-replay hits are drawn as a
                    brighter stroke overlay (var(--ag-graph-accent)) while
                    replay-all is running. Reverse-scrub fades the tint off
                    in reverse order.
     • Comet     — the leading edge of the current step is drawn with a
                    travelling glow head + a short trailing tail; bridge
                    edges get a distinct accent + dashed pattern.
     • Bounce    — node arrival bumps point size via the canvas's existing
                    per-node size buffer (handled by the canvas, not here).

   Reduced motion: when prefers-reduced-motion is set, we skip the comet,
   pulse, and bounce. Chevrons + tinting stay (they're static styling).

   Audio is wired by the host (mcp-activity-overlay) via the controller's
   onArrive callback — this overlay is rendering-only.
   ============================================================ */

import { memo, useEffect, useRef, useState } from "react";
import type { GraphCanvasHandle } from "./graph-canvas";

// Shape of one MCP "step" in the replay timeline. A step lights up a NODE;
// the edge between step i-1 and step i is what the comet rides. When the two
// nodes are not directly connected we still draw a straight comet between
// them (visual cue that the agent jumped between investigations).
export interface JarvisStep {
  /** The node id the agent is "arriving at" on this step. */
  nodeId: string;
  /** Which MCP event index in the activity log this step came from. */
  eventIdx: number;
  /** Optional label (tool name) shown in scrubber hover. */
  label?: string;
}

export interface GraphJarvisOverlayProps {
  /** The graph canvas (used to resolve screen positions on each frame). */
  canvasHandle: GraphCanvasHandle | null;
  /** Replay timeline. */
  steps: JarvisStep[];
  /** Index of the step the comet is heading TO. -1 = idle. */
  currentTarget: number;
  /** 0..1 progress along the current edge. */
  edgeProgress: number;
  /** Edge indices already traversed (i.e. target step idx). */
  traversedEdges: ReadonlySet<number>;
  /** True while a comet is in flight (not paused / idle). */
  running: boolean;
  /** True while paused mid-flow (comet frozen). */
  paused: boolean;
  /** Disable comet + pulse + bounce (chevrons + tint stay). */
  reducedMotion?: boolean;
  /** Highlighted nodes (from useGraphHighlight) — drives chevron density. */
  highlightedNodeIds?: ReadonlySet<string>;
  className?: string;
}

// Size of the chevron path (in px at unit scale).
const CHEVRON = 9;
// Cap the number of "incident" chevrons we render so a huge graph stays smooth.
const CHEVRON_BUDGET = 600;

/**
 * Re-project the screen position of a list of node ids via the canvas handle.
 * Returns a Map keyed by id so callers can do O(1) lookups.
 */
function projectNodes(
  handle: GraphCanvasHandle | null,
  ids: ReadonlySet<string>,
): Map<string, { x: number; y: number }> {
  const out = new Map<string, { x: number; y: number }>();
  if (!handle) return out;
  for (const id of ids) {
    const p = handle.getNodeScreenPosition(id);
    if (p) out.set(id, p);
  }
  return out;
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}

export const GraphJarvisOverlay = memo(function GraphJarvisOverlay({
  canvasHandle,
  steps,
  currentTarget,
  edgeProgress,
  traversedEdges,
  running,
  paused,
  reducedMotion = false,
  highlightedNodeIds,
  className = "",
}: GraphJarvisOverlayProps) {
  // We re-render the overlay on a rAF loop while ANY of:
  //   • replay is running (comet must move),
  //   • replay is paused (comet frozen, but viewport may still drag),
  //   • user is interacting with the cosmos camera (pan/zoom → chevrons follow),
  //   • highlightedNodeIds is non-empty (recent MCP glow → chevrons follow).
  // The cheap way to track viewport motion without wiring into cosmos's event
  // bus is to compare the screen position of an anchor node each frame and
  // re-render when it moves. Even simpler: just tick rAF whenever anything
  // visible could move. The overlay's render cost is tiny (a few SVGs).
  const [tick, setTick] = useState(0);
  const rafRef = useRef<number | null>(null);
  useEffect(() => {
    const shouldTick =
      !!canvasHandle &&
      (running || paused || (highlightedNodeIds && highlightedNodeIds.size > 0));
    if (!shouldTick) return;
    let stopped = false;
    const loop = () => {
      if (stopped) return;
      setTick((t) => (t + 1) & 0x3fffffff);
      rafRef.current = requestAnimationFrame(loop);
    };
    rafRef.current = requestAnimationFrame(loop);
    return () => {
      stopped = true;
      if (rafRef.current != null) cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    };
  }, [canvasHandle, running, paused, highlightedNodeIds]);

  // Re-render once on mount + once on any interactive event so chevrons follow
  // user pan/zoom even outside an active replay. We listen on `pointerup` /
  // `wheel` at the window level; the cosmos canvas swallows but the events
  // still bubble in capture phase.
  useEffect(() => {
    const onInteract = () => setTick((t) => (t + 1) & 0x3fffffff);
    window.addEventListener("pointermove", onInteract, { passive: true });
    window.addEventListener("wheel", onInteract, { passive: true, capture: true });
    window.addEventListener("resize", onInteract);
    return () => {
      window.removeEventListener("pointermove", onInteract);
      window.removeEventListener("wheel", onInteract, { capture: true } as EventListenerOptions);
      window.removeEventListener("resize", onInteract);
    };
  }, []);

  // ── compute current frame geometry ──────────────────────────────────────
  // tick is consumed via the dependency on the render itself; eslint can't see
  // the indirection so explicitly reference it.
  void tick;

  if (!canvasHandle) return null;

  // Project the union of: every step node + every highlighted node.
  const ids = new Set<string>();
  for (const s of steps) ids.add(s.nodeId);
  if (highlightedNodeIds) for (const id of highlightedNodeIds) ids.add(id);
  const projected = projectNodes(canvasHandle, ids);

  // Collect bridge edges from the canvas handle (one-time per render).
  const edges = canvasHandle.getEdgeList();
  // Chevron candidate set: bridge edges + edges incident to a highlighted node.
  const chevronEdges: { src: string; tgt: string; bridge: boolean }[] = [];
  const hi = highlightedNodeIds;
  for (const e of edges) {
    const incident = hi && (hi.has(e.src) || hi.has(e.tgt));
    if (e.bridge || incident) chevronEdges.push(e);
    if (chevronEdges.length >= CHEVRON_BUDGET) break;
  }

  // Trail (traversed) edge geometry: for each traversed step index i, the
  // edge i-1 → i. Both endpoints must be projected.
  const trail: Array<{
    x1: number; y1: number; x2: number; y2: number; bridge: boolean;
  }> = [];
  for (const ti of traversedEdges) {
    if (ti <= 0 || ti >= steps.length) continue;
    const a = projected.get(steps[ti - 1].nodeId);
    const b = projected.get(steps[ti].nodeId);
    if (!a || !b) continue;
    const bridge = canvasHandle.isBridgeEdge(steps[ti - 1].nodeId, steps[ti].nodeId);
    trail.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y, bridge });
  }

  // Comet geometry: for the current target edge.
  let comet: {
    x1: number; y1: number; x2: number; y2: number;
    headX: number; headY: number; bridge: boolean;
  } | null = null;
  if ((running || paused) && !reducedMotion && currentTarget > 0 && currentTarget < steps.length) {
    const a = projected.get(steps[currentTarget - 1].nodeId);
    const b = projected.get(steps[currentTarget].nodeId);
    if (a && b) {
      const bridge = canvasHandle.isBridgeEdge(
        steps[currentTarget - 1].nodeId,
        steps[currentTarget].nodeId,
      );
      comet = {
        x1: a.x,
        y1: a.y,
        x2: b.x,
        y2: b.y,
        headX: lerp(a.x, b.x, edgeProgress),
        headY: lerp(a.y, b.y, edgeProgress),
        bridge,
      };
    }
  }

  const accent = "var(--ag-graph-accent, #60a5fa)";
  const bridgeAccent = "var(--ag-graph-bridge-accent, #a78bfa)";

  return (
    <svg
      className={`pointer-events-none absolute inset-0 ${className}`}
      style={{ overflow: "visible" }}
      role="presentation"
      aria-hidden
      data-testid="graph-jarvis-overlay"
    >
      <defs>
        {/* Chevron marker — regular edges. */}
        <marker
          id="ag-graph-chev"
          viewBox="0 0 10 10"
          markerWidth={CHEVRON}
          markerHeight={CHEVRON}
          refX="9"
          refY="5"
          orient="auto"
        >
          <path d="M 0 0 L 9 5 L 0 10 z" fill="var(--ag-graph-edge, #475569)" opacity="0.65" />
        </marker>
        {/* Chevron marker — bridge edges (distinct accent). */}
        <marker
          id="ag-graph-chev-bridge"
          viewBox="0 0 10 10"
          markerWidth={CHEVRON + 1}
          markerHeight={CHEVRON + 1}
          refX="9"
          refY="5"
          orient="auto"
        >
          <path d="M 0 0 L 9 5 L 0 10 z" fill={bridgeAccent} opacity="0.85" />
        </marker>
        {/* Chevron marker — accent (on traversed / comet edges). */}
        <marker
          id="ag-graph-chev-accent"
          viewBox="0 0 10 10"
          markerWidth={CHEVRON + 1}
          markerHeight={CHEVRON + 1}
          refX="9"
          refY="5"
          orient="auto"
        >
          <path d="M 0 0 L 9 5 L 0 10 z" fill={accent} />
        </marker>
        <filter id="ag-graph-comet-glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="2.4" />
        </filter>
      </defs>

      {/* ── Static chevrons (bridge + incident edges) ─────────────────────── */}
      {chevronEdges.map((e, i) => {
        const a = projected.get(e.src);
        const b = projected.get(e.tgt);
        if (!a || !b) return null;
        // Only draw if endpoints span a reasonable on-screen distance, so we
        // don't pile chevrons on top of each other for collapsed clusters.
        const dx = b.x - a.x;
        const dy = b.y - a.y;
        if (Math.hypot(dx, dy) < 20) return null;
        // Place the chevron MID-edge (path-with-marker-mid). Use a 3-point
        // path so SVG draws the marker at the middle vertex with the correct
        // orientation. Stroke is invisible — we just want the marker.
        const mx = (a.x + b.x) / 2;
        const my = (a.y + b.y) / 2;
        return (
          <path
            key={`chev-${i}-${e.src}-${e.tgt}`}
            d={`M ${a.x} ${a.y} L ${mx} ${my} L ${b.x} ${b.y}`}
            fill="none"
            stroke={e.bridge ? bridgeAccent : "transparent"}
            strokeWidth={e.bridge ? 0.85 : 0}
            strokeDasharray={e.bridge ? "4 3" : undefined}
            strokeLinecap="round"
            markerMid={e.bridge ? "url(#ag-graph-chev-bridge)" : "url(#ag-graph-chev)"}
            opacity={e.bridge ? 0.85 : 0.55}
          />
        );
      })}

      {/* ── Trail tint (traversed edges, current replay only) ───────────── */}
      {trail.map((t, i) => (
        <line
          key={`trail-${i}`}
          x1={t.x1}
          y1={t.y1}
          x2={t.x2}
          y2={t.y2}
          stroke={t.bridge ? bridgeAccent : accent}
          strokeWidth={t.bridge ? 2.2 : 1.6}
          strokeLinecap="round"
          strokeDasharray={t.bridge ? "5 3" : undefined}
          opacity={0.55}
          markerEnd="url(#ag-graph-chev-accent)"
        />
      ))}

      {/* ── Comet (current in-flight edge) ───────────────────────────────── */}
      {comet ? (
        <g>
          {/* Base stroke under the comet so the edge reads as a path even when
              there's no real graph edge between the two MCP hits. */}
          <line
            x1={comet.x1}
            y1={comet.y1}
            x2={comet.x2}
            y2={comet.y2}
            stroke={comet.bridge ? bridgeAccent : accent}
            strokeWidth={comet.bridge ? 1.6 : 1.2}
            strokeDasharray={comet.bridge ? "5 3" : undefined}
            strokeLinecap="round"
            opacity={0.45}
          />
          {/* Comet head: bright glowing dot at edgeProgress. */}
          <circle
            cx={comet.headX}
            cy={comet.headY}
            r={comet.bridge ? 4 : 3.4}
            fill={comet.bridge ? bridgeAccent : accent}
            filter="url(#ag-graph-comet-glow)"
          />
          <circle
            cx={comet.headX}
            cy={comet.headY}
            r={comet.bridge ? 2 : 1.7}
            fill="#ffffff"
            opacity="0.9"
          />
          {/* Edge-pulse halo when nearly arrived (last ~10% of progress). */}
          {edgeProgress > 0.9 ? (
            <circle
              cx={comet.x2}
              cy={comet.y2}
              r={9 * (1 - (1 - edgeProgress) * 10)}
              fill="none"
              stroke={comet.bridge ? bridgeAccent : accent}
              strokeWidth={2}
              opacity={(1 - edgeProgress) * 10}
            />
          ) : null}
        </g>
      ) : null}
    </svg>
  );
});
