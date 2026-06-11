/* CTZone.tsx — a containment zone box. Click the header to collapse/expand.
   A collapsed zone renders as one solid leaf box (with a member count) and the
   diagram aggregates its members' cross-zone edges into summary edges.

   #4866 — the zone box reads clearly as a grouping frame: a solid,
   higher-contrast border (heavier than the leaf node cards), a faint per-kind
   background tint, and a prominent header chip. Nesting depth is distinguished
   by a stronger tint/border on outer zones so a nested zone stays legible. All
   colors come from the theme tone palette (CSS vars) so light + dark track. */

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

/** Per-kind accent hue (theme tone vars) so each zone reads on-theme (#4866). */
function zoneColor(kind: string): string {
  switch (kind) {
    case "cloud":
      return "var(--info)";
    case "network":
      return "var(--success)";
    case "repo":
      return "var(--accent)";
    case "service":
      return "var(--warning)";
    default:
      return "var(--text-3)";
  }
}

function CTZoneImpl({ data }: NodeProps) {
  const d = data as CTZoneData;
  const Icon = zoneIcon(d.kind);
  const Chevron = d.collapsed ? ChevronRight : ChevronDown;
  const color = zoneColor(d.kind);

  // Outer zones get a marginally stronger frame so a nested box stays legible
  // inside its parent (#4866). depth 0 = outermost.
  const outer = (d.depth ?? 0) === 0;
  const fill = `color-mix(in srgb, ${color} ${outer ? 6 : 10}%, transparent)`;
  const borderColor = `color-mix(in srgb, ${color} ${outer ? 55 : 70}%, var(--border-strong))`;

  return (
    <div
      className={
        d.collapsed
          ? "flex h-full w-full flex-col rounded-lg border-2 shadow-sm"
          : "h-full w-full rounded-lg border-2"
      }
      style={{
        borderColor,
        backgroundColor: d.collapsed
          ? `color-mix(in srgb, ${color} 14%, var(--surface-2))`
          : fill,
        boxShadow: d.collapsed
          ? undefined
          : "inset 0 0 0 1px color-mix(in srgb, var(--surface) 55%, transparent)",
      }}
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
        className="m-1 flex w-[calc(100%-0.5rem)] items-center gap-1.5 rounded-md px-2 py-1 text-text-2 transition-colors hover:brightness-95"
        style={{ backgroundColor: `color-mix(in srgb, ${color} 16%, var(--surface))` }}
      >
        <Chevron size={13} className="shrink-0 text-text-3" />
        <Icon size={12} className="shrink-0" style={{ color }} />
        <span className="truncate font-mono text-[11px] font-semibold tracking-tight" title={d.label}>
          {d.label}
        </span>
        <span
          className="ml-auto shrink-0 rounded-full px-1.5 text-[10px] font-medium tabular-nums text-text-3"
          style={{ backgroundColor: "color-mix(in srgb, var(--surface-2) 80%, transparent)" }}
        >
          {d.nodeCount}
        </span>
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
