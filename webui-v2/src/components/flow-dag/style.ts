/* ============================================================
   components/flow-dag/style.ts — node taxonomy + edge-kind visual vocabulary.

   Single source of truth for how the downstream-DAG looks, so the node
   renderer, edge renderer, and legend all agree. All colors route through the
   shared design tokens (tokens.css) — no hardcoded hex — so theme switching is
   free.

   #4566: the node palette is now a data-driven TAXONOMY. Each rendered node is
   classified into ~8-10 buckets derived from its role + kind + subtype +
   effects (never from a framework name), and each bucket carries a pastel token
   pair + legend label. Rare/unknown kinds fold into a neutral 'Node'. The
   exception bucket (#4556, red) and the terminal end-cap (#4561) are part of the
   same map so node, edge, and legend stay in lockstep.
   ============================================================ */

import type {
  DownstreamDAGEdgeKind,
  DownstreamDAGNode,
  DownstreamDAGRole,
} from "@/data/types";

/* ------------------------------------------------------------------
   Node taxonomy (#4566)
   ------------------------------------------------------------------ */

/**
 * The visual bucket a node falls into. Capped at ~8-10 so the legend stays
 * scannable; everything that doesn't classify folds into `node` (the neutral
 * spine step). Derivation is signal-based (role/kind/subtype/effects), never
 * framework-name based, so it generalizes across every indexed stack.
 */
export type NodeBucket =
  | "endpoint"
  | "handler"
  | "service"
  | "repository"
  | "schema"
  | "function"
  | "exception"
  | "external"
  | "terminal"
  | "collection"
  | "node";

/** Per-bucket styling: a pastel token pair + a short legend label. */
export interface RoleStyle {
  /** CSS background for the node body. */
  bg: string;
  /** CSS foreground (text/accent) for the node. */
  ink: string;
  /**
   * CSS border color for the node card — derived from the SAME bucket color
   * family as `bg`, but more intense/saturated (the solid pastel token rather
   * than the washed @22% bg) so the outline reads as a darker shade of the
   * card's own hue instead of a generic neutral/brown frame (#4617).
   */
  border: string;
  label: string;
}

/**
 * Bucket → token pair + label. Endpoint (the root) is the most prominent;
 * handler is the HTTP-boundary crossing; service/repository/schema/function
 * split the body of the spine by what the node DOES; exception is red (#4556);
 * external is muted (#4558/#4564); terminal is the return/finish end-cap
 * (#4561); collection sinks read as terminal data stores; node is the neutral
 * fallback.
 */
export const NODE_BUCKET_STYLE: Record<NodeBucket, RoleStyle> = {
  endpoint: { bg: "var(--pastel-2)", ink: "var(--pastel-2-ink)", border: "var(--pastel-2)", label: "Endpoint" },
  handler: { bg: "var(--pastel-1)", ink: "var(--pastel-1-ink)", border: "var(--pastel-1)", label: "Handler" },
  service: { bg: "var(--pastel-5)", ink: "var(--pastel-5-ink)", border: "var(--pastel-5)", label: "Service / logic" },
  repository: { bg: "var(--pastel-4)", ink: "var(--pastel-4-ink)", border: "var(--pastel-4)", label: "Repository / data" },
  schema: { bg: "var(--pastel-6)", ink: "var(--pastel-6-ink)", border: "var(--pastel-6)", label: "DTO / schema" },
  function: { bg: "var(--pastel-7)", ink: "var(--pastel-7-ink)", border: "var(--pastel-7)", label: "Function" },
  exception: { bg: "var(--danger)", ink: "var(--exception-ink)", border: "var(--danger)", label: "Exception" },
  external: { bg: "var(--text-4)", ink: "var(--text-3)", border: "var(--text-4)", label: "External / library" },
  terminal: { bg: "var(--pastel-8)", ink: "var(--pastel-8-ink)", border: "var(--pastel-8)", label: "Return / finish" },
  collection: { bg: "var(--pastel-3)", ink: "var(--pastel-3-ink)", border: "var(--pastel-3)", label: "Collection" },
  node: { bg: "var(--pastel-5)", ink: "var(--pastel-5-ink)", border: "var(--pastel-5)", label: "Node" },
};

/**
 * Legacy role → bucket. The backend still ships a coarse `role`; we keep it as
 * a strong prior, then refine with kind/subtype/effects below.
 */
const ROLE_TO_BUCKET: Record<DownstreamDAGRole, NodeBucket> = {
  endpoint: "endpoint",
  handler: "handler",
  collection: "collection",
  node: "node",
};

/**
 * #4556 (kept): is this node an exception/error site? Conservative,
 * signal-based: the node's KIND is ExceptionType, OR its name carries the
 * canonical "exception:" prefix, OR the parent reached it via a THROWS edge.
 * We deliberately do NOT paint every node whose name merely contains
 * "Error"/"Exception" — that over-paints ordinary error-handling helpers.
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
 * #4558/#4564: is this node EXTERNAL / unresolved — i.e. defined outside the
 * indexed source? Signal-based: an explicit external flag, no resolvable
 * source file (empty / `<...>` placeholder), or a `scope.operation` /
 * synthetic-scope name the backend uses for symbols it couldn't bind. These
 * nodes can't be source-peeked and should read muted, not as an empty card.
 *
 * (We do NOT treat ExceptionType as external here — exceptions are their own
 * red bucket and take precedence.)
 */
export function isExternalNode(
  node: Pick<DownstreamDAGNode, "kind" | "name" | "file"> & {
    external?: boolean;
  },
): boolean {
  if (node.external === true) return true;
  const file = (node.file ?? "").trim();
  // No file, or a synthetic placeholder like "<external>" / "<builtin>".
  if (file === "" || (file.startsWith("<") && file.endsWith(">"))) {
    // An empty file alone isn't conclusive (some real leaves omit it), so we
    // require a corroborating signal: a scope-style name or an explicit
    // external/unknown kind.
    if (
      node.name.startsWith("scope.") ||
      node.name.startsWith("<") ||
      node.kind === "External" ||
      node.kind === "ExternalSymbol" ||
      node.kind === "Unknown" ||
      node.kind === "Unresolved"
    ) {
      return true;
    }
  }
  return false;
}

/** Lowercased haystack of the classification signals, computed once. */
function sig(node: Pick<DownstreamDAGNode, "kind" | "subtype">): {
  kind: string;
  subtype: string;
} {
  return {
    kind: (node.kind ?? "").toLowerCase(),
    subtype: (node.subtype ?? "").toLowerCase(),
  };
}

/**
 * Classify a node into a taxonomy bucket (#4566). Precedence, highest first:
 *   1. exception (red, #4556)
 *   2. external / unresolved (muted, #4558/#4564)
 *   3. terminal end-cap (#4561) — only when the caller marks it a real leaf
 *   4. role==endpoint / handler / collection (strong backend prior)
 *   5. repository / data — db_read|db_write|cache effects, or a Repository kind
 *   6. schema / dto — Schema/DTO/Model kinds or subtypes
 *   7. service / logic — an Operation·method (a method on a service)
 *   8. function — an Operation·function (free function)
 *   9. node — neutral fallback
 *
 * `isLeaf` distinguishes a genuine terminal (real leaf / return) from a node
 * whose children were merely cut by the depth control; the latter must NOT get
 * the end-cap (it keeps its normal bucket + the 'more/depth' affordance).
 */
export function nodeBucket(
  node: Pick<DownstreamDAGNode, "kind" | "name" | "role" | "subtype" | "effects"> & {
    external?: boolean;
    file?: string;
  },
  incomingEdgeKind?: DownstreamDAGEdgeKind,
  isLeaf = false,
): NodeBucket {
  if (isExceptionNode(node, incomingEdgeKind)) return "exception";
  if (isExternalNode(node)) return "external";

  // A genuine terminal (real leaf / return) that isn't a known collection sink
  // gets the dedicated end-cap. role==collection keeps its own bucket below.
  if (isLeaf && node.role !== "collection" && node.role !== "endpoint") {
    return "terminal";
  }

  if (node.role && (node.role === "endpoint" || node.role === "handler" || node.role === "collection")) {
    return ROLE_TO_BUCKET[node.role];
  }

  const { kind, subtype } = sig(node);
  const effects = (node.effects ?? []).map((e) => e.toLowerCase());

  // Repository / data: any data effect, or an explicitly data-ish kind.
  const dataEffect = effects.some(
    (e) =>
      e.includes("db_read") ||
      e.includes("db_write") ||
      e.includes("db") ||
      e.includes("cache") ||
      e.includes("query"),
  );
  if (
    dataEffect ||
    kind.includes("repository") ||
    kind.includes("repo") ||
    subtype.includes("repository") ||
    subtype.includes("dao")
  ) {
    return "repository";
  }

  // DTO / schema: declarative data shapes.
  if (
    kind.includes("schema") ||
    kind.includes("dto") ||
    kind.includes("model") ||
    subtype.includes("schema") ||
    subtype.includes("dto") ||
    subtype.includes("model")
  ) {
    return "schema";
  }

  // Service vs free function: an Operation carries a method/function subtype.
  if (kind.includes("operation") || kind.includes("method") || kind.includes("function")) {
    if (subtype.includes("method") || kind.includes("method")) return "service";
    if (subtype.includes("function") || kind.includes("function")) return "function";
    // Bare Operation with no subtype reads as service/logic.
    return "service";
  }

  return "node";
}

/** Fallback for an unexpected/missing role — render as a generic node. */
export function roleStyle(role: DownstreamDAGRole | undefined): RoleStyle {
  return (role && NODE_BUCKET_STYLE[ROLE_TO_BUCKET[role]]) || NODE_BUCKET_STYLE.node;
}

/**
 * Exception/error node styling (#4556). Exposed for back-compat; equal to the
 * exception taxonomy bucket.
 */
export const EXCEPTION_STYLE: RoleStyle = NODE_BUCKET_STYLE.exception;

/**
 * Resolve the node body style from its taxonomy bucket (#4566). `isLeaf` lets
 * the renderer request the terminal end-cap for a genuine leaf (#4561).
 */
export function nodeStyle(
  node: Pick<DownstreamDAGNode, "kind" | "name" | "role" | "subtype" | "effects"> & {
    external?: boolean;
    file?: string;
  },
  incomingEdgeKind?: DownstreamDAGEdgeKind,
  isLeaf = false,
): RoleStyle {
  return NODE_BUCKET_STYLE[nodeBucket(node, incomingEdgeKind, isLeaf)];
}

/* ------------------------------------------------------------------
   Module grouping (#4557)
   ------------------------------------------------------------------ */

/** A node's module bucket: a stable key + a human label. */
export interface NodeModule {
  key: string;
  label: string;
}

/**
 * Derive a node's MODULE from its source-file path (#4557). Mirrors the
 * paths-view group-by-module logic (#4485): prefer a
 * `modules|apps|packages|services|domains/<name>` anchor segment, else the
 * file's directory, else a single shared "external"/"(ungrouped)" bucket for
 * nodes with no resolvable file (external/synthetic). Framework-agnostic.
 */
export function nodeModule(
  node: Pick<DownstreamDAGNode, "file" | "repo"> & { external?: boolean },
): NodeModule {
  const file = (node.file ?? "").replace(/\\/g, "/").trim();
  if (file === "" || (file.startsWith("<") && file.endsWith(">"))) {
    return { key: "__external__", label: "external" };
  }
  const segs = file.split("/").filter(Boolean);

  const ANCHORS = ["modules", "apps", "packages", "services", "domains"];
  for (let i = 0; i < segs.length - 1; i++) {
    if (ANCHORS.includes(segs[i].toLowerCase())) {
      const label = `${segs[i]}/${segs[i + 1]}`;
      return { key: `${node.repo}:${label.toLowerCase()}`, label };
    }
  }

  if (segs.length > 1) {
    const dir = segs.slice(0, -1).join("/");
    return { key: `${node.repo}:${dir.toLowerCase()}`, label: dir };
  }

  return { key: `${node.repo}:__root__`, label: file };
}

/** Number of categorical pastel slots in tokens.css (--pastel-1 … --pastel-10). */
const MODULE_BAND_SLOTS = 10;

/**
 * Stable per-module accent color (#4557). Hashes the module key onto the
 * categorical pastel scale so every node of the same module shares a left
 * color-band, making related work read as a unit regardless of layout
 * direction / depth / fullscreen. The synthetic "external" bucket always reads
 * muted (no band hue).
 */
export function moduleBand(mod: NodeModule | undefined): { color: string } | null {
  if (!mod || mod.key === "__external__") return null;
  let h = 0;
  for (let i = 0; i < mod.key.length; i++) {
    h = (h * 31 + mod.key.charCodeAt(i)) | 0;
  }
  const slot = (Math.abs(h) % MODULE_BAND_SLOTS) + 1;
  return { color: `var(--pastel-${slot}-ink)` };
}

/* ------------------------------------------------------------------
   Edges
   ------------------------------------------------------------------ */

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
