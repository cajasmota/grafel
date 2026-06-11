/* CTEdge.tsx — a typed relationship edge. Summary (aggregated) edges from a
   collapsed zone are drawn thicker with a ×N count label. */

import { memo } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from "@xyflow/react";
import type { CTEdgeData } from "./layout";
import { edgeStroke } from "./tierStyle";

function CTEdgeImpl({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  markerEnd,
}: EdgeProps) {
  const d = (data ?? { type: "depends", label: "depends", count: 1, summary: false }) as CTEdgeData;
  const [path, labelX, labelY] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const stroke = edgeStroke(d.type);

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke,
          strokeWidth: d.summary ? 2 : 1.25,
          strokeDasharray: d.summary ? "6 3" : undefined,
        }}
      />
      <EdgeLabelRenderer>
        <div
          style={{
            position: "absolute",
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
            color: stroke,
            pointerEvents: "none",
          }}
          className="rounded bg-surface/80 px-1 text-[8px] font-medium uppercase tracking-wide"
        >
          {d.label}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

export const CTEdge = memo(CTEdgeImpl);
