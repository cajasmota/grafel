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
import {
  ChevronRight,
  ChevronDown,
  CircleDot,
  Flag,
  MoreHorizontal,
  PackageOpen,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { RepoChip } from "@/lib/repo-color";
import { effectBadge } from "@/lib/effect-badge";
import { useSourcePeek } from "@/components/SourcePeek";
import { nodeStyle, isExternalNode, moduleBand, edgeStyle } from "./style";
import { SOURCE_HANDLE_ID, TARGET_HANDLE_ID, type FlowDagNodeData } from "./layout";

/**
 * FlowDagNode — one DAG node. Color + label come from the role; terminal
 * nodes get a doubled ring so collection sinks read as endpoints of the walk.
 * The collapsed-children count badge expands an inline list on click.
 */
function FlowDagNodeImpl({ id, data, sourcePosition, targetPosition }: NodeProps) {
  const {
    node,
    edgeKind,
    expanded,
    onToggleExpand,
    selected,
    onRoute,
    isLeaf,
    truncatedHere,
    module: mod,
    replay,
  } = data as FlowDagNodeData;
  // Taxonomy bucket → tint (#4566): exception=red (#4556), external=muted
  // (#4558/#4564), genuine leaf=Return/finish end-cap (#4561), else role/kind.
  const rs = nodeStyle(node, edgeKind, isLeaf);
  // #4558/#4564: an external / unresolved node — render muted, label clearly,
  // and replace the (broken) source-peek with an 'external symbol' note.
  const external = isExternalNode(node);
  // #4561: a genuine terminal end-cap (NOT a branch merely cut by depth).
  const terminalCap = !!isLeaf;
  // #4557: per-module color band so same-module nodes read as a unit.
  const band = moduleBand(mod);
  // #4558(a): if the callee text is unnamed, fall back to a clear label rather
  // than an empty card. A truly nameless external reads as 'external symbol'.
  const displayName =
    node.name && node.name.trim() !== "" && node.name !== "<...>"
      ? node.name
      : external
        ? "external symbol"
        : "(unnamed)";
  const { openSourcePeek } = useSourcePeek();
  const { groupId = "" } = useParams<{ groupId: string }>();
  const collapsed = node.collapsed_children ?? [];
  const hasCollapsed = collapsed.length > 0;
  // Click-to-highlight (#4479): when a route is active, off-route nodes dim.
  // Step-replay (#4362): a pending (not-yet-reached) node dims like an off-route
  // node; the active node glows; traversed nodes stay at full opacity (trail).
  const dimmed = onRoute === false || replay === "pending";
  const replayActive = replay === "active";

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
        // #4362: CSS-only arrival bounce on the active replay node. Cheap
        // transform animation, respects prefers-reduced-motion via the keyframe.
        replayActive && "flow-replay-bounce",
      )}
      style={{
        // Role/taxonomy tint as a soft background wash (#4566). The border is a
        // more INTENSE shade of the card's OWN bucket hue (#4617): the bucket's
        // solid pastel token (rs.border) deepened toward its dark anchor so the
        // outline reads as a saturated version of the background, never a
        // generic neutral/brown frame. External nodes stay muted.
        background: `color-mix(in srgb, ${rs.bg} ${external ? 12 : 22}%, var(--surface))`,
        borderColor: selected || onRoute === true || replayActive
          ? "var(--accent)"
          : external
            ? "color-mix(in srgb, var(--text-4) 45%, transparent)"
            : `color-mix(in srgb, ${rs.border} 72%, var(--text))`,
        // #4557: a left module color-band groups same-module nodes visually.
        borderLeft: band ? `3px solid ${band.color}` : undefined,
        // Selected / route-lit (#4479) → accent ring; a genuine terminal
        // end-cap (#4561) or a collection sink → a heavier outline ring.
        boxShadow: replayActive
          ? "0 0 0 2px var(--accent), 0 0 16px 2px color-mix(in srgb, var(--accent) 55%, transparent)"
          : selected || onRoute === true
          ? "0 0 0 2px var(--accent)"
          : terminalCap || node.terminal
            ? `0 0 0 2px color-mix(in srgb, ${rs.border} 60%, var(--text))`
            : undefined,
      }}
    >
      {/* in/out handles — positions follow the H/V layout direction (TB →
          top/bottom, LR → left/right). Stable ids ({@link TARGET_HANDLE_ID} /
          {@link SOURCE_HANDLE_ID}) so the edge binds to THIS centered handle
          deterministically: with multiple handles per type, React Flow's
          fallback "pick any handle of this type" is unspecified, which is how a
          TB edge could resolve against a stale Left/Right anchor and dock on the
          side (#4882). The edges set sourceHandle/targetHandle to match. */}
      <Handle
        id={TARGET_HANDLE_ID}
        type="target"
        position={targetPosition ?? Position.Left}
        className="!bg-text-4"
      />
      <Handle
        id={SOURCE_HANDLE_ID}
        type="source"
        position={sourcePosition ?? Position.Right}
        className="!bg-text-4"
      />

      <div className="px-2.5 py-2">
        {/* Header — two tidy rows (#4816). Cramming kind + role + terminal +
            repo into ONE row clipped the load-bearing badges ('finish'→'F',
            'handler'→'handl'). Split into:
              row 1 — KIND badge + terminal end-cap (both must stay fully
                      readable, so they never truncate / shrink),
              row 2 — incoming-role word + repo chip (the least-critical info;
                      repo truncates gracefully with a hover title).
            Each row clips to the card width (min-w-0 + overflow-hidden) so
            nothing bleeds past the card's right edge. */}
        <div className="flex flex-col gap-1 min-w-0">
          {/* Row 1: KIND badge + terminal marker — kept fully readable. */}
          <div className="flex items-center gap-1.5 min-w-0 overflow-hidden">
            <span
              className="inline-flex items-center h-[15px] px-1 rounded text-[9px] font-semibold uppercase tracking-wide leading-none shrink-0"
              style={{
                background: `color-mix(in srgb, ${rs.bg} 38%, transparent)`,
                color: rs.ink,
              }}
            >
              {rs.label}
            </span>
            {/* #4561: a genuine return/finish end-cap reads distinctly from a
                collection sink and from a depth-truncated branch. */}
            {terminalCap && (
              <span
                className="inline-flex items-center gap-0.5 h-[15px] px-1 rounded text-[9px] font-semibold uppercase tracking-wide leading-none shrink-0"
                style={{
                  background: `color-mix(in srgb, ${rs.bg} 38%, transparent)`,
                  color: rs.ink,
                }}
                title="Return / finish — this branch terminates here"
              >
                <Flag size={9} /> end
              </span>
            )}
            {!terminalCap && node.terminal && (
              <CircleDot size={11} className="shrink-0" style={{ color: rs.ink }} aria-label="terminal" />
            )}
          </div>
          {/* Row 2: incoming-role word + repo chip (least critical → truncates). */}
          {(edgeLabel || node.repo) && (
            <div className="flex items-center gap-1.5 min-w-0 overflow-hidden">
              {edgeLabel && (
                <span
                  className="inline-flex items-center h-[15px] px-1 rounded text-[9px] font-medium lowercase leading-none min-w-0 truncate text-text-3 bg-surface-2"
                  title={`incoming relationship: ${edgeLabel}`}
                >
                  {edgeLabel}
                </span>
              )}
              {node.repo && <RepoChip slug={node.repo} className="ml-auto" maxLength={18} />}
            </div>
          )}
        </div>

        {/* Name — responsive size, wraps to 2 lines, full name on hover. An
            external/unnamed callee gets a clear label instead of an empty card
            (#4558a). */}
        <div
          className={cn(
            "mt-1 text-[11px] leading-tight font-medium break-words line-clamp-2",
            external ? "text-text-3 italic" : "text-text",
          )}
          title={displayName}
        >
          {displayName}
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

        {/* #4558(b)/#4564: external/unresolved nodes can't be source-peeked.
            Show an honest 'external symbol' note (with the package name when the
            node carries one) instead of a broken peek button. */}
        {external ? (
          <div
            className="mt-0.5 flex items-center gap-1 font-mono text-[10px] text-text-4 min-w-0"
            title={`External symbol — defined outside indexed source${
              node.package ? ` (${node.package})` : ""
            }`}
          >
            <PackageOpen size={10} className="shrink-0" />
            <span className="truncate">
              external symbol{node.package ? ` · ${node.package}` : ""}
            </span>
          </div>
        ) : (
          fileRef && (
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
          )
        )}

        {/* #4561: a branch CUT by the depth control (not a real leaf) keeps a
            'more downstream' affordance so it's clearly distinct from a terminal. */}
        {truncatedHere && (
          <div
            className="mt-1 inline-flex items-center gap-1 text-[10px] text-text-4"
            title="More downstream nodes exist below the depth limit — raise depth to expand."
          >
            <MoreHorizontal size={11} className="shrink-0" />
            more downstream
          </div>
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
