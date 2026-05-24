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

// Shape of one MCP "step" in the replay timeline (#1953 two-phase model).
//
// One step == one MCP CALL. Phase 1 sweeps a phantom arrow along the entire
// `nodeIds` polyline; Phase 2 pulses every node in `nodeIds` simultaneously.
//
// `nodeId` is retained as a convenience (= first node, used for legacy
// labelling like the scrubber tooltip) but the renderer should iterate
// `nodeIds` for both sweep and glow.
export interface JarvisStep {
  /** The PRIMARY node id (first hit) — kept for legacy label sites. */
  nodeId: string;
  /** All returned node ids for this call, in order. Drives sweep + glow. */
  nodeIds: string[];
  /** Which MCP event index in the activity log this step came from. */
  eventIdx: number;
  /** Optional label (tool name) shown in scrubber hover. */
  label?: string;
}

export interface GraphJarvisOverlayProps {
  /** The graph canvas (used to resolve screen positions on each frame). */
  canvasHandle: GraphCanvasHandle | null;
  /** Replay timeline (one entry per MCP call). */
  steps: JarvisStep[];
  /** Index of the MCP call in flight. -1 = idle. */
  currentTarget: number;
  /** 0..1 progress of the Phase 1 sweep along the call's polyline. */
  edgeProgress: number;
  /** Completed call indices (drives trail tint). */
  traversedEdges: ReadonlySet<number>;
  /** True while a replay is in flight (not paused / idle). */
  running: boolean;
  /** True while paused mid-flow. */
  paused: boolean;
  /** #1953 — current phase of the in-flight call. */
  phase?: "idle" | "sweep" | "glow" | "gap";
  /** #1953 — 0..1 decay of the Phase 2 glow burst. */
  glowProgress?: number;
  /** Disable Phase 1 sweep + size animation (Phase 2 glow still fires statically). */
  reducedMotion?: boolean;
  /** Highlighted nodes (from useGraphHighlight) — drives chevron density. */
  highlightedNodeIds?: ReadonlySet<string>;
  className?: string;
}

// Fix #2109a: arrowhead size reduced to ~50% of previous (was 9, now 5).
// Smaller triangles read as clean directional cues without overwhelming a
// dense graph at any zoom level.
const CHEVRON = 5;
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

/**
 * Fix #2109b: build a quadratic bezier SVG path string from source to target
 * with a perpendicular midpoint offset for the Yondu curved-arc aesthetic.
 * The control point is the midpoint shifted perpendicular to the line by
 * `curvature` px. Direction is always "left" of the travel direction so all
 * arcs curve consistently (no random flipping).
 */
function yonduArcPath(
  sx: number, sy: number,
  tx: number, ty: number,
  curvature = 0.25,
): string {
  const mx = (sx + tx) / 2;
  const my = (sy + ty) / 2;
  const dx = tx - sx;
  const dy = ty - sy;
  const len = Math.hypot(dx, dy) || 1;
  // Perpendicular (left of travel direction): (-dy, dx) normalised × offset.
  const offset = len * curvature;
  const cpx = mx + (-dy / len) * offset;
  const cpy = my + (dx / len) * offset;
  return `M ${sx} ${sy} Q ${cpx} ${cpy} ${tx} ${ty}`;
}


export const GraphJarvisOverlay = memo(function GraphJarvisOverlay({
  canvasHandle,
  steps,
  currentTarget,
  edgeProgress,
  traversedEdges,
  running,
  paused,
  phase = "idle",
  glowProgress = 0,
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

  // Project the union of: every node in every step + every highlighted node.
  const ids = new Set<string>();
  for (const s of steps) for (const id of s.nodeIds) ids.add(id);
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

  // ── Per-step polyline geometry ──────────────────────────────────────────
  // For each call (step), build the array of projected points (skipping any
  // node we couldn't project — view culled / not in current viewport). Each
  // polyline drives both the Phase 1 sweep (current step) and the trail tint
  // (completed steps).
  function polylineFor(step: JarvisStep): { x: number; y: number }[] {
    const pts: { x: number; y: number }[] = [];
    for (const id of step.nodeIds) {
      const p = projected.get(id);
      if (p) pts.push(p);
    }
    return pts;
  }

  // Cumulative arc-length helpers — used to place trail dashes at FIXED
  // pixel offsets behind the head along the polyline (not parametric).
  function cumLengths(pts: { x: number; y: number }[]): number[] {
    const out: number[] = [0];
    for (let i = 1; i < pts.length; i++) {
      const dx = pts[i].x - pts[i - 1].x;
      const dy = pts[i].y - pts[i - 1].y;
      out.push(out[i - 1] + Math.hypot(dx, dy));
    }
    return out;
  }

  // Sample a point at arc-length `s` along the polyline.
  function sampleAt(
    pts: { x: number; y: number }[],
    lens: number[],
    s: number,
  ): { x: number; y: number } | null {
    if (pts.length === 0) return null;
    if (pts.length === 1) return pts[0];
    const total = lens[lens.length - 1];
    if (total <= 0) return pts[0];
    const sc = Math.max(0, Math.min(total, s));
    // Binary-search-ish linear scan is fine: polylines are ≤ ~50 segments.
    for (let i = 1; i < lens.length; i++) {
      if (lens[i] >= sc) {
        const seg = lens[i] - lens[i - 1];
        const t = seg === 0 ? 0 : (sc - lens[i - 1]) / seg;
        return {
          x: lerp(pts[i - 1].x, pts[i].x, t),
          y: lerp(pts[i - 1].y, pts[i].y, t),
        };
      }
    }
    return pts[pts.length - 1];
  }

  // Trail (completed call polylines): drawn as a faint accent stroke under
  // the bridges/chevrons. Bridge dashing is preserved from #1932/#1948.
  type TrailSeg = { x1: number; y1: number; x2: number; y2: number; bridge: boolean };
  const trail: TrailSeg[] = [];
  for (const ti of traversedEdges) {
    if (ti < 0 || ti >= steps.length) continue;
    const poly = polylineFor(steps[ti]);
    for (let j = 1; j < poly.length; j++) {
      const a = poly[j - 1];
      const b = poly[j];
      const bridge = canvasHandle.isBridgeEdge(
        steps[ti].nodeIds[j - 1],
        steps[ti].nodeIds[j],
      );
      trail.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y, bridge });
    }
  }

  // ── Current-call geometry (Phase 1 sweep + Phase 2 glow) ────────────────
  const inFlight = (running || paused) && currentTarget >= 0 && currentTarget < steps.length;
  const currentStep = inFlight ? steps[currentTarget] : null;
  const currentPoly = currentStep ? polylineFor(currentStep) : [];
  const currentLens = currentPoly.length > 1 ? cumLengths(currentPoly) : [];
  const currentTotalLen = currentLens.length ? currentLens[currentLens.length - 1] : 0;

  // Phase 1 sweep: phantom-arrow head + 4 trailing dashes at fixed pixel
  // offsets BEHIND the head. Dashes fade out the older they are.
  // Trail dash spacing (#1953 spec: ~20px).
  const TRAIL_DASH_PX = 20;
  const TRAIL_DASH_COUNT = 4;
  const TRAIL_DASH_OPACITIES = [0.5, 0.3, 0.15, 0.05];

  type SweepDash = { x: number; y: number; opacity: number; r: number };
  let sweepHead: { x: number; y: number } | null = null;
  const sweepDashes: SweepDash[] = [];
  // Base stroke geometry for the current call (drawn as a faint guide).
  const currentSegs: TrailSeg[] = [];
  if (
    !reducedMotion &&
    currentStep &&
    currentPoly.length > 0 &&
    (phase === "sweep" || (phase === "glow" && currentTotalLen === 0))
  ) {
    // Head position at current sweep progress (0..1 of total polyline length).
    const headS = currentTotalLen * Math.max(0, Math.min(1, edgeProgress));
    const head = sampleAt(currentPoly, currentLens, headS);
    if (head) {
      sweepHead = head;
      for (let i = 0; i < TRAIL_DASH_COUNT; i++) {
        const s = headS - (i + 1) * TRAIL_DASH_PX;
        if (s < 0) break;
        const p = sampleAt(currentPoly, currentLens, s);
        if (!p) break;
        sweepDashes.push({
          x: p.x,
          y: p.y,
          opacity: TRAIL_DASH_OPACITIES[i] ?? 0.05,
          r: 2.2 - i * 0.35,
        });
      }
    }
    for (let j = 1; j < currentPoly.length; j++) {
      const a = currentPoly[j - 1];
      const b = currentPoly[j];
      const bridge = canvasHandle.isBridgeEdge(
        currentStep.nodeIds[j - 1],
        currentStep.nodeIds[j],
      );
      currentSegs.push({ x1: a.x, y1: a.y, x2: b.x, y2: b.y, bridge });
    }
  }

  // Phase 2 glow burst: every node in the current step pulses synchronously.
  // size: 1.0 → 1.4 → 1.0 with an 80ms hold at peak.
  //   Peak hold window = first ~80/total of glow progress (peak first).
  // We model glowProgress 0..1 over the glow duration. With glowMs default
  // 300ms, hold ratio ≈ 80/300 ≈ 0.27.
  //
  // Fix #2109c: glow IGNITES at 90% of the Phase 1 sweep flight (edgeProgress ≥ 0.9).
  // While the arrow is still in-flight (phase === "sweep" and edgeProgress ≥ 0.9),
  // we start rendering glow dots at a fraction of their peak intensity so the
  // glow "ignites" before the arrow fully arrives — the sequence:
  //   0-80%: arrow visible, no glow
  //   80-90%: arrow fading (still visible), glow igniting (0→50% intensity)
  //   90-100%: arrow gone, glow at 50%→full intensity
  //   Phase 2 glow: glow at full burst (500ms normal phase)
  type GlowDot = { x: number; y: number; sizeMul: number; opacity: number };
  const glowDots: GlowDot[] = [];
  const inGlow = inFlight && phase === "glow";
  // Pre-ignition: glow starts at 90% of sweep flight (#2109c).
  const preIgnite = inFlight && phase === "sweep" && edgeProgress >= 0.9;
  if (currentStep && (inGlow || preIgnite || (reducedMotion && inFlight))) {
    // Curve: ramp up over first ~10%, hold until ~37% (80ms of 300ms),
    // then ease back to baseline over the remaining 63%. With reduced-
    // motion: hold at peak the entire phase (no size animation).
    //
    // Fix #2109c: pre-ignite phase (edgeProgress 0.9-1.0 during sweep):
    // ramp glow from 0→50% intensity as the arrow fades, so the node starts
    // glowing before the arrow fully arrives (~500ms burst timing).
    let mul = 1.0;
    let op = 1.0;
    if (reducedMotion) {
      mul = 1.0; // no size anim
      op = 1.0;
    } else if (preIgnite) {
      // Pre-ignition: edgeProgress 0.9..1.0 → glow 0→50% intensity.
      const t = (edgeProgress - 0.9) / 0.1;
      mul = lerp(1.0, 1.2, t);
      op = lerp(0.0, 0.5, t);
    } else {
      const g = Math.max(0, Math.min(1, glowProgress));
      const RAMP = 0.1;
      const HOLD_END = 0.37;
      if (g < RAMP) {
        mul = lerp(1.0, 1.4, g / RAMP);
        op = 1.0;
      } else if (g < HOLD_END) {
        mul = 1.4;
        op = 1.0;
      } else {
        const t = (g - HOLD_END) / Math.max(0.0001, 1 - HOLD_END);
        mul = lerp(1.4, 1.0, t);
        op = lerp(1.0, 0.0, t);
      }
    }
    for (const id of currentStep.nodeIds) {
      const p = projected.get(id);
      if (!p) continue;
      glowDots.push({ x: p.x, y: p.y, sizeMul: mul, opacity: op });
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
        {/* Fix #2109a: arrowhead size halved. Chevron marker — regular edges. */}
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
          markerWidth={CHEVRON}
          markerHeight={CHEVRON}
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
          markerWidth={CHEVRON}
          markerHeight={CHEVRON}
          refX="9"
          refY="5"
          orient="auto"
        >
          <path d="M 0 0 L 9 5 L 0 10 z" fill={accent} />
        </marker>
        <filter id="ag-graph-comet-glow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="2.4" />
        </filter>
        {/* Fix #2109b: Yondu-style glow filter for the trail tip. */}
        <filter id="ag-graph-yondu-tip-glow" x="-80%" y="-80%" width="260%" height="260%">
          <feGaussianBlur stdDeviation="3.5" result="blur" />
          <feMerge>
            <feMergeNode in="blur" />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
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

      {/* ── Trail tint (completed call polylines) ─────────────────────────
          One <line> per polyline segment per completed call. Bridges keep
          their dashed stroke + bridge-accent color (#1926/#1948 styling). */}
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
          opacity={0.45}
          markerEnd="url(#ag-graph-chev-accent)"
        />
      ))}

      {/* ── Phase 1 sweep (#1953 + Fix #2109b/c) — Yondu-style curved arc trail.
          Each polyline segment is a quadratic bezier (curved arc). The trail
          has a stroke-gradient feel: bright at the tip, transparent at the tail
          (achieved via opacity tiers on fading dashes). Arrow opacity follows
          the #2109c fade sequence: full 0-80% flight, taper to 0 at 100%.    */}
      {currentSegs.length > 0 ? (
        <g data-testid="graph-jarvis-sweep">
          {/* Fix #2109b: curved bezier arcs replace straight line segments.
              Opacity is stepped so arcs closer to the head are brighter. */}
          {currentSegs.map((s, i) => {
            // Fix #2109c: scale the base arc opacity by the arrow fade envelope.
            // arrowFadeOpacity: 1.0 for edgeProgress ≤ 0.8, linearly → 0 by 1.0.
            const arrowFade = edgeProgress <= 0.8
              ? 1.0
              : 1.0 - (edgeProgress - 0.8) / 0.2;
            // Arcs closer to the tail are more transparent (gradient trail feel).
            const segOpacity = 0.35 * arrowFade * (1 - i * 0.12);
            return (
              <path
                key={`cur-${i}`}
                d={yonduArcPath(s.x1, s.y1, s.x2, s.y2, 0.18)}
                fill="none"
                stroke={s.bridge ? bridgeAccent : accent}
                strokeWidth={s.bridge ? 1.6 : 1.2}
                strokeDasharray={s.bridge ? "5 3" : undefined}
                strokeLinecap="round"
                opacity={Math.max(0, segOpacity)}
              />
            );
          })}
          {/* Trail dashes (older = dimmer + smaller). Fix #2109b: positioned
              along the bezier arc implicitly (sample is still arc-length based). */}
          {sweepDashes.map((d, i) => {
            const arrowFade = edgeProgress <= 0.8
              ? 1.0
              : 1.0 - (edgeProgress - 0.8) / 0.2;
            return (
              <circle
                key={`dash-${i}`}
                cx={d.x}
                cy={d.y}
                r={Math.max(0.6, d.r)}
                fill={accent}
                opacity={d.opacity * arrowFade}
              />
            );
          })}
          {/* Fix #2109b: Arrow HEAD — bright glowing tip with drop-shadow.
              Fix #2109c: tip fades from 80-100% of flight. */}
          {sweepHead ? (() => {
            const arrowFade = edgeProgress <= 0.8
              ? 1.0
              : 1.0 - (edgeProgress - 0.8) / 0.2;
            return arrowFade > 0.01 ? (
              <>
                {/* Outer glow halo via drop-shadow filter */}
                <circle
                  cx={sweepHead.x}
                  cy={sweepHead.y}
                  r={5.5}
                  fill={accent}
                  opacity={0.55 * arrowFade}
                  filter="url(#ag-graph-yondu-tip-glow)"
                />
                {/* Mid glow ring */}
                <circle
                  cx={sweepHead.x}
                  cy={sweepHead.y}
                  r={3.6}
                  fill={accent}
                  opacity={0.85 * arrowFade}
                  filter="url(#ag-graph-comet-glow)"
                />
                {/* Bright white hot core */}
                <circle
                  cx={sweepHead.x}
                  cy={sweepHead.y}
                  r={1.8}
                  fill="#ffffff"
                  opacity={0.95 * arrowFade}
                />
              </>
            ) : null;
          })() : null}
        </g>
      ) : null}

      {/* ── Phase 2 glow burst (#1953) — synchronized pulse on every
          returned node for the current call. Size 1.0 → 1.4 → 1.0 with
          80ms hold at peak; under reduced motion the pulse renders
          statically (no size animation). */}
      {glowDots.length > 0 ? (
        <g data-testid="graph-jarvis-glow">
          {glowDots.map((d, i) => (
            <g key={`glow-${i}`}>
              <circle
                cx={d.x}
                cy={d.y}
                r={9 * d.sizeMul}
                fill={accent}
                opacity={d.opacity * 0.18}
                filter="url(#ag-graph-comet-glow)"
              />
              <circle
                cx={d.x}
                cy={d.y}
                r={4.2 * d.sizeMul}
                fill={accent}
                opacity={d.opacity * 0.9}
              />
              <circle
                cx={d.x}
                cy={d.y}
                r={1.8 * d.sizeMul}
                fill="#ffffff"
                opacity={d.opacity * 0.95}
              />
            </g>
          ))}
        </g>
      ) : null}
    </svg>
  );
});
