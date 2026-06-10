/* ============================================================
   components/flow-dag/FlowDagNode.tsx — custom React Flow node.

   Renders one downstream-DAG node styled by `role`, marks `terminal` sinks
   distinctly, and (in spine mode) renders an inline expander for the
   collapsed builder/predicate children. Expanding is purely client-side —
   the rows are already on the payload, so no refetch.

   #4481: cards handle long names (responsive font + wrap to 2 lines + wider
   card + full-name tooltip) and surface richer info — file:line, the incoming
   edge kind, the collapsed-children count, plus the enriched backend fields
   (signature, subtype, doc, effects, collection). Secondary info is small +
   muted and omitted when absent so the card stays compact.
   ============================================================ */

import { memo } from "react";
import { useParams } from "react-router-dom";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { ChevronRight, ChevronDown, CircleDot } from "lucide-react";
import { cn } from "@/lib/utils";
import { RepoChip } from "@/lib/repo-color";
import { effectBadge } from "@/lib/effect-badge";
import { useSourcePeek } from "@/components/SourcePeek";
import { nodeStyle, edgeStyle } from "./style";
import type { FlowDagNodeData } from "./layout";

/**
 * FlowDagNode — one DAG node. Color + label come from the role; terminal
 * nodes get a doubled ring so collection sinks read as endpoints of the walk.
 * The collapsed-children count badge expands an inline list on click.
 */
function FlowDagNodeImpl({ id, data, sourcePosition, targetPosition }: NodeProps) {
  const { node, edgeKind, expanded, onToggleExpand, selected, onRoute } =
    data as FlowDagNodeData;
  // Exception/error nodes paint red (#4556); otherwise the role tint. The red
  // hue matches the THROWS edge so a thrown error reads consistently node→edge.
  const rs = nodeStyle(node, edgeKind);
  const { openSourcePeek } = useSourcePeek();
  const { groupId = "" } = useParams<{ groupId: string }>();
  const collapsed = node.collapsed_children ?? [];
  const hasCollapsed = collapsed.length > 0;
  // Click-to-highlight (#4479): when a route is active, off-route nodes dim.
  const dimmed = onRoute === false;

  // Incoming relationship label (calls/handler/joins/throws/validates) — from
  // the single in-edge feeding this instance. Absent on the root.
  const edgeLabel = edgeKind ? edgeStyle(edgeKind).label : null;
  // file:line — plain muted text; this canvas is read-only (no source-open).
  // Split into head (dir prefix, truncates) + tail (filename:line, kept whole)
  // so the load-bearing end of the path is always visible (cf. RefLine).
  const fileRef = node.file
    ? node.line
      ? `${node.file}:${node.line}`
      : node.file
    : null;
  let fileHead = "";
  let fileTail = "";
  if (node.file) {
    const slash = node.file.lastIndexOf("/");
    const filename = slash < 0 ? node.file : node.file.slice(slash + 1);
    fileHead = slash < 0 ? "" : node.file.slice(0, slash + 1);
    fileTail = node.line ? `${filename}:${node.line}` : filename;
  }
  const effects = node.effects ?? [];

  return (
    <div
      className={cn(
        "rounded-lg border bg-surface shadow-[var(--shadow-2)] text-left cursor-pointer",
        "min-w-[268px] max-w-[268px] transition-opacity",
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
        {/* Header row: role pill · incoming-edge pill · terminal marker · repo */}
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
          {edgeLabel && (
            <span
              className="inline-flex items-center h-[15px] px-1 rounded text-[9px] font-medium lowercase leading-none shrink-0 text-text-3 bg-surface-2"
              title={`incoming relationship: ${edgeLabel}`}
            >
              {edgeLabel}
            </span>
          )}
          {node.terminal && (
            <CircleDot size={11} className="shrink-0" style={{ color: rs.ink }} aria-label="terminal" />
          )}
          <RepoChip slug={node.repo} className="ml-auto" maxLength={14} />
        </div>

        {/* Name — responsive size, wraps to 2 lines, full name on hover. */}
        <div
          className="mt-1 text-[11px] leading-tight font-medium text-text break-words line-clamp-2"
          title={node.name}
        >
          {node.name}
        </div>

        {/* doc — one-line muted subtitle when present. */}
        {node.doc && (
          <div className="mt-0.5 text-[10px] text-text-3 truncate" title={node.doc}>
            {node.doc}
          </div>
        )}

        {/* signature — monospace, truncated, full on hover. */}
        {node.signature && (
          <div
            className="mt-0.5 text-[10px] text-text-4 font-mono truncate"
            title={node.signature}
          >
            {node.signature}
          </div>
        )}

        {/* kind / subtype · collection — small muted meta line. */}
        <div
          className="text-[10px] text-text-4 font-mono truncate"
          title={`${node.kind}${node.subtype ? ` · ${node.subtype}` : ""}${node.collection ? ` · ${node.collection}` : ""}`}
        >
          {node.kind}
          {node.subtype && node.subtype !== node.kind && (
            <span className="text-text-3"> · {node.subtype}</span>
          )}
          {node.collection && (
            <span className="text-text-3"> · {node.collection}</span>
          )}
        </div>

        {/* file:line — click opens the shared source-peek modal (#4499). */}
        {fileRef && (
          <button
            type="button"
            onClick={(e) => {
              // Don't let the node-select / canvas handlers swallow the click.
              e.stopPropagation();
              if (node.file && groupId) {
                openSourcePeek({
                  groupId,
                  file: node.file,
                  line: node.line ?? 0,
                  repo: node.repo,
                });
              }
            }}
            className="mt-0.5 flex items-center font-mono text-[10px] text-text-4 tabular-nums min-w-0 w-full text-left cursor-pointer hover:text-accent"
            title={`${fileRef} — open source`}
          >
            {/* head (dir prefix) truncates LTR; tail (filename:line) never shrinks. */}
            <span className="overflow-hidden whitespace-nowrap text-ellipsis min-w-0 shrink">
              {fileHead}
            </span>
            <span className="shrink-0 whitespace-nowrap">{fileTail}</span>
          </button>
        )}

        {/* effect badges — small, only what's present. */}
        {effects.length > 0 && (
          <div className="mt-1 flex flex-wrap gap-1">
            {effects.map((eff) => {
              const b = effectBadge(eff);
              return (
                <span
                  key={eff}
                  className={cn(
                    "inline-flex items-center h-[15px] px-1 rounded text-[9px] font-medium leading-none",
                    b.cls,
                  )}
                  title={eff}
                >
                  {b.label}
                </span>
              );
            })}
          </div>
        )}

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
            +{collapsed.length} collapsed
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
