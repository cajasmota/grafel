/* ============================================================
   components/flow-dag/FlowDagEdge.tsx — custom React Flow edge.

   Draws a semantic edge styled + labelled by its kind (CALLS /
   HANDLER_CONTINUATION / JOINS_COLLECTION / THROWS / VALIDATES). The CALLS
   spine is neutral + solid; semantic side-edges are colored + dashed with a
   small mid-edge label so branches are legible.
   ============================================================ */

import { memo } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from "@xyflow/react";
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
  // Click-to-highlight (#4479): off-route edges dim; on-route edges stay full.
  const dimmed = ed?.onRoute === false;

  const [path, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
  });

  return (
    <>
      <BaseEdge
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: es.stroke,
          strokeWidth: kind === "CALLS" ? 1.5 : 1.75,
          strokeDasharray: es.dash,
          opacity: dimmed ? 0.18 : 1,
          transition: "opacity 150ms",
        }}
      />
      {/* Only the non-spine semantic edges carry a label, to keep CALLS clean. */}
      {kind !== "CALLS" && (
        <EdgeLabelRenderer>
          <div
            className="absolute pointer-events-none rounded px-1 py-px text-[9px] font-medium leading-none"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
              background: "var(--surface)",
              color: es.stroke,
              border: `1px solid color-mix(in srgb, ${es.stroke} 45%, transparent)`,
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
