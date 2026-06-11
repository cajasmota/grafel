/* CTNode.tsx — one entity in the compound topology, tinted by its tier lane. */

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { CTNodeData } from "./layout";
import { tierStyle } from "./tierStyle";

function CTNodeImpl({ data }: NodeProps) {
  const d = data as CTNodeData;
  const s = tierStyle(d.tier);
  return (
    <div
      className="flex h-full w-full flex-col justify-center rounded-md border px-2.5 py-1"
      style={{ borderColor: s.color, background: s.tint }}
      title={`${d.label} · ${d.kind} · ${s.label}${d.repo ? ` · ${d.repo}` : ""}`}
    >
      <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-text-4" />
      <span className="truncate text-[11px] font-medium text-text" style={{ color: s.color }}>
        {d.label || d.kind}
      </span>
      <span className="truncate text-[9px] uppercase tracking-wide text-text-4">{d.kind}</span>
      <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-text-4" />
    </div>
  );
}

export const CTNode = memo(CTNodeImpl);
