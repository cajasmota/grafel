/* ============================================================
   components/flow-dag/style.ts — role + edge-kind visual vocabulary.

   Single source of truth for how the downstream-DAG looks, so the node
   renderer, edge renderer, and legend all agree. All colors route through the
   shared design tokens (tokens.css) — no hardcoded hex — so theme switching is
   free.
   ============================================================ */

import type {
  DownstreamDAGEdgeKind,
  DownstreamDAGNode,
  DownstreamDAGRole,
} from "@/data/types";

/** Per-role node styling: a pastel token pair + a short legend label. */
export interface RoleStyle {
  /** CSS background for the node body. */
  bg: string;
  /** CSS foreground (border + accent) for the node. */
  ink: string;
  label: string;
}

/**
 * Role → token pair. endpoint (the root) is the most prominent; collection
 * sinks read as terminal data stores; handler is the HTTP-boundary crossing;
 * node is a generic spine step.
 */
export const ROLE_STYLE: Record<DownstreamDAGRole, RoleStyle> = {
  endpoint: { bg: "var(--pastel-2)", ink: "var(--pastel-2-ink)", label: "Endpoint" },
  handler: { bg: "var(--pastel-1)", ink: "var(--pastel-1-ink)", label: "Handler" },
  node: { bg: "var(--pastel-5)", ink: "var(--pastel-5-ink)", label: "Node" },
  collection: { bg: "var(--pastel-3)", ink: "var(--pastel-3-ink)", label: "Collection" },
};

/** Fallback for an unexpected/missing role — render as a generic node. */
export function roleStyle(role: DownstreamDAGRole | undefined): RoleStyle {
  return (role && ROLE_STYLE[role]) || ROLE_STYLE.node;
}

/**
 * Exception/error node styling (#4556). A red tint + red ink so a node where an
 * error is raised stands out from the neutral spine. The hue matches the red
 * THROWS edge (--danger) so a thrown error reads consistently node→edge; the
 * ink is a contrast-tuned red (--exception-ink) that stays legible as the pill
 * text + border over the @22% danger wash in BOTH light and dark themes.
 */
export const EXCEPTION_STYLE: RoleStyle = {
  bg: "var(--danger)",
  ink: "var(--exception-ink)",
  label: "Exception",
};

/**
 * Detect whether a node represents an exception/error site so it can be painted
 * red. Conservative, signal-based (#4556): the node's KIND is ExceptionType, OR
 * its name carries the canonical "exception:" prefix, OR the parent reached it
 * via a THROWS edge (it's the target of a throw). We deliberately do NOT paint
 * every node whose name merely contains "Error"/"Exception" — that over-paints
 * ordinary error-handling helpers. `incomingEdgeKind` is the kind of the single
 * in-edge feeding this rendered instance (undefined for the root).
 */
export function isExceptionNode(
  node: Pick<DownstreamDAGNode, "kind" | "name">,
  incomingEdgeKind?: DownstreamDAGEdgeKind,
): boolean {
  return (
    node.kind === "ExceptionType" ||
    node.name.startsWith("exception:") ||
    incomingEdgeKind === "THROWS"
  );
}

/**
 * Resolve the node body style: an exception/error node gets the red EXCEPTION
 * palette; otherwise it falls back to its role tint. Used by the node renderer.
 */
export function nodeStyle(
  node: Pick<DownstreamDAGNode, "kind" | "name" | "role">,
  incomingEdgeKind?: DownstreamDAGEdgeKind,
): RoleStyle {
  return isExceptionNode(node, incomingEdgeKind)
    ? EXCEPTION_STYLE
    : roleStyle(node.role);
}

/** Per-edge-kind styling: stroke color, dashed?, and a short label. */
export interface EdgeStyle {
  stroke: string;
  /** SVG dash pattern, or undefined for a solid line. */
  dash?: string;
  label: string;
}

/**
 * Edge kind → styling. The CALLS spine is solid + neutral; the HTTP-boundary
 * crossing (HANDLER_CONTINUATION) is accent + solid; the semantic side-edges
 * are dashed + semantically colored so they read as branches off the spine:
 *   JOINS_COLLECTION → success (data sink), THROWS → danger, VALIDATES → warn.
 */
export const EDGE_STYLE: Record<DownstreamDAGEdgeKind, EdgeStyle> = {
  CALLS: { stroke: "var(--text-4)", label: "calls" },
  HANDLER_CONTINUATION: { stroke: "var(--accent)", label: "handler" },
  JOINS_COLLECTION: { stroke: "var(--success)", dash: "5 4", label: "joins" },
  THROWS: { stroke: "var(--danger)", dash: "5 4", label: "throws" },
  VALIDATES: { stroke: "var(--info)", dash: "5 4", label: "validates" },
};

export function edgeStyle(kind: DownstreamDAGEdgeKind): EdgeStyle {
  return EDGE_STYLE[kind] ?? EDGE_STYLE.CALLS;
}
