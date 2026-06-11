/* ============================================================
   lib/shape-tree-indent.ts — pure geometry + label helpers for
   the ShapeTree component (#4858).

   Expanding a DTO row reveals its fields, but prior to #4858 the
   children rendered flat (no indentation/hierarchy) and showed no
   type. These helpers centralise the depth→indent math and the
   field type/optionality label logic so the rendering stays a
   thin, testable layer and the visual contract is unit-checked
   without a DOM.
   ============================================================ */

/** px of horizontal indent per nesting level. */
export const SHAPE_INDENT_STEP = 16;

/**
 * Horizontal indent (in px) for a nested field row at the given depth.
 * Top-level rows are depth 0; their direct fields are depth 1, etc.
 */
export function indentForDepth(depth: number): number {
  return Math.max(0, depth) * SHAPE_INDENT_STEP;
}

/**
 * Compact type token shown next to a field name, e.g. `string` or
 * `number | null` for an optional/nullable field. Falls back to the
 * bare type when no type string is available.
 */
export function fieldTypeLabel(type: string | undefined, nullable: boolean | undefined): string {
  const base = (type ?? "").trim() || "unknown";
  if (!nullable) return base;
  // Don't double-annotate a type that already advertises nullability.
  if (/\bnull\b/.test(base) || base.endsWith("?")) return base;
  return `${base} | null`;
}

/** Optionality marker for a field — `optional` when nullable, else `required`. */
export function fieldOptionality(nullable: boolean | undefined): "optional" | "required" {
  return nullable ? "optional" : "required";
}
