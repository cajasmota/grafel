/* CTZone.tsx — a containment zone box. Click the header to collapse/expand.
   A collapsed zone renders as one solid leaf box (with a member count) and the
   diagram aggregates its members' cross-zone edges into summary edges. */

import { memo } from "react";
import { ChevronDown, ChevronRight, Box, Cloud, Network, Boxes, Server } from "lucide-react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import type { CTZoneData } from "./layout";

function zoneIcon(kind: string) {
  switch (kind) {
    case "cloud":
      return Cloud;
    case "network":
      return Network;
    case "repo":
      return Boxes;
    case "service":
      return Server;
    default:
      return Box;
  }
}

function CTZoneImpl({ data }: NodeProps) {
  const d = data as CTZoneData;
  const Icon = zoneIcon(d.kind);
  const Chevron = d.collapsed ? ChevronRight : ChevronDown;

  return (
    <div
      className={
        d.collapsed
          ? "flex h-full w-full flex-col rounded-lg border border-border bg-surface-2/80 shadow-sm"
          : "h-full w-full rounded-lg border border-dashed border-border bg-surface-2/30"
      }
      title={`${d.label} (${d.kind}) · ${d.nodeCount} node${d.nodeCount === 1 ? "" : "s"}`}
    >
      {d.collapsed && (
        <>
          <Handle type="target" position={Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-text-4" />
          <Handle type="source" position={Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-text-4" />
        </>
      )}
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          d.onToggle(d.zoneId);
        }}
        className="flex w-full items-center gap-1.5 rounded-t-lg px-2.5 py-1.5 text-text-3 transition-colors hover:bg-surface-2"
      >
        <Chevron size={12} className="shrink-0 text-text-4" />
        <Icon size={11} className="shrink-0 text-text-4" />
        <span className="truncate font-mono text-[10px] font-medium" title={d.label}>
          {d.label}
        </span>
        <span className="ml-auto shrink-0 text-[10px] tabular-nums text-text-4">{d.nodeCount}</span>
      </button>
      {d.collapsed && (
        <div className="flex flex-1 items-center justify-center px-2 pb-2 text-[10px] text-text-4">
          collapsed · {d.nodeCount} node{d.nodeCount === 1 ? "" : "s"}
        </div>
      )}
    </div>
  );
}

export const CTZone = memo(CTZoneImpl);
