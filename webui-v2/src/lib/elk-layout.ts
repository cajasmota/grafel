/* ============================================================
   lib/elk-layout.ts — shared elkjs layout helper for React Flow diagrams.

   Foundation of the elkjs adoption epic (#4824/#4825). The dashboard has four
   React-Flow diagrams (compound-topology, iac-diagram, flow-dag, flows) that are
   all NESTED-CONTAINMENT graphs (compound zones / IaC groups). dagre fakes
   compound layout via manual setParent + offset math; elkjs (the Eclipse Layout
   Kernel JS port) has first-class hierarchical layout (`hierarchyHandling:
   INCLUDE_CHILDREN`) and orthogonal edge routing — a far better fit.

   This module is intentionally GENERIC: it knows nothing about topology zones,
   IaC groups, tiers, etc. Consumers pass plain React-Flow nodes/edges (with the
   compound parent/child structure already expressed via `parentId`) plus a few
   layout options, and get back laid-out positions + container sizes. The other
   three diagrams (#4826/#4827/#4828) consume the SAME `layoutWithElk` /
   `useElkLayout` here — do not fork topology-specific logic in.

   Engine note: we import `elkjs/lib/elk.bundled.js` (the self-contained build)
   rather than wiring elk's Web Worker through vite. The bundled build runs
   identically in the browser and under node/vitest, layout() is already async
   (Promise-based) so it does not block paint for our graph sizes, and it bundles
   cleanly with zero vite worker config. The public API here is worker-agnostic,
   so a `new Worker(new URL('elkjs/lib/elk-worker.js'))` backend can be swapped in
   later (epic follow-up) without touching any consumer.
   ============================================================ */

import ELK, {
  type ElkNode,
  type ElkExtendedEdge,
  type LayoutOptions,
} from "elkjs/lib/elk.bundled.js";

/** A single ELK layout engine instance, shared across all callers. */
const elk = new ELK();

/** Minimal shape of the React-Flow nodes we lay out (subset of @xyflow Node). */
export interface ElkLayoutNode {
  id: string;
  /** Compound parent id (React Flow `parentId`). Empty/undefined ⇒ root. */
  parentId?: string;
  /** Measured/initial size. Containers may omit; ELK sizes them from children. */
  width?: number;
  height?: number;
  /**
   * Optional lane/order hint. When `laneOf` is supplied to the layout call,
   * nodes are biased into ordered lanes (e.g. tier columns) along the layout
   * direction. The value is an integer rank (lower ⇒ earlier in the flow).
   */
  lane?: number;
  /** True for container/group nodes (zones, IaC groups). Sized from children. */
  isContainer?: boolean;
}

/** Minimal shape of the React-Flow edges we lay out. */
export interface ElkLayoutEdge {
  id: string;
  source: string;
  target: string;
}

/** Layout result for one node: absolute-within-parent position + final size. */
export interface ElkLayoutPosition {
  id: string;
  /** Position relative to the node's parent (React Flow convention). */
  x: number;
  y: number;
  /** Final size. For containers this is ELK's computed bounding box. */
  width: number;
  height: number;
}

/** A single 2D point in React-Flow ABSOLUTE flow coordinates. */
export interface ElkPoint2D {
  x: number;
  y: number;
}

/**
 * ELK's computed orthogonal route for one edge, as a polyline of ABSOLUTE flow
 * coordinates: [startPoint, ...bendPoints, endPoint]. ELK stores edge section
 * points RELATIVE to the container the edge is attached to (its endpoints' LCA);
 * we translate them to absolute flow space by adding that container's absolute
 * offset, so consumers can build the SVG path directly (no further offsetting).
 *
 * The edge component should draw a polyline through these points (right-angle
 * H/V segments — ELK routes orthogonally) and fall back to getSmoothStepPath
 * when no route is present for an edge.
 */
export interface ElkLayoutEdgeRoute {
  id: string;
  /** ≥2 points: start, optional bends, end — absolute flow coordinates. */
  points: ElkPoint2D[];
}

/**
 * Full ELK layout result: node positions/sizes AND per-edge orthogonal routes.
 * `nodes` keeps the original parent-relative position contract; `edges` carries
 * ELK's routed bendPoints translated to absolute flow coords (#4843).
 */
export interface ElkLayoutResult {
  nodes: Map<string, ElkLayoutPosition>;
  edges: Map<string, ElkLayoutEdgeRoute>;
}

export type ElkDirection = "RIGHT" | "LEFT" | "DOWN" | "UP";

export interface ElkLayoutOptions {
  /** Flow direction. RIGHT = left→right architecture-diagram reading. */
  direction?: ElkDirection;
  /** ELK algorithm. "layered" is the hierarchical default. */
  algorithm?: string;
  /** Edge routing style. */
  edgeRouting?: "ORTHOGONAL" | "POLYLINE" | "SPLINES";
  /** Spacing between sibling nodes in the same layer. */
  nodeSpacing?: number;
  /** Spacing between layers (ranks). */
  layerSpacing?: number;
  /** Inner padding of container nodes: "top,right,bottom,left" (px each). */
  padding?: { top: number; right: number; bottom: number; left: number };
  /** Default size applied to leaf nodes that don't carry width/height. */
  defaultNodeWidth?: number;
  defaultNodeHeight?: number;
  /**
   * #4874: pin every node's edge endpoints to a SINGLE centered port on its
   * leading (outgoing) / trailing (incoming) face, chosen by `direction`:
   *   - RIGHT → source on the right-edge center, target on the left-edge center
   *   - DOWN  → source on the bottom-edge center, target on the top-edge center
   *   - LEFT/UP → mirror images of the above
   * Every node gets exactly one fixed source port and one fixed target port
   * (FIXED_POS), so ELK's routed bendPoints start/end at the same centered point
   * React Flow draws its <Handle> at — no diagonal stub from handle to route, and
   * a node's many outgoing edges all leave from the one centered trunk and split
   * via the orthogonal bus instead of fanning from offset side points.
   */
  centeredPorts?: boolean;
  /**
   * Optional tier-lane ordering hint. Given a node id, return its lane rank
   * (lower ⇒ earlier along `direction`). Nodes are pinned into ordered lanes so
   * e.g. client·edge·auth·compute·data·messaging·external read left→right. When
   * omitted, ELK orders nodes purely from edge structure.
   *
   * Lanes are applied per nesting level (only across siblings sharing a parent),
   * so a child zone's internal nodes still order independently.
   */
  laneOf?: (nodeId: string) => number | undefined;
  /** Extra raw ELK layoutOptions, merged last (escape hatch / per-diagram). */
  extraLayoutOptions?: LayoutOptions;
}

const DEFAULTS: Required<
  Omit<ElkLayoutOptions, "laneOf" | "extraLayoutOptions" | "padding" | "centeredPorts">
> & { padding: NonNullable<ElkLayoutOptions["padding"]> } = {
  direction: "RIGHT",
  algorithm: "layered",
  edgeRouting: "ORTHOGONAL",
  nodeSpacing: 28,
  layerSpacing: 72,
  padding: { top: 34, right: 16, bottom: 14, left: 16 },
  defaultNodeWidth: 180,
  defaultNodeHeight: 52,
};

const finite = (v: number | undefined, fallback = 0): number =>
  typeof v === "number" && Number.isFinite(v) ? v : fallback;

/**
 * orthogonalPath builds an SVG path string through an ELK-routed polyline,
 * rounding each corner by up to `radius` px for polish (the segments stay
 * orthogonal H/V; only the corners are filleted). Returns "" for <2 points so
 * the caller can fall back to getSmoothStepPath. Also returns a sensible
 * mid-point for label placement (the polyline's mid-length vertex).
 *
 * Consumers pass ELK's absolute-flow-coord points (ElkLayoutEdgeRoute.points)
 * straight in — they're already in the diagram's coordinate space (#4843).
 */
export function orthogonalPath(
  points: ElkPoint2D[],
  radius = 8,
): { path: string; labelX: number; labelY: number } | null {
  if (!points || points.length < 2) return null;
  // Drop consecutive duplicate points (ELK can emit zero-length segments).
  const pts: ElkPoint2D[] = [];
  for (const p of points) {
    const last = pts[pts.length - 1];
    if (!last || last.x !== p.x || last.y !== p.y) pts.push(p);
  }
  if (pts.length < 2) return null;

  let d = `M ${pts[0].x},${pts[0].y}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const prev = pts[i - 1];
    const cur = pts[i];
    const next = pts[i + 1];
    // Approach/leave lengths, capped so radius never overshoots a short segment.
    const inLen = Math.hypot(cur.x - prev.x, cur.y - prev.y);
    const outLen = Math.hypot(next.x - cur.x, next.y - cur.y);
    const r = Math.max(0, Math.min(radius, inLen / 2, outLen / 2));
    if (r < 0.5) {
      d += ` L ${cur.x},${cur.y}`;
      continue;
    }
    const ix = cur.x - (cur.x - prev.x) * (r / (inLen || 1));
    const iy = cur.y - (cur.y - prev.y) * (r / (inLen || 1));
    const ox = cur.x + (next.x - cur.x) * (r / (outLen || 1));
    const oy = cur.y + (next.y - cur.y) * (r / (outLen || 1));
    d += ` L ${ix},${iy} Q ${cur.x},${cur.y} ${ox},${oy}`;
  }
  const end = pts[pts.length - 1];
  d += ` L ${end.x},${end.y}`;

  // Label at the polyline's mid-length point.
  let total = 0;
  for (let i = 1; i < pts.length; i++) {
    total += Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y);
  }
  let acc = 0;
  let labelX = pts[0].x;
  let labelY = pts[0].y;
  for (let i = 1; i < pts.length; i++) {
    const seg = Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y);
    if (acc + seg >= total / 2) {
      const t = seg === 0 ? 0 : (total / 2 - acc) / seg;
      labelX = pts[i - 1].x + (pts[i].x - pts[i - 1].x) * t;
      labelY = pts[i - 1].y + (pts[i].y - pts[i - 1].y) * t;
      break;
    }
    acc += seg;
  }
  return { path: d, labelX, labelY };
}

/**
 * buildElkTree turns the flat React-Flow node list (with `parentId` links) into
 * the nested ElkNode tree ELK expects. Edges are attached at the lowest common
 * ancestor of their endpoints so cross-container edges route correctly.
 */
/**
 * Centered source/target port coordinates (relative to a node's top-left) for a
 * given flow direction (#4874). The SOURCE port sits on the node's LEADING face
 * (where the flow exits) and the TARGET port on its TRAILING face (where the flow
 * enters), each at the face's midpoint — matching React Flow's centered handles.
 */
function centeredPortGeom(
  direction: ElkDirection,
  width: number,
  height: number,
): { source: { x: number; y: number }; target: { x: number; y: number } } {
  switch (direction) {
    case "RIGHT":
      return {
        source: { x: width, y: height / 2 }, // right-edge center
        target: { x: 0, y: height / 2 }, // left-edge center
      };
    case "LEFT":
      return {
        source: { x: 0, y: height / 2 }, // left-edge center
        target: { x: width, y: height / 2 }, // right-edge center
      };
    case "UP":
      return {
        source: { x: width / 2, y: 0 }, // top-edge center
        target: { x: width / 2, y: height }, // bottom-edge center
      };
    case "DOWN":
    default:
      return {
        source: { x: width / 2, y: height }, // bottom-edge center
        target: { x: width / 2, y: 0 }, // top-edge center
      };
  }
}

/**
 * ELK port `side` for the source/target port under a given flow direction. The
 * source leaves on the leading face, the target enters on the trailing face.
 */
function portSideFor(direction: ElkDirection, role: "source" | "target"): string {
  const leading: Record<ElkDirection, string> = {
    RIGHT: "EAST",
    LEFT: "WEST",
    DOWN: "SOUTH",
    UP: "NORTH",
  };
  const trailing: Record<ElkDirection, string> = {
    RIGHT: "WEST",
    LEFT: "EAST",
    DOWN: "NORTH",
    UP: "SOUTH",
  };
  return role === "source" ? leading[direction] : trailing[direction];
}

function buildElkTree(
  nodes: ElkLayoutNode[],
  edges: ElkLayoutEdge[],
  opts: Required<Pick<ElkLayoutOptions, "defaultNodeWidth" | "defaultNodeHeight">> & {
    laneOf?: ElkLayoutOptions["laneOf"];
    centeredPorts?: boolean;
    direction: ElkDirection;
  },
): ElkNode {
  const byId = new Map<string, ElkLayoutNode>();
  for (const n of nodes) byId.set(n.id, n);

  // Build empty ElkNode shells, indexed by id.
  const elkById = new Map<string, ElkNode>();
  for (const n of nodes) {
    const isContainer = n.isContainer ?? false;
    const width = finite(n.width, opts.defaultNodeWidth);
    const height = finite(n.height, opts.defaultNodeHeight);
    const shell: ElkNode = {
      id: n.id,
      children: [],
      edges: [],
      ...(isContainer ? {} : { width, height }),
    };
    // Per-node lane hint → ELK position constraint within its layer.
    const lane = opts.laneOf?.(n.id) ?? n.lane;
    if (typeof lane === "number" && Number.isFinite(lane)) {
      shell.layoutOptions = {
        ...shell.layoutOptions,
        // Pins the node's layer (rank) so lanes stay ordered along direction.
        "elk.layered.layering.layerChoiceConstraint": String(lane),
      };
    }
    // #4874: pin a single centered source + target port per leaf node so ELK's
    // routed endpoints land exactly where React Flow draws the centered handle.
    if (opts.centeredPorts && !isContainer) {
      const g = centeredPortGeom(opts.direction, width, height);
      shell.layoutOptions = {
        ...shell.layoutOptions,
        // FIXED_POS honours the explicit port (x,y) we set below verbatim.
        "elk.portConstraints": "FIXED_POS",
      };
      shell.ports = [
        {
          id: `${n.id}__src`,
          x: g.source.x,
          y: g.source.y,
          width: 0,
          height: 0,
          // Direction the port faces, so the router leaves/arrives cleanly.
          layoutOptions: { "elk.port.side": portSideFor(opts.direction, "source") },
        },
        {
          id: `${n.id}__tgt`,
          x: g.target.x,
          y: g.target.y,
          width: 0,
          height: 0,
          layoutOptions: { "elk.port.side": portSideFor(opts.direction, "target") },
        },
      ] as ElkNode["ports"];
    }
    elkById.set(n.id, shell);
  }

  const root: ElkNode = { id: "__elk_root__", children: [], edges: [] };

  // Attach each node under its parent (or root).
  for (const n of nodes) {
    const shell = elkById.get(n.id)!;
    const parent = n.parentId ? elkById.get(n.parentId) : undefined;
    (parent?.children ?? root.children!).push(shell);
  }

  // Depth lookup for lowest-common-ancestor edge attachment.
  const parentOf = (id: string): string | undefined => byId.get(id)?.parentId;
  const ancestry = (id: string): string[] => {
    const chain: string[] = [];
    let cur: string | undefined = id;
    const seen = new Set<string>();
    while (cur && !seen.has(cur)) {
      seen.add(cur);
      chain.push(cur);
      cur = parentOf(cur);
    }
    return chain;
  };
  const lca = (a: string, b: string): string | undefined => {
    const aChain = new Set(ancestry(a));
    for (const id of ancestry(b)) if (aChain.has(id)) return id;
    return undefined;
  };

  // #4874: when ports are pinned, anchor edges to the centered source/target
  // PORT id (not the node id) so ELK routes from/to the exact centered point.
  const srcRef = (id: string): string =>
    opts.centeredPorts && !(byId.get(id)?.isContainer ?? false) ? `${id}__src` : id;
  const tgtRef = (id: string): string =>
    opts.centeredPorts && !(byId.get(id)?.isContainer ?? false) ? `${id}__tgt` : id;

  for (const e of edges) {
    if (!byId.has(e.source) || !byId.has(e.target)) continue;
    const common = lca(e.source, e.target);
    // Attach the edge at the LCA container, or root when they share none / are
    // in different subtrees (cross-container edge).
    let host: ElkNode = root;
    if (common && elkById.has(common) && common !== e.source && common !== e.target) {
      host = elkById.get(common)!;
    } else {
      // Walk up from the source to the first common container, else root.
      const target: ElkExtendedEdge = {
        id: e.id,
        sources: [srcRef(e.source)],
        targets: [tgtRef(e.target)],
      };
      root.edges!.push(target);
      continue;
    }
    host.edges!.push({ id: e.id, sources: [srcRef(e.source)], targets: [tgtRef(e.target)] });
  }

  return root;
}

function graphLayoutOptions(o: typeof DEFAULTS, extra?: LayoutOptions): LayoutOptions {
  return {
    "elk.algorithm": o.algorithm,
    "elk.direction": o.direction,
    "elk.hierarchyHandling": "INCLUDE_CHILDREN",
    "elk.edgeRouting": o.edgeRouting,
    "elk.layered.spacing.nodeNodeBetweenLayers": String(o.layerSpacing),
    "elk.spacing.nodeNode": String(o.nodeSpacing),
    // Keep clean lanes: edges leave/enter on consistent sides and reserve
    // gutter so orthogonal segments route in channels rather than overlapping
    // nodes (#4843). Ports are fixed to the layout direction sides.
    "elk.layered.spacing.edgeNodeBetweenLayers": String(Math.round(o.nodeSpacing * 0.6)),
    "elk.spacing.edgeNode": String(Math.round(o.nodeSpacing * 0.6)),
    "elk.padding": `[top=${o.padding.top},left=${o.padding.left},bottom=${o.padding.bottom},right=${o.padding.right}]`,
    "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
    ...extra,
  };
}

/**
 * collectLayout flattens ELK's laid-out tree back into per-node positions AND
 * per-edge orthogonal routes.
 *
 * - Node positions: ELK positions children relative to their parent already —
 *   exactly the React-Flow `parentId` convention — so x/y are returned as-is.
 * - Edge routes: ELK stores each edge under its endpoints' lowest-common-ancestor
 *   container, with section points RELATIVE to that container's content box.
 *   We walk the tree carrying each container's ABSOLUTE offset (sum of its and
 *   its ancestors' relative positions) and translate every edge point into
 *   absolute flow space, so consumers can draw the polyline directly.
 */
function collectLayout(
  root: ElkNode,
  outNodes: Map<string, ElkLayoutPosition>,
  outEdges: Map<string, ElkLayoutEdgeRoute>,
): void {
  // Visit carries the ABSOLUTE offset of `node`'s content origin. The root's
  // own x/y are layout origin (0,0); children are relative to that origin.
  const visit = (node: ElkNode, absX: number, absY: number) => {
    // Edges declared on THIS container route in this container's coordinate
    // space → translate each point by the container's absolute offset.
    for (const edge of node.edges ?? []) {
      const points: ElkPoint2D[] = [];
      for (const sec of edge.sections ?? []) {
        points.push({ x: absX + finite(sec.startPoint.x), y: absY + finite(sec.startPoint.y) });
        for (const bp of sec.bendPoints ?? []) {
          points.push({ x: absX + finite(bp.x), y: absY + finite(bp.y) });
        }
        points.push({ x: absX + finite(sec.endPoint.x), y: absY + finite(sec.endPoint.y) });
      }
      if (edge.id && points.length >= 2) {
        outEdges.set(edge.id, { id: edge.id, points });
      }
    }
    for (const child of node.children ?? []) {
      outNodes.set(child.id, {
        id: child.id,
        x: finite(child.x),
        y: finite(child.y),
        width: finite(child.width, 0),
        height: finite(child.height, 0),
      });
      // A child container's content origin is its own absolute top-left.
      visit(child, absX + finite(child.x), absY + finite(child.y));
    }
  };
  visit(root, 0, 0);
}

/**
 * layoutWithElk runs ELK over a compound React-Flow graph and returns a map of
 * node id → laid-out position/size. Positions are PARENT-RELATIVE (React Flow's
 * convention for child nodes), and container sizes are ELK's computed bounding
 * boxes. Consumers map these back onto their own node objects.
 *
 * Async: ELK layout is Promise-based. Call from an effect and store the result.
 *
 * Returns BOTH node positions (`.nodes`) and ELK's orthogonal edge routes
 * (`.edges`, bendPoints in absolute flow coords) — see ElkLayoutResult (#4843).
 *
 * @example
 *   const { nodes: pos, edges: routes } = await layoutWithElk(nodes, edges, {
 *     direction: "RIGHT",
 *     laneOf: (id) => tierRankById.get(id),
 *   });
 *   const placed = nodes.map(n => ({ ...n, position: { x: pos.get(n.id)!.x, y: pos.get(n.id)!.y } }));
 *   const route = routes.get(edgeId)?.points; // [{x,y}, …] orthogonal polyline
 */
export async function layoutWithElk(
  nodes: ElkLayoutNode[],
  edges: ElkLayoutEdge[],
  options: ElkLayoutOptions = {},
): Promise<ElkLayoutResult> {
  const outNodes = new Map<string, ElkLayoutPosition>();
  const outEdges = new Map<string, ElkLayoutEdgeRoute>();
  if (nodes.length === 0) return { nodes: outNodes, edges: outEdges };

  const o = {
    ...DEFAULTS,
    ...options,
    padding: { ...DEFAULTS.padding, ...(options.padding ?? {}) },
  } as typeof DEFAULTS;

  const root = buildElkTree(nodes, edges, {
    defaultNodeWidth: o.defaultNodeWidth,
    defaultNodeHeight: o.defaultNodeHeight,
    laneOf: options.laneOf,
    centeredPorts: options.centeredPorts,
    direction: o.direction,
  });
  root.layoutOptions = graphLayoutOptions(o, options.extraLayoutOptions);

  const laid = await elk.layout(root);
  collectLayout(laid, outNodes, outEdges);
  return { nodes: outNodes, edges: outEdges };
}
