/* ============================================================
   components/compound-topology/layout.ts — compound architecture-diagram layout.

   Model 1 of the compound-topology epic (#4810/#4811). Turns the compound
   topology payload (zones + tiered nodes + typed edges, topology_compound.go)
   into a positioned React Flow graph:

     - zones  = nested containment boxes (React Flow parent/child sub-flows).
                A COLLAPSED zone renders as ONE leaf box and its members'
                cross-zone edges fold into summary edges.
     - nodes  = entities, placed by their `tier` lane (client→…→external) so the
                canvas reads left→right like an architecture diagram.
     - edges  = typed relationships (reads/writes/invokes/consumes/routes/
                depends); when an endpoint is inside a collapsed zone the edge
                is re-targeted at the collapsed-zone box and aggregated.

   Layout engine: as of the elkjs epic (#4824/#4825) the default backend is
   ELK (elk.hierarchyHandling INCLUDE_CHILDREN + orthogonal routing) via the
   shared `layoutWithElk` helper (lib/elk-layout.ts) — a true compound layout.
   The legacy dagre compound pass (setParent + tier-rank hints + manual zone
   bounding boxes) is KEPT as a synchronous fallback behind a flag so we can
   revert visually if needed.

   The "planning" stage (which nodes/zones render, edge folding into summary
   edges) is engine-agnostic and shared by both backends; only the positioning
   differs.

   Performance: hard node ceiling (MAX_NODES) + the graph-render crash-guard
   lessons (#4618/#4658) — we never emit a child whose parent box wasn't laid
   out, and positions are always finite numbers.
   ============================================================ */

import dagre from "dagre";
import { Position, type Node, type Edge } from "@xyflow/react";
import type {
  CompoundTopologyResponse,
  CompoundNode,
  CompoundTier,
  CompoundEdgeType,
} from "@/data/types";
import {
  layoutWithElk,
  type ElkLayoutNode,
  type ElkLayoutEdge,
  type ElkPoint2D,
} from "@/lib/elk-layout";

export const CT_NODE_TYPE = "ctNode";
export const CT_ZONE_TYPE = "ctZone";
export const CT_EDGE_TYPE = "ctEdge";

const NODE_W = 188;
const NODE_H = 52;
const ZONE_PAD_X = 16;
const ZONE_PAD_TOP = 34;
const ZONE_PAD_BOTTOM = 14;

/** Hard ceiling so a huge group can't hang the browser (#4618/#4658). */
export const MAX_NODES = 600;

/**
 * Layout engine selection. Defaults to ELK (#4825); set the env flag
 * `VITE_CT_LAYOUT_ENGINE=dagre` (or pass engine="dagre") to use the legacy
 * dagre fallback for a visual revert.
 */
export type CTLayoutEngine = "elk" | "dagre";
export function defaultLayoutEngine(): CTLayoutEngine {
  const env =
    typeof import.meta !== "undefined"
      ? (import.meta as { env?: Record<string, string | undefined> }).env
      : undefined;
  return env?.VITE_CT_LAYOUT_ENGINE === "dagre" ? "dagre" : "elk";
}

/** Canonical tier lane order (mirrors the backend canonicalTiers). */
export const TIER_ORDER: CompoundTier[] = [
  "client",
  "edge",
  "auth",
  "compute",
  "data",
  "messaging",
  "external",
];

export interface CTNodeData {
  label: string;
  kind: string;
  tier: CompoundTier;
  repo: string;
  [key: string]: unknown;
}

export interface CTZoneData {
  label: string;
  kind: string;
  zoneId: string;
  /** True when this zone is rendered collapsed (one box, internals hidden). */
  collapsed: boolean;
  /** Member node count (direct + transitive) — shown when collapsed. */
  nodeCount: number;
  /**
   * Container nesting depth (0 = outermost). Drives a stronger tint/border at
   * outer levels so a nested zone box stays legible inside its parent (#4866).
   */
  depth: number;
  /** Toggles collapse for this zone id. */
  onToggle: (zoneId: string) => void;
  [key: string]: unknown;
}

export interface CTEdgeData {
  type: CompoundEdgeType;
  label: string;
  /** Number of underlying edges folded into this one (>=1). */
  count: number;
  /** True when this is a zone-level summary edge (a collapse aggregate). */
  summary: boolean;
  /**
   * ELK's orthogonal route (absolute flow coords). When present the edge draws
   * an H/V polyline through these points instead of a bezier (#4843). Absent
   * under the dagre fallback.
   */
  elkPoints?: ElkPoint2D[];
  [key: string]: unknown;
}

export interface CTLayoutResult {
  nodes: Node[];
  edges: Edge[];
  capped: boolean;
  /** Number of summary (aggregated) edges emitted for collapsed zones. */
  summaryEdgeCount: number;
}

/**
 * collapsedAncestor returns the id of the OUTERMOST collapsed zone on a node's
 * zone_path, or "" when none of its ancestors are collapsed. When a node is
 * inside a collapsed zone it is not rendered; its edges re-target that zone.
 */
function collapsedAncestor(
  zonePath: string[],
  collapsed: Set<string>,
): string {
  for (const zid of zonePath) {
    if (collapsed.has(zid)) return zid;
  }
  return "";
}

/**
 * effectiveEndpoint maps a node id to the id it is rendered as: itself when
 * visible, otherwise the outermost collapsed zone box that swallows it.
 */
function effectiveEndpoint(
  nodeId: string,
  nodeIndex: Map<string, CompoundNode>,
  collapsed: Set<string>,
): string {
  const n = nodeIndex.get(nodeId);
  if (!n) return nodeId; // endpoint not on canvas; caller drops it.
  const anc = collapsedAncestor(n.zone_path, collapsed);
  return anc || nodeId;
}

/** tierRank gives a node a stable column index from its tier. */
function tierRank(tier: CompoundTier): number {
  const i = TIER_ORDER.indexOf(tier);
  return i < 0 ? TIER_ORDER.length : i;
}

interface FoldedEdge {
  from: string;
  to: string;
  type: CompoundEdgeType;
  count: number;
  summary: boolean;
}

/**
 * CTPlan is the engine-agnostic intermediate produced from the payload + the
 * collapse set: which leaf nodes and zone boxes render, their parent links, the
 * folded (summary-aware) edges, and the helpers both backends need. The dagre
 * and ELK positioners consume this identically.
 */
interface CTPlan {
  capped: boolean;
  visibleNodes: CompoundNode[];
  renderedZoneIds: Set<string>;
  folded: Map<string, FoldedEdge>;
  /** Innermost VISIBLE zone id for a node ("" ⇒ root). */
  innermostVisibleZone: (n: CompoundNode) => string;
  /** Nearest rendered ancestor zone id for a zone ("" ⇒ root). */
  zoneParentOf: (zoneId: string) => string;
  isCollapsedRendered: (zoneId: string) => boolean;
  depthOf: (zoneId: string) => number;
  zoneOf: (zoneId: string) =>
    | { id: string; label: string; kind: string; node_count: number; parent_id?: string }
    | undefined;
  /** True when an id corresponds to a rendered leaf node or zone box. */
  layoutable: (id: string) => boolean;
}

/**
 * planCompoundTopology computes the engine-agnostic render plan: visible nodes,
 * rendered zones, parent relationships, and folded summary edges. This is the
 * topology-specific logic shared by both the ELK and dagre backends.
 */
function planCompoundTopology(
  report: CompoundTopologyResponse,
  collapsed: Set<string>,
): CTPlan {
  const capped = report.nodes.length > MAX_NODES;
  const allNodes = capped ? report.nodes.slice(0, MAX_NODES) : report.nodes;

  const nodeIndex = new Map<string, CompoundNode>();
  for (const n of allNodes) nodeIndex.set(n.id, n);

  const zoneIndex = new Map(report.zones.map((z) => [z.id, z]));

  const isCollapsedRendered = (zoneId: string): boolean => {
    if (!collapsed.has(zoneId)) return false;
    let z = zoneIndex.get(zoneId);
    while (z && z.parent_id) {
      if (collapsed.has(z.parent_id)) return false;
      z = zoneIndex.get(z.parent_id);
    }
    return true;
  };

  const visibleNodes = allNodes.filter(
    (n) => collapsedAncestor(n.zone_path, collapsed) === "",
  );

  const innermostVisibleZone = (n: CompoundNode): string => {
    let parent = "";
    for (const zid of n.zone_path) {
      if (collapsed.has(zid)) break;
      parent = zid;
    }
    return parent;
  };

  const renderedZoneIds = new Set<string>();
  for (const z of report.zones) {
    if (isCollapsedRendered(z.id)) {
      renderedZoneIds.add(z.id);
      let p = z.parent_id;
      while (p) {
        if (!collapsed.has(p)) renderedZoneIds.add(p);
        const zp = zoneIndex.get(p);
        p = zp?.parent_id;
      }
    }
  }
  for (const n of visibleNodes) {
    for (const zid of n.zone_path) {
      if (collapsed.has(zid)) break;
      renderedZoneIds.add(zid);
    }
  }

  const zoneParentOf = (zoneId: string): string => {
    const z = zoneIndex.get(zoneId);
    let p = z?.parent_id || "";
    while (p && !renderedZoneIds.has(p)) {
      p = zoneIndex.get(p)?.parent_id || "";
    }
    return p;
  };

  // ── Edge folding (summary-aware). ─────────────────────────────────────────
  const folded = new Map<string, FoldedEdge>();
  for (const e of report.edges) {
    const src = effectiveEndpoint(e.source, nodeIndex, collapsed);
    const tgt = effectiveEndpoint(e.target, nodeIndex, collapsed);
    if (!nodeIndex.has(e.source) || !nodeIndex.has(e.target)) continue;
    if (src === tgt) continue;
    const summary = src !== e.source || tgt !== e.target;
    const key = `${src} ${tgt} ${e.type}`;
    const prev = folded.get(key);
    if (prev) {
      prev.count += 1;
      prev.summary = prev.summary || summary;
    } else {
      folded.set(key, { from: src, to: tgt, type: e.type, count: 1, summary });
    }
  }

  const layoutable = (id: string) =>
    nodeIndex.has(id) || renderedZoneIds.has(id);

  const depthOf = (zid: string): number => {
    let d = 0;
    let p = zoneIndex.get(zid)?.parent_id;
    while (p) {
      if (renderedZoneIds.has(p)) d++;
      p = zoneIndex.get(p)?.parent_id;
    }
    return d;
  };

  return {
    capped,
    visibleNodes,
    renderedZoneIds,
    folded,
    innermostVisibleZone,
    zoneParentOf,
    isCollapsedRendered,
    depthOf,
    zoneOf: (zid) => zoneIndex.get(zid),
    layoutable,
  };
}

/**
 * materialize turns a CTPlan + absolute boxes for every laid-out leaf/collapsed
 * zone into the final React-Flow node/edge lists (parent-relative positions,
 * derived expanded-container boxes, summary edges). Engine-agnostic: both dagre
 * and ELK feed it their absolute boxes.
 */
function materialize(
  plan: CTPlan,
  abs: Map<string, { x: number; y: number; w: number; h: number }>,
  onToggle: (zoneId: string) => void,
  /** Optional ELK routes keyed by folded-edge key "from to type" (abs coords). */
  edgeRoutes?: Map<string, ElkPoint2D[]>,
): CTLayoutResult {
  const {
    visibleNodes,
    renderedZoneIds,
    folded,
    innermostVisibleZone,
    zoneParentOf,
    isCollapsedRendered,
    depthOf,
    zoneOf,
    layoutable,
  } = plan;

  const out: Node[] = [];

  // Recompute EXPANDED container boxes from member bounding boxes when an engine
  // doesn't size containers itself (dagre). ELK sizes containers natively, but
  // recomputing from members is harmless and keeps padded headers consistent.
  const memberBox = new Map<
    string,
    { minX: number; minY: number; maxX: number; maxY: number }
  >();
  const noteMember = (
    zoneId: string,
    a: { x: number; y: number; w: number; h: number },
  ) => {
    let p: string | undefined = zoneId;
    while (p && renderedZoneIds.has(p)) {
      const b =
        memberBox.get(p) ??
        { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
      b.minX = Math.min(b.minX, a.x);
      b.minY = Math.min(b.minY, a.y);
      b.maxX = Math.max(b.maxX, a.x + a.w);
      b.maxY = Math.max(b.maxY, a.y + a.h);
      memberBox.set(p, b);
      p = zoneOf(p)?.parent_id;
      while (p && !renderedZoneIds.has(p)) p = zoneOf(p)?.parent_id;
    }
  };
  for (const n of visibleNodes) {
    const a = abs.get(n.id);
    const parent = innermostVisibleZone(n);
    if (a && parent && renderedZoneIds.has(parent)) noteMember(parent, a);
  }
  for (const zid of renderedZoneIds) {
    if (!isCollapsedRendered(zid)) continue;
    const a = abs.get(zid);
    const parent = zoneParentOf(zid);
    if (a && parent) noteMember(parent, a);
  }

  // Order zones outermost→innermost so React Flow parents exist before children.
  const zoneOrder = [...renderedZoneIds].sort((a, b) => depthOf(a) - depthOf(b));

  const zoneAbs = new Map<string, { x: number; y: number }>();
  for (const zid of zoneOrder) {
    const z = zoneOf(zid)!;
    const collapsedRendered = isCollapsedRendered(zid);
    let x: number, y: number, w: number, h: number;
    if (collapsedRendered) {
      const a = abs.get(zid)!;
      x = a.x;
      y = a.y;
      w = a.w;
      h = a.h;
    } else {
      const b = memberBox.get(zid);
      if (!b || !Number.isFinite(b.minX)) continue; // empty container → skip.
      x = b.minX - ZONE_PAD_X;
      y = b.minY - ZONE_PAD_TOP;
      w = b.maxX - b.minX + ZONE_PAD_X * 2;
      h = b.maxY - b.minY + ZONE_PAD_TOP + ZONE_PAD_BOTTOM;
    }
    zoneAbs.set(zid, { x, y });

    const parent = zoneParentOf(zid);
    const pAbs = parent ? zoneAbs.get(parent) : undefined;
    const pos = pAbs ? { x: x - pAbs.x, y: y - pAbs.y } : { x, y };

    const data: CTZoneData = {
      label: z.label,
      kind: z.kind,
      zoneId: zid,
      collapsed: collapsedRendered,
      nodeCount: z.node_count,
      depth: depthOf(zid),
      onToggle,
    };
    out.push({
      id: zid,
      type: CT_ZONE_TYPE,
      position: pos,
      ...(parent && renderedZoneIds.has(parent) ? { parentId: parent } : {}),
      data,
      width: w,
      height: h,
      draggable: false,
      selectable: false,
      zIndex: depthOf(zid),
    });
  }

  // Emit visible leaf nodes (relative to their innermost visible zone).
  for (const n of visibleNodes) {
    const a = abs.get(n.id);
    if (!a) continue;
    const parent = innermostVisibleZone(n);
    const pAbs = parent ? zoneAbs.get(parent) : undefined;
    const pos = pAbs ? { x: a.x - pAbs.x, y: a.y - pAbs.y } : { x: a.x, y: a.y };
    const data: CTNodeData = {
      label: n.label,
      kind: n.kind,
      tier: n.tier,
      repo: n.repo,
    };
    out.push({
      id: n.id,
      type: CT_NODE_TYPE,
      position: pos,
      ...(parent && renderedZoneIds.has(parent)
        ? { parentId: parent, extent: "parent" as const }
        : {}),
      data,
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
      width: NODE_W,
      height: NODE_H,
      zIndex: 50,
    });
  }

  // ── Edges. ────────────────────────────────────────────────────────────────
  let summaryEdgeCount = 0;
  const edges: Edge[] = [];
  let i = 0;
  for (const fe of folded.values()) {
    if (!layoutable(fe.from) || !layoutable(fe.to)) continue;
    if (fe.summary) summaryEdgeCount++;
    const data: CTEdgeData = {
      type: fe.type,
      label: fe.count > 1 ? `${fe.type} ×${fe.count}` : fe.type,
      count: fe.count,
      summary: fe.summary,
      elkPoints: edgeRoutes?.get(`${fe.from} ${fe.to} ${fe.type}`),
    };
    edges.push({
      id: `cte:${fe.from}->${fe.to}:${fe.type}:${i++}`,
      source: fe.from,
      target: fe.to,
      type: CT_EDGE_TYPE,
      data,
      zIndex: 40,
    });
  }

  return { nodes: out, edges, capped: plan.capped, summaryEdgeCount };
}

/* ============================================================
   ELK backend (default, async) — #4825.
   ============================================================ */

/**
 * layoutCompoundTopologyElk builds a positioned React Flow graph using the
 * shared ELK helper (compound INCLUDE_CHILDREN layout + orthogonal routing).
 * Async because ELK layout is Promise-based.
 */
export async function layoutCompoundTopologyElk(
  report: CompoundTopologyResponse | undefined,
  collapsed: Set<string>,
  onToggle: (zoneId: string) => void,
): Promise<CTLayoutResult> {
  if (!report || report.nodes.length === 0) {
    return { nodes: [], edges: [], capped: false, summaryEdgeCount: 0 };
  }

  const plan = planCompoundTopology(report, collapsed);
  const { visibleNodes, renderedZoneIds, folded, innermostVisibleZone, zoneParentOf, isCollapsedRendered } =
    plan;

  // ── Build ELK inputs from the plan. ──────────────────────────────────────
  const elkNodes: ElkLayoutNode[] = [];

  // Zone boxes: expanded zones are containers (ELK sizes from children),
  // collapsed-rendered zones are sized leaves.
  for (const zid of renderedZoneIds) {
    const collapsedRendered = isCollapsedRendered(zid);
    const parent = zoneParentOf(zid);
    elkNodes.push({
      id: zid,
      parentId: parent && renderedZoneIds.has(parent) ? parent : undefined,
      isContainer: !collapsedRendered,
      ...(collapsedRendered ? { width: NODE_W + 24, height: NODE_H + 8 } : {}),
    });
  }

  // Visible leaf nodes, with a tier lane hint for left→right ordering.
  for (const n of visibleNodes) {
    const parent = innermostVisibleZone(n);
    elkNodes.push({
      id: n.id,
      parentId: parent && renderedZoneIds.has(parent) ? parent : undefined,
      width: NODE_W,
      height: NODE_H,
      lane: tierRank(n.tier),
    });
  }

  // Folded edges between laid-out endpoints only. Track each elk edge id → its
  // folded key so we can re-attach ELK's route to the right React Flow edge.
  const elkEdges: ElkLayoutEdge[] = [];
  const elkEdgeKeyById = new Map<string, string>();
  let ei = 0;
  for (const fe of folded.values()) {
    if (plan.layoutable(fe.from) && plan.layoutable(fe.to)) {
      const id = `elk-e:${ei++}`;
      elkEdges.push({ id, source: fe.from, target: fe.to });
      elkEdgeKeyById.set(id, `${fe.from} ${fe.to} ${fe.type}`);
    }
  }

  const { nodes: positions, edges: routes } = await layoutWithElk(elkNodes, elkEdges, {
    direction: "RIGHT",
    edgeRouting: "ORTHOGONAL",
    nodeSpacing: 22,
    layerSpacing: 70,
    padding: { top: ZONE_PAD_TOP, right: ZONE_PAD_X, bottom: ZONE_PAD_BOTTOM, left: ZONE_PAD_X },
    defaultNodeWidth: NODE_W,
    defaultNodeHeight: NODE_H,
    // #4887: pin one centered source/target port per LEAF node on its leading/
    // trailing face for the layout direction (this view is horizontal → RIGHT,
    // so source = right-edge center, target = left-edge center), matching the
    // leaf nodes' Position.Right/Left React-Flow handles — edges leave/enter at
    // the centered face instead of side-escaping. Zone containers (expanded
    // zones) are NOT pinned: the helper only centers non-container leaves, so
    // cross-zone edges still exit/enter their leaf endpoints at the centered
    // face while ELK routes the segment through the zone boxes. Collapsed-
    // rendered zones are sized leaves and get centered ports too, which is
    // correct (they behave as nodes). #4862 nesting / #4866 styling untouched.
    centeredPorts: true,
  });

  // ELK positions are parent-relative; convert to ABSOLUTE for materialize()
  // (which re-derives parent-relative positions + container boxes uniformly).
  const finite = (v: number | undefined) =>
    Number.isFinite(v) ? (v as number) : 0;
  const absById = new Map<string, { x: number; y: number; w: number; h: number }>();

  // Absolute position of a node = sum of its and all ancestors' relative pos.
  const parentChain = (id: string): string[] => {
    const chain: string[] = [];
    const node = elkNodes.find((e) => e.id === id);
    let p = node?.parentId;
    const seen = new Set<string>();
    while (p && !seen.has(p)) {
      seen.add(p);
      chain.push(p);
      p = elkNodes.find((e) => e.id === p)?.parentId;
    }
    return chain;
  };
  const absPos = (id: string): { x: number; y: number } => {
    const self = positions.get(id);
    let x = finite(self?.x);
    let y = finite(self?.y);
    for (const anc of parentChain(id)) {
      const ap = positions.get(anc);
      x += finite(ap?.x);
      y += finite(ap?.y);
    }
    return { x, y };
  };

  for (const n of visibleNodes) {
    const p = absPos(n.id);
    absById.set(n.id, { x: p.x, y: p.y, w: NODE_W, h: NODE_H });
  }
  for (const zid of renderedZoneIds) {
    if (!isCollapsedRendered(zid)) continue;
    const p = absPos(zid);
    const pos = positions.get(zid);
    absById.set(zid, {
      x: p.x,
      y: p.y,
      w: finite(pos?.width) || NODE_W + 24,
      h: finite(pos?.height) || NODE_H + 8,
    });
  }

  // Re-key ELK routes by folded-edge key for materialize. ELK already returns
  // points in absolute flow coords (cross-zone edges live on the LCA container
  // and lib/elk-layout adds the container's absolute offset), so they line up
  // with the absolute node boxes materialize positions against.
  const edgeRoutes = new Map<string, ElkPoint2D[]>();
  for (const [elkId, key] of elkEdgeKeyById) {
    const route = routes.get(elkId);
    if (route && route.points.length >= 2) edgeRoutes.set(key, route.points);
  }

  return materialize(plan, absById, onToggle, edgeRoutes);
}

/* ============================================================
   dagre backend (legacy fallback, synchronous) — kept for visual revert.
   ============================================================ */

/**
 * layoutCompoundTopology builds a positioned React Flow graph using the legacy
 * dagre compound pass. Kept as a synchronous fallback (see defaultLayoutEngine).
 *
 * @param report     the compound payload (already fetched for a group_by).
 * @param collapsed  set of collapsed zone ids.
 */
export function layoutCompoundTopology(
  report: CompoundTopologyResponse | undefined,
  collapsed: Set<string>,
  onToggle: (zoneId: string) => void,
): CTLayoutResult {
  if (!report || report.nodes.length === 0) {
    return { nodes: [], edges: [], capped: false, summaryEdgeCount: 0 };
  }

  const plan = planCompoundTopology(report, collapsed);
  const { visibleNodes, renderedZoneIds, folded, innermostVisibleZone, zoneParentOf, isCollapsedRendered } =
    plan;

  // ── Dagre compound layout. ────────────────────────────────────────────────
  const g = new dagre.graphlib.Graph({ compound: true });
  g.setGraph({
    rankdir: "LR",
    nodesep: 22,
    ranksep: 70,
    marginx: 24,
    marginy: 24,
    ranker: "network-simplex",
  });
  g.setDefaultEdgeLabel(() => ({}));

  // Zone container/leaf nodes.
  for (const zid of renderedZoneIds) {
    if (isCollapsedRendered(zid)) {
      g.setNode(zid, { width: NODE_W + 24, height: NODE_H + 8 });
    } else {
      g.setNode(zid, {});
    }
    const parent = zoneParentOf(zid);
    if (parent) g.setParent(zid, parent);
  }

  // Visible leaf nodes.
  for (const n of visibleNodes) {
    g.setNode(n.id, { width: NODE_W, height: NODE_H });
    const parent = innermostVisibleZone(n);
    if (parent && renderedZoneIds.has(parent)) g.setParent(n.id, parent);
  }

  // Lay edges into dagre only when BOTH endpoints are laid-out nodes/zones.
  const layoutable = (id: string) => g.hasNode(id) && plan.layoutable(id);
  for (const fe of folded.values()) {
    if (layoutable(fe.from) && layoutable(fe.to)) g.setEdge(fe.from, fe.to);
  }

  applyTierHints(g, visibleNodes);

  dagre.layout(g);

  // ── Absolute boxes for everything dagre placed. ───────────────────────────
  const finite = (v: number | undefined) =>
    Number.isFinite(v) ? (v as number) : 0;
  const abs = new Map<string, { x: number; y: number; w: number; h: number }>();
  for (const zid of renderedZoneIds) {
    const dn = g.node(zid);
    if (!dn) continue;
    abs.set(zid, {
      x: finite(dn.x) - finite(dn.width) / 2,
      y: finite(dn.y) - finite(dn.height) / 2,
      w: finite(dn.width),
      h: finite(dn.height),
    });
  }
  for (const n of visibleNodes) {
    const dn = g.node(n.id);
    if (!dn) continue;
    abs.set(n.id, {
      x: finite(dn.x) - NODE_W / 2,
      y: finite(dn.y) - NODE_H / 2,
      w: NODE_W,
      h: NODE_H,
    });
  }

  return materialize(plan, abs, onToggle);
}

/**
 * applyTierHints adds zero-rendered ordering edges so dagre keeps the tier
 * lanes in canonical left→right order. We pick one representative node per tier
 * (the first seen) and chain reps of consecutive tiers. These edges are dropped
 * from the rendered output — they only bias the layout.
 */
function applyTierHints(
  g: dagre.graphlib.Graph,
  visibleNodes: CompoundNode[],
): void {
  const repByTier = new Map<number, string>();
  for (const n of visibleNodes) {
    const r = tierRank(n.tier);
    if (!repByTier.has(r)) repByTier.set(r, n.id);
  }
  const ranks = [...repByTier.keys()].sort((a, b) => a - b);
  for (let k = 1; k < ranks.length; k++) {
    const from = repByTier.get(ranks[k - 1])!;
    const to = repByTier.get(ranks[k])!;
    if (g.hasNode(from) && g.hasNode(to)) {
      g.setEdge(from, to, { weight: 0, minlen: 1 });
    }
  }
}
