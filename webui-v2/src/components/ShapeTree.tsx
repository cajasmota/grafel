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

import { useState } from "react";
import { ChevronRight, ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";
import { useShape } from "@/hooks/use-paths";

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
  /** Tone for the chip — uses Paths page CSS vars. */
  inTone?: "path" | "query" | "body" | "header" | "status";
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
    <div className="px-4 py-2 text-xs font-mono">
      {rows.map((row) => (
        <ShapeTreeNode key={row.key} groupId={groupId} row={row} depth={0} />
      ))}
    </div>
  );
}

/**
 * One top-level row (Parameters or Response top row) — non-recursive.
 * The recursive subtree (one level of fields fetched on demand) is
 * handled by NestedFieldRows; this stays specialised for the
 * top-level row's richer chrome (`in` chip + description column).
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
    <div>
      <div
        className={cn(
          "flex items-start gap-2 py-1 border-b border-border-soft last:border-0",
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
        style={{ paddingLeft: depth * 16 }}
      >
        <ExpandGlyph expandable={expandable} open={open} />
        <span className="font-mono text-text shrink-0">
          {row.name}
          {row.required && <span className="text-danger ml-0.5">*</span>}
        </span>
        {row.inLabel && (
          <span className={cn("px-1.5 py-0.5 rounded-sm text-[10px] font-medium shrink-0", inToneClass(row.inTone))}>
            {row.inLabel}
          </span>
        )}
        <span className="font-mono text-text-3 shrink-0">{row.type}</span>
        {row.desc && (
          <span className="text-text-3 leading-snug truncate flex-1 ml-2 font-sans">
            {row.desc}
          </span>
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
      <div className="py-1 text-text-4" style={{ paddingLeft: depth * 16 + 16 }}>
        loading…
      </div>
    );
  }
  if (isError) {
    return (
      <div className="py-1 text-danger" style={{ paddingLeft: depth * 16 + 16 }}>
        Failed to load: {(error as Error)?.message ?? "unknown"}
      </div>
    );
  }
  const rows = data?.rows ?? [];
  if (rows.length === 0) {
    return (
      <div className="py-1 text-text-4" style={{ paddingLeft: depth * 16 + 16 }}>
        (no fields)
      </div>
    );
  }
  return (
    <div>
      {rows.map((field) => (
        <NestedFieldRow
          key={field.name}
          groupId={groupId}
          field={field}
          depth={depth}
        />
      ))}
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
  return (
    <div>
      <div
        className={cn(
          "flex items-start gap-2 py-1 border-b border-border-soft last:border-0",
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
        style={{ paddingLeft: depth * 16 }}
      >
        <ExpandGlyph expandable={expandable} open={open} />
        <span className="font-mono text-text shrink-0">
          {field.name}
          {field.nullable && <span className="text-text-4 ml-0.5">?</span>}
        </span>
        <span className="font-mono text-text-3 shrink-0">{field.type}</span>
        {field.annotations && field.annotations.length > 0 && (
          <span className="text-text-4 truncate">{field.annotations.join(" ")}</span>
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

function inToneClass(tone?: ShapeTreeRow["inTone"]): string {
  switch (tone) {
    case "path":
      return "bg-[var(--info-soft)] text-[var(--info)]";
    case "query":
      return "bg-[var(--pastel-1)] text-[var(--pastel-1-ink)]";
    case "body":
      return "bg-[var(--success-soft)] text-[var(--success)]";
    case "header":
      return "bg-surface-2 text-text-3";
    case "status":
      return "bg-surface-2 text-text-3";
    default:
      return "bg-surface-2 text-text-3";
  }
}
