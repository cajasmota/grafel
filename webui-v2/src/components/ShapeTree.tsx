/* ============================================================
   components/ShapeTree.tsx — unified collapsible subtree for
   Parameters + Response sections (#1935 Phase 1).

   A ShapeTree renders one indented subtree per top-level entry
   (a request body / path / query parameter, or a response row).
   When a row carries `type_entity_id` and `has_children=true` the
   user can click the expand glyph to fetch the field list of that
   class via GET /api/v2/groups/:id/shape and render the children
   as indented sub-rows. Nested types are recursive — clicking a
   child whose type is itself expandable fetches its subtree.

   Visual contract:
     ▾ request   body   TransferRequest        Request body…
        ├ transferId       String       @NotBlank
        ├ confirmedQty     BigDecimal   @Min(0)
        └ items            List<ItemDTO>  ▸ (expandable)

   The component takes raw top-level rows so it can be reused by
   the Parameters section (PathParameter[] inputs) and the
   Response section (ResponseShape[] inputs) without each call
   site reimplementing the expansion logic.
   ============================================================ */

import { useState, type ReactNode } from "react";
import { ChevronRight, ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";
import { useShape } from "@/hooks/use-paths";
import {
  indentForDepth,
  fieldTypeLabel,
  fieldOptionality,
} from "@/lib/shape-tree-indent";

/**
 * A top-level row consumed by ShapeTree. Both PathParameter and
 * ResponseShape are projected into this shape by the caller so the
 * component does not need to know which section it's rendering.
 */
export interface ShapeTreeRow {
  /** Display name (e.g. parameter name, "response", or `${verb} ${statusCode}`). */
  name: string;
  /** Small categorical tag rendered as a chip ("path", "body", "200", "201", …). */
  inLabel?: string;
  /**
   * Tone for the chip — colour-coded per the spec (#2113):
   *   path   → amber/orange
   *   query  → violet
   *   body   → green
   *   header → blue
   *   cookie → slate
   *   status → neutral
   */
  inTone?: "path" | "query" | "body" | "header" | "cookie" | "form" | "status";
  /** Type token (e.g. "TransferRequest", "List<UserDTO>", "String"). */
  type: string;
  /** Optional inline description (right-most column). */
  desc?: string;
  /** Required marker → trailing red asterisk on the name. */
  required?: boolean;
  /** Prefixed entity id (`slug:id`) — present when the type resolves to a class. */
  type_entity_id?: string;
  /** Fallback name lookup when type_entity_id is absent. */
  type_name_fallback?: string;
  /** When true the expand glyph renders and the row is clickable. */
  has_children?: boolean;
  /** Stable key for React + selection. */
  key: string;
}

interface ShapeTreeProps {
  groupId: string;
  rows: ShapeTreeRow[];
  /** Optional empty-state copy. Defaults to "None". */
  emptyText?: string;
}

export function ShapeTree({ groupId, rows, emptyText = "None" }: ShapeTreeProps) {
  if (rows.length === 0) {
    return <p className="text-xs text-text-4 py-1 px-4">{emptyText}</p>;
  }
  return (
    <div
      className="py-1 text-xs"
      data-testid="shape-tree"
      /*
       * CSS grid column layout per #2113 spec:
       *   col 1 — chevron glyph:  24px fixed (only on expandable rows)
       *   col 2 — [in] chip:      64px min-width fixed
       *   col 3 — name + *:      180px fixed, ellipsis overflow
       *   col 4 — type chip:     200px fixed, monospaced
       *   col 5 — description:   1fr (remaining space)
       *
       * The grid is set here on the container; each ShapeTreeNode renders
       * its cells as direct children with `display: contents` so they
       * participate in the same grid track.
       *
       * Note: nested (child) rows use a plain flex layout since they don't
       * need the [in] column.
       */
    >
      {rows.map((row) => (
        <ShapeTreeNode key={row.key} groupId={groupId} row={row} depth={0} />
      ))}
    </div>
  );
}

/**
 * One top-level row (Parameters or Response top row) — non-recursive.
 *
 * Column layout (#2113):
 *   [chevron 24px] | [in chip min-64px] | [name 180px] | [type 200px] | [desc 1fr]
 *
 * Uses a flex row with explicit widths rather than CSS grid (grid + display:contents
 * has poor Safari 15 support; flex is safer for the nested/indented case too).
 */
function ShapeTreeNode({
  groupId,
  row,
  depth,
}: {
  groupId: string;
  row: ShapeTreeRow;
  depth: number;
}) {
  const [open, setOpen] = useState(false);
  const expandable = !!row.has_children;
  return (
    <div data-testid={`shape-row-${row.key}`}>
      <div
        className={cn(
          "flex items-center gap-0 py-1.5 px-4 border-b border-border-soft last:border-0 min-w-0",
          expandable && "cursor-pointer hover:bg-surface-2/40",
        )}
        onClick={() => expandable && setOpen((v) => !v)}
        role={expandable ? "button" : undefined}
        tabIndex={expandable ? 0 : -1}
        onKeyDown={(e) => {
          if (!expandable) return;
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOpen((v) => !v);
          }
        }}
        style={{ paddingLeft: `calc(1rem + ${depth * 16}px)` }}
      >
        {/* Col 1 — chevron, 24px */}
        <span className="w-6 shrink-0 flex items-center justify-center">
          <ExpandGlyph expandable={expandable} open={open} />
        </span>

        {/* Col 2 — [in] chip, min-w-16 (64px) */}
        <span
          className="shrink-0 mr-2"
          style={{ minWidth: 64 }}
        >
          {row.inLabel ? (
            <span
              data-testid={`in-chip-${row.inLabel}`}
              className={cn(
                "inline-flex items-center justify-center px-1.5 py-0.5 rounded-sm text-[10px] font-medium font-sans w-full",
                inToneClass(row.inTone),
              )}
            >
              {row.inLabel}
            </span>
          ) : null}
        </span>

        {/* Col 3 — name, 180px, ellipsis */}
        <span
          className="shrink-0 font-mono text-text overflow-hidden text-ellipsis whitespace-nowrap mr-2"
          style={{ width: 180 }}
          title={row.name + (row.required ? " (required)" : "")}
        >
          {row.name}
          {row.required && <span className="text-danger ml-0.5">*</span>}
        </span>

        {/* Col 4 — type chip, 200px, monospaced. Expandable object types are
            rendered as an accented, underlined "link" so the field-expansion
            affordance is discoverable even before the chevron is noticed (#4606). */}
        <span
          className={cn(
            "shrink-0 font-mono overflow-hidden text-ellipsis whitespace-nowrap mr-2",
            expandable
              ? "text-accent underline decoration-dotted underline-offset-2"
              : "text-text-3",
          )}
          style={{ width: 200 }}
          title={expandable ? `${row.type} — click to expand fields` : row.type}
        >
          {row.type}
        </span>

        {/* Col 5 — description, flex-1 */}
        {row.desc ? (
          <span className="text-text-3 leading-snug truncate flex-1 font-sans text-[11px]" title={row.desc}>
            {row.desc}
          </span>
        ) : (
          <span className="flex-1" />
        )}
      </div>
      {open && expandable && (
        <NestedFieldRows
          groupId={groupId}
          typeEntityId={row.type_entity_id}
          typeName={row.type_name_fallback}
          depth={depth + 1}
        />
      )}
    </div>
  );
}

/**
 * Lazy fetches the children of an expandable row and renders one row
 * per CONTAINS field. Nested expandable rows recurse via the same
 * component.
 */
function NestedFieldRows({
  groupId,
  typeEntityId,
  typeName,
  depth,
}: {
  groupId: string;
  typeEntityId?: string;
  typeName?: string;
  depth: number;
}) {
  const { data, isLoading, isError, error } = useShape(
    groupId,
    { typeEntityId, type: typeName },
    true,
  );
  if (isLoading) {
    return (
      <TreeGuide depth={depth}>
        <div className="py-1 px-2 text-text-4">loading…</div>
      </TreeGuide>
    );
  }
  if (isError) {
    return (
      <TreeGuide depth={depth}>
        <div className="py-1 px-2 text-danger">
          Failed to load: {(error as Error)?.message ?? "unknown"}
        </div>
      </TreeGuide>
    );
  }
  const rows = data?.rows ?? [];
  if (rows.length === 0) {
    return (
      <TreeGuide depth={depth}>
        <div className="py-1 px-2 text-text-4">(no fields)</div>
      </TreeGuide>
    );
  }
  return (
    <TreeGuide depth={depth}>
      {rows.map((field) => (
        <NestedFieldRow
          key={field.name}
          groupId={groupId}
          field={field}
          depth={depth}
        />
      ))}
    </TreeGuide>
  );
}

/**
 * Indents one nesting level and draws the vertical tree-guide rail so a
 * field list reads as a hierarchy under its parent row rather than as a
 * flat, left-aligned block (#4858). The left border is the rail; each
 * child row paints its own horizontal "elbow" connector.
 */
function TreeGuide({ depth, children }: { depth: number; children: ReactNode }) {
  return (
    <div
      data-testid="shape-tree-guide"
      className="border-l border-border-soft"
      style={{ marginLeft: indentForDepth(depth) + 4 }}
    >
      {children}
    </div>
  );
}

function NestedFieldRow({
  groupId,
  field,
  depth,
}: {
  groupId: string;
  field: import("@/data/types").ShapeRow;
  depth: number;
}) {
  const [open, setOpen] = useState(false);
  const expandable = field.has_children;
  const optional = !!field.nullable;
  return (
    <div>
      <div
        data-testid={`shape-field-${field.name}`}
        data-depth={depth}
        className={cn(
          "flex items-center gap-2 py-1 pl-3 pr-2 border-b border-border-soft last:border-0 min-w-0",
          // horizontal elbow connecting the rail to the row content
          "before:content-[''] before:w-2 before:h-px before:bg-border-soft before:shrink-0 before:-ml-3",
          expandable && "cursor-pointer hover:bg-surface-2/40",
        )}
        onClick={() => expandable && setOpen((v) => !v)}
        role={expandable ? "button" : undefined}
        tabIndex={expandable ? 0 : -1}
        onKeyDown={(e) => {
          if (!expandable) return;
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOpen((v) => !v);
          }
        }}
      >
        <ExpandGlyph expandable={expandable} open={open} />
        <span className="font-mono text-text shrink-0">{field.name}</span>
        <span
          className={cn(
            "font-mono shrink-0",
            expandable
              ? "text-accent underline decoration-dotted underline-offset-2"
              : "text-text-3",
          )}
          title={expandable ? `${field.type} — click to expand fields` : field.type}
        >
          {fieldTypeLabel(field.type, field.nullable)}
        </span>
        <span
          data-testid={`shape-field-optionality-${field.name}`}
          className={cn(
            "inline-flex items-center px-1 py-0.5 rounded-sm text-[10px] font-medium font-sans shrink-0",
            optional
              ? "bg-surface-2 text-text-4 border border-border"
              : "bg-[var(--success-soft)] text-[var(--success)] border border-[var(--success-soft)]",
          )}
          title={fieldOptionality(field.nullable)}
        >
          {optional ? "optional" : "required"}
        </span>
        {field.annotations && field.annotations.length > 0 && (
          <span className="text-text-4 truncate font-mono text-[11px]">
            {field.annotations.join(" ")}
          </span>
        )}
      </div>
      {open && expandable && (
        <NestedFieldRows
          groupId={groupId}
          typeEntityId={field.type_entity_id}
          depth={depth + 1}
        />
      )}
    </div>
  );
}

function ExpandGlyph({ expandable, open }: { expandable: boolean; open: boolean }) {
  if (!expandable) {
    return <span className="w-3.5 shrink-0" />;
  }
  return open ? (
    <ChevronDown size={12} className="text-text-3 shrink-0 mt-0.5" />
  ) : (
    <ChevronRight size={12} className="text-text-3 shrink-0 mt-0.5" />
  );
}

/**
 * Colour-coded chip classes for the `[in]` column (#2113 spec):
 *   path   → amber/orange (warning palette)
 *   query  → violet       (pastel-5)
 *   body   → green        (success palette)
 *   header → blue         (info palette)
 *   cookie → slate        (surface-2 / neutral)
 *   form   → teal         (pastel-2)
 *   status → neutral
 */
function inToneClass(tone?: ShapeTreeRow["inTone"]): string {
  switch (tone) {
    case "path":
      return "bg-warning-soft text-warning border border-warning-soft";
    case "query":
      return "bg-[var(--pastel-5)] text-[var(--pastel-5-ink)] border border-[var(--pastel-5)]";
    case "body":
      return "bg-[var(--success-soft)] text-[var(--success)] border border-[var(--success-soft)]";
    case "header":
      return "bg-[var(--info-soft)] text-[var(--info)] border border-[var(--info-soft)]";
    case "cookie":
      return "bg-surface-2 text-text-3 border border-border-strong";
    case "form":
      return "bg-[var(--pastel-2)] text-[var(--pastel-2-ink)] border border-[var(--pastel-2)]";
    case "status":
      return "bg-surface-2 text-text-3 border border-border";
    default:
      return "bg-surface-2 text-text-3 border border-border";
  }
}
