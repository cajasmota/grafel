/* ============================================================
   components/flow-dag/FlowDagEdge.tsx — custom React Flow edge.

   Draws a semantic edge styled + labelled by its kind (CALLS /
   HANDLER_CONTINUATION / JOINS_COLLECTION / THROWS / VALIDATES). The CALLS
   spine is neutral + solid; semantic side-edges are colored + dashed with a
   small mid-edge label so branches are legible.
   ============================================================ */

import { memo, useEffect, useRef, useState } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  type EdgeProps,
} from "@xyflow/react";
import { orthogonalPath } from "@/lib/elk-layout";
import { edgeStyle } from "./style";
import type { FlowDagEdgeData } from "./layout";

function FlowDagEdgeImpl({
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  data,
}: EdgeProps) {
  const ed = data as FlowDagEdgeData | undefined;
  const kind = ed?.kind ?? "CALLS";
  const es = edgeStyle(kind);
  // Step-replay (#4362): active edge carries the comet; traversed edges stay
  // tinted; pending edges dim. Falls back to the route-highlight dimming.
  const replay = ed?.replay;
  const replayProgress = ed?.replayProgress ?? 0;
  // Click-to-highlight (#4479): off-route edges dim; on-route edges stay full.
  const dimmed = ed?.onRoute === false || replay === "pending";
  const traversed = replay === "traversed" || replay === "active";

  // Follow ELK's orthogonal route when available (#4843); else smoothstep (H/V).
  // Never a diagonal bezier — the comet rides whichever path we render.
  const elk = orthogonalPath(ed?.elkPoints ?? []);
  let path: string;
  let labelX: number;
  let labelY: number;
  if (elk) {
    ({ path, labelX, labelY } = elk);
  } else {
    [path, labelX, labelY] = getSmoothStepPath({
      sourceX,
      sourceY,
      targetX,
      targetY,
      sourcePosition,
      targetPosition,
    });
  }

  // Comet position — sample the exact path via getPointAtLength on a
  // measured (off-screen) copy of the path. Only the ACTIVE edge re-renders per
  // frame, so this stays cheap (one DOM measure per frame, one edge at a time).
  const measureRef = useRef<SVGPathElement | null>(null);
  const [comet, setComet] = useState<{ x: number; y: number } | null>(null);
  useEffect(() => {
    if (replay !== "active" || !measureRef.current) {
      setComet(null);
      return;
    }
    const el = measureRef.current;
    let len = 0;
    try {
      len = el.getTotalLength();
    } catch {
      return;
    }
    if (!len) return;
    const pt = el.getPointAtLength(len * Math.min(1, Math.max(0, replayProgress)));
    setComet({ x: pt.x, y: pt.y });
  }, [replay, replayProgress, path]);

  const stroke = traversed ? "var(--accent)" : es.stroke;

  return (
    <>
      <BaseEdge
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke,
          strokeWidth:
            replay === "active" ? 2.5 : kind === "CALLS" ? 1.5 : 1.75,
          strokeDasharray: es.dash,
          opacity: dimmed ? 0.18 : traversed ? 0.95 : 1,
          transition: "opacity 150ms, stroke-width 150ms",
        }}
      />
      {/* Hidden measuring copy of the path so getPointAtLength samples the exact
          bezier the comet rides. Rendered only while this edge is active. */}
      {replay === "active" && (
        <path ref={measureRef} d={path} fill="none" stroke="none" />
      )}
      {/* Traveling-light comet (#4362) — a glowing dot at the sampled point with
          a soft halo. Pure SVG, no per-frame layout thrash. */}
      {replay === "active" && comet && (
        <g className="pointer-events-none">
          <circle
            cx={comet.x}
            cy={comet.y}
            r={6}
            fill="var(--accent)"
            opacity={0.25}
          />
          <circle
            cx={comet.x}
            cy={comet.y}
            r={3}
            fill="var(--accent)"
            style={{ filter: "drop-shadow(0 0 4px var(--accent))" }}
          />
        </g>
      )}
      {/* Only the non-spine semantic edges carry a label, to keep CALLS clean. */}
      {kind !== "CALLS" && (
        <EdgeLabelRenderer>
          <div
            className="absolute pointer-events-none rounded px-1 py-px text-[9px] font-medium leading-none"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
              background: "var(--surface)",
              color: stroke,
              border: `1px solid color-mix(in srgb, ${stroke} 45%, transparent)`,
              opacity: dimmed ? 0.18 : 1,
            }}
          >
            {es.label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export const FlowDagEdge = memo(FlowDagEdgeImpl);
