/* CTEdge.tsx — a typed relationship edge. Summary (aggregated) edges from a
   collapsed zone are drawn thicker with a ×N count label. */

import { memo } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  type EdgeProps,
} from "@xyflow/react";
import { orthogonalPath } from "@/lib/elk-layout";
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
  // Follow ELK's orthogonal route when available (#4843); else smoothstep (H/V).
  const elk = orthogonalPath(d.elkPoints ?? []);
  let path: string;
  let labelX: number;
  let labelY: number;
  if (elk) {
    ({ path, labelX, labelY } = elk);
  } else {
    [path, labelX, labelY] = getSmoothStepPath({
      sourceX,
      sourceY,
      sourcePosition,
      targetX,
      targetY,
      targetPosition,
    });
  }
  const stroke = edgeStroke(d.type);
  // Unified diagram (Model 3, #4810): a real code↔infra usage edge (e.g. a
  // service that WRITES a queue) is drawn thicker/fully-opaque so the
  // code-to-infra wiring stands out from intra-layer edges.
  const cross = d.crossBoundary === true;

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke,
          strokeWidth: cross ? 2.25 : d.summary ? 2 : 1.25,
          strokeDasharray: d.summary ? "6 3" : undefined,
          opacity: cross ? 1 : 0.9,
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
