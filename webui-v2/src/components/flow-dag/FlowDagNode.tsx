/* ============================================================
   components/flow-dag/FlowDagNode.tsx — custom React Flow node.

   Renders one downstream-DAG node styled by `role`, marks `terminal` sinks
   distinctly, and (in spine mode) renders an inline expander for the
   collapsed builder/predicate children. Expanding is purely client-side —
   the rows are already on the payload, so no refetch.
   ============================================================ */

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { ChevronRight, ChevronDown, CircleDot } from "lucide-react";
import { cn } from "@/lib/utils";
import { RepoChip } from "@/lib/repo-color";
import { roleStyle } from "./style";
import type { FlowDagNodeData } from "./layout";

/**
 * FlowDagNode — one DAG node. Color + label come from the role; terminal
 * nodes get a doubled ring so collection sinks read as endpoints of the walk.
 * The collapsed-children count badge expands an inline list on click.
 */
function FlowDagNodeImpl({ id, data, sourcePosition, targetPosition }: NodeProps) {
  const { node, expanded, onToggleExpand, selected, onRoute } = data as FlowDagNodeData;
  const rs = roleStyle(node.role);
  const collapsed = node.collapsed_children ?? [];
  const hasCollapsed = collapsed.length > 0;
  // Click-to-highlight (#4479): when a route is active, off-route nodes dim.
  const dimmed = onRoute === false;

  return (
    <div
      className={cn(
        "rounded-lg border bg-surface shadow-[var(--shadow-2)] text-left cursor-pointer",
        "min-w-[220px] max-w-[220px] transition-opacity",
        dimmed ? "opacity-25" : "opacity-100",
      )}
      style={{
        // Role tint as a soft background wash; the ink as the border.
        background: `color-mix(in srgb, ${rs.bg} 22%, var(--surface))`,
        borderColor: selected || onRoute === true
          ? "var(--accent)"
          : `color-mix(in srgb, ${rs.ink} 55%, transparent)`,
        // Selected node, or a node lit on the highlighted route (#4479), gets an
        // accent ring; otherwise terminal sinks get a heavier outline ring.
        boxShadow: selected || onRoute === true
          ? "0 0 0 2px var(--accent)"
          : node.terminal
            ? `0 0 0 2px color-mix(in srgb, ${rs.ink} 45%, transparent)`
            : undefined,
      }}
    >
      {/* in/out handles — positions follow the H/V layout direction. */}
      <Handle type="target" position={targetPosition ?? Position.Left} className="!bg-text-4" />
      <Handle type="source" position={sourcePosition ?? Position.Right} className="!bg-text-4" />

      <div className="px-2.5 py-2">
        <div className="flex items-center gap-1.5">
          <span
            className="inline-flex items-center h-[15px] px-1 rounded text-[9px] font-semibold uppercase tracking-wide leading-none shrink-0"
            style={{
              background: `color-mix(in srgb, ${rs.bg} 38%, transparent)`,
              color: rs.ink,
            }}
          >
            {rs.label}
          </span>
          {node.terminal && (
            <CircleDot size={11} className="shrink-0" style={{ color: rs.ink }} aria-label="terminal" />
          )}
          <RepoChip slug={node.repo} className="ml-auto" maxLength={14} />
        </div>

        <div className="mt-1 text-xs font-medium text-text truncate" title={node.name}>
          {node.name}
        </div>
        <div className="text-[10px] text-text-4 font-mono truncate" title={`${node.kind}${node.file ? ` · ${node.file}` : ""}`}>
          {node.kind}
        </div>

        {hasCollapsed && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              // Keyed by INSTANCE id so duplicated instances expand independently.
              onToggleExpand(id);
            }}
            className={cn(
              "mt-1.5 inline-flex items-center gap-1 h-[18px] px-1.5 rounded",
              "text-[10px] font-medium text-text-3 bg-surface-2 hover:bg-bg-soft transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
            )}
            title={expanded ? "Collapse builder/predicate calls" : "Expand builder/predicate calls"}
          >
            {expanded ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
            {collapsed.length} collapsed
          </button>
        )}

        {expanded && hasCollapsed && (
          <ul className="mt-1.5 space-y-0.5 border-t border-border pt-1.5">
            {collapsed.map((c) => (
              <li
                key={c.id}
                className="flex items-center gap-1 text-[10px] text-text-3"
                title={`${c.kind} · ${c.edge_kind}${c.file ? ` · ${c.file}` : ""}`}
              >
                <span className="size-1 rounded-full bg-text-4 shrink-0" />
                <span className="font-mono truncate">{c.name}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

export const FlowDagNode = memo(FlowDagNodeImpl);
