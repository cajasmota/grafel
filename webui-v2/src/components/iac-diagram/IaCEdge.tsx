/* ============================================================
   components/iac-diagram/IaCEdge.tsx — custom relation edge (#4526).

   Draws a directional resource-to-resource relation styled by its facet:
   grant (danger, dashed) / event_source (warning, dashed) / topology (info,
   dotted) / dependency (neutral, solid). A small mid-edge label names the
   facet so a dense stack stays legible.
   ============================================================ */

import { memo } from "react";
import {
  BaseEdge,
  EdgeLabelRenderer,
  getBezierPath,
  type EdgeProps,
} from "@xyflow/react";
import type { IaCEdgeData } from "./layout";

interface FacetStyle {
  stroke: string;
  dash?: string;
  label: string;
}

export function facetStyle(facet: string): FacetStyle {
  switch ((facet || "").toLowerCase()) {
    case "grant":
      return { stroke: "var(--danger)", dash: "5 3", label: "grant" };
    case "event_source":
      return { stroke: "var(--warning)", dash: "5 3", label: "event" };
    case "trigger":
      return { stroke: "var(--accent)", dash: "5 3", label: "trigger" };
    case "topology":
      return { stroke: "var(--info)", dash: "2 3", label: "topology" };
    case "output":
      return { stroke: "var(--text-4)", dash: "2 3", label: "output" };
    default:
      return { stroke: "var(--text-4)", label: "depends" };
  }
}

function IaCEdgeImpl({
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  data,
}: EdgeProps) {
  const ed = data as IaCEdgeData | undefined;
  const fs = facetStyle(ed?.facet ?? "dependency");

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
        style={{ stroke: fs.stroke, strokeWidth: 1.5, strokeDasharray: fs.dash }}
      />
      {/* Label only the semantic (non-plain-dependency) edges to reduce clutter. */}
      {ed?.facet && ed.facet.toLowerCase() !== "dependency" && (
        <EdgeLabelRenderer>
          <div
            className="absolute pointer-events-none rounded px-1 py-px text-[9px] font-medium leading-none"
            style={{
              transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
              background: "var(--surface)",
              color: fs.stroke,
              border: `1px solid color-mix(in srgb, ${fs.stroke} 45%, transparent)`,
            }}
            title={ed.detail ? `${fs.label}: ${ed.detail}` : fs.label}
          >
            {fs.label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

export const IaCEdge = memo(IaCEdgeImpl);
