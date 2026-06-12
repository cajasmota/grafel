/* CTNode.tsx — one entity in the compound topology, tinted by its tier lane. */

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Database, Box } from "lucide-react";
import type { CTNodeData } from "./layout";
import { tierStyle } from "./tierStyle";

function CTNodeImpl({ data }: NodeProps) {
  const d = data as CTNodeData;
  const s = tierStyle(d.tier);

  // Cross-link highlight (Model 2). "primary" = same entity in the other lens,
  // "linked" = edge-connected counterpart. Non-matching nodes dim when a
  // selection is active so the cross-link reads at a glance.
  const hl = d.highlight;
  const isPrimary = hl === "primary";
  const isLinked = hl === "linked";
  const dim = d.dimmed && hl === "none";
  const borderColor = isPrimary ? "var(--accent)" : isLinked ? "var(--info)" : s.color;

  // Unified diagram (Model 3, #4810): infra resources read differently from
  // code entities so the single interleaved canvas stays legible. Infra nodes
  // get a heavier left rail + a datastore glyph and a slightly more "panel"
  // shape; code nodes keep the rounded card. Undefined outside the unified view
  // (no visual change there).
  const cls = d.nodeClass;
  const isInfra = cls === "infra";
  const isCode = cls === "code";
  const ClassIcon = isInfra ? Database : isCode ? Box : null;
  const shapeClass = isInfra ? "rounded-sm" : "rounded-md";

  return (
    <div
      className={`flex h-full w-full flex-col justify-center border px-2.5 py-1 transition-opacity ${shapeClass}`}
      style={{
        borderColor,
        borderWidth: isPrimary || isLinked ? 2 : 1,
        background: s.tint,
        opacity: dim ? 0.28 : 1,
        // Infra resources carry a solid left rail so they read as "a deployed
        // thing" vs code; code keeps a flush card.
        borderLeftWidth: isInfra ? 4 : isPrimary || isLinked ? 2 : 1,
        boxShadow: isPrimary
          ? "0 0 0 2px color-mix(in srgb, var(--accent) 40%, transparent)"
          : isLinked
            ? "0 0 0 2px color-mix(in srgb, var(--info) 32%, transparent)"
            : undefined,
      }}
      title={`${d.label} · ${d.kind} · ${s.label}${d.repo ? ` · ${d.repo}` : ""}${
        cls ? ` · ${cls === "infra" ? "infra resource" : "code"}` : ""
      }${isPrimary ? " · cross-linked (same entity)" : isLinked ? " · linked by edge" : ""}`}
    >
      <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-text-4" />
      <span className="flex items-center gap-1 truncate text-[11px] font-medium" style={{ color: s.color }}>
        {ClassIcon && <ClassIcon size={10} className="shrink-0" />}
        <span className="truncate">{d.label || d.kind}</span>
      </span>
      <span className="truncate text-[9px] uppercase tracking-wide text-text-4">
        {isInfra ? "infra · " : isCode ? "code · " : ""}{d.kind}
      </span>
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-text-4" />
    </div>
  );
}

export const CTNode = memo(CTNodeImpl);
