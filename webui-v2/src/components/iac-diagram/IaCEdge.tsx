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
  getSmoothStepPath,
  type EdgeProps,
} from "@xyflow/react";
import { orthogonalPath } from "@/lib/elk-layout";
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
    // #4625 — cross-module semantic verbs. A consuming resource → another
    // module's output, labelled with its cloud-architecture meaning.
    case "consumes":
      return { stroke: "var(--accent)", label: "consumes" };
    case "redrive":
      return { stroke: "var(--warning)", dash: "5 3", label: "redrive" };
    case "logs-to":
      return { stroke: "var(--info)", dash: "1 3", label: "logs-to" };
    case "assumes":
      return { stroke: "var(--danger)", dash: "4 2", label: "assumes" };
    case "grants":
      return { stroke: "var(--danger)", dash: "5 3", label: "grants" };
    case "reads":
      return { stroke: "var(--text-3)", label: "reads" };
    // #4657 — module instantiation: an env's module instance → the resources
    // of the module definition it instantiates. Solid accent so the env→
    // definition projection reads as a first-class architecture link.
    case "instantiates":
      return { stroke: "var(--accent)", label: "instantiates" };
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

  // Follow ELK's orthogonal route when available (#4843); else fall back to a
  // smoothstep (H/V) path — never a diagonal bezier.
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
