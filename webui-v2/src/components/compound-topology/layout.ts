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

   Layout uses dagre as a COMPOUND graph (setParent) for the hierarchy, with a
   tier-rank constraint so nodes of the same tier share a column. We then derive
   each (expanded) zone's bounding box from its laid-out children.

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

/**
 * layoutCompoundTopology builds a positioned React Flow graph.
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

  const capped = report.nodes.length > MAX_NODES;
  const allNodes = capped ? report.nodes.slice(0, MAX_NODES) : report.nodes;

  const nodeIndex = new Map<string, CompoundNode>();
  for (const n of allNodes) nodeIndex.set(n.id, n);

  const zoneIndex = new Map(report.zones.map((z) => [z.id, z]));

  // A zone is RENDERED collapsed only when it is collapsed AND no collapsed
  // ancestor already swallows it (avoid drawing a zone-in-a-collapsed-zone).
  const isCollapsedRendered = (zoneId: string): boolean => {
    if (!collapsed.has(zoneId)) return false;
    let z = zoneIndex.get(zoneId);
    while (z && z.parent_id) {
      if (collapsed.has(z.parent_id)) return false;
      z = zoneIndex.get(z.parent_id);
    }
    return true;
  };

  // ── Visible leaves: nodes whose zone_path has no collapsed ancestor. ──────
  const visibleNodes = allNodes.filter(
    (n) => collapsedAncestor(n.zone_path, collapsed) === "",
  );

  // Each visible node's innermost VISIBLE zone is its React Flow parent. A zone
  // is visible when it is not inside a collapsed zone.
  const innermostVisibleZone = (n: CompoundNode): string => {
    let parent = "";
    for (const zid of n.zone_path) {
      if (collapsed.has(zid)) break; // stop before a collapsed boundary.
      parent = zid;
    }
    return parent;
  };

  // ── Which zones do we render as boxes? ────────────────────────────────────
  // - collapsed-rendered zones → leaf boxes (no children).
  // - expanded zones that contain ≥1 visible node OR a collapsed-rendered
  //   child zone → container boxes.
  const renderedZoneIds = new Set<string>();
  for (const z of report.zones) {
    if (isCollapsedRendered(z.id)) {
      renderedZoneIds.add(z.id);
      // also render all expanded ancestors (so the box nests visibly).
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

  const zoneParentOf = (zoneId: string): string => {
    const z = zoneIndex.get(zoneId);
    let p = z?.parent_id || "";
    // climb to nearest rendered ancestor.
    while (p && !renderedZoneIds.has(p)) {
      p = zoneIndex.get(p)?.parent_id || "";
    }
    return p;
  };

  // Zone container/leaf nodes.
  for (const zid of renderedZoneIds) {
    if (isCollapsedRendered(zid)) {
      // Collapsed → a sized leaf (dagre lays it out like a node).
      g.setNode(zid, { width: NODE_W + 24, height: NODE_H + 8 });
    } else {
      g.setNode(zid, {}); // container; dagre sizes from children.
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

  // ── Edge folding. Re-target each typed edge at the effective (possibly
  // collapsed-zone) endpoints, drop self/inside-collapsed edges, and aggregate
  // by (src,tgt,type) so collapsed zones surface ONE summary edge. ──────────
  interface FoldedEdge {
    from: string;
    to: string;
    type: CompoundEdgeType;
    count: number;
    summary: boolean;
  }
  const folded = new Map<string, FoldedEdge>();
  for (const e of report.edges) {
    const src = effectiveEndpoint(e.source, nodeIndex, collapsed);
    const tgt = effectiveEndpoint(e.target, nodeIndex, collapsed);
    if (!nodeIndex.has(e.source) || !nodeIndex.has(e.target)) continue;
    if (src === tgt) continue; // wholly inside one collapsed zone → hidden.
    // An endpoint is a summary edge when at least one side was re-targeted to
    // a collapsed zone box (i.e. it differs from the raw node id).
    const summary = src !== e.source || tgt !== e.target;
    const key = `${src} ${tgt} ${e.type}`;
    const prev = folded.get(key);
    if (prev) {
      prev.count += 1;
      prev.summary = prev.summary || summary;
    } else {
      folded.set(key, { from: src, to: tgt, type: e.type, count: 1, summary });
    }
  }

  // Lay edges into dagre only when BOTH endpoints are laid-out nodes/zones.
  const layoutable = (id: string) =>
    g.hasNode(id) && (nodeIndex.has(id) || renderedZoneIds.has(id));
  for (const fe of folded.values()) {
    if (layoutable(fe.from) && layoutable(fe.to)) g.setEdge(fe.from, fe.to);
  }

  // Tier-column hint: chain nodes within each tier so dagre keeps lanes ordered
  // (invisible weight-0 edges between consecutive tiers' representatives).
  applyTierHints(g, visibleNodes);

  dagre.layout(g);

  // ── Materialize React Flow nodes. ─────────────────────────────────────────
  const out: Node[] = [];
  const finite = (v: number | undefined) => (Number.isFinite(v) ? (v as number) : 0);

  // Absolute positions for everything dagre placed.
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

  // Recompute EXPANDED container boxes from member bounding boxes (dagre's own
  // cluster sizing is unreliable for our padded headers).
  const memberBox = new Map<string, { minX: number; minY: number; maxX: number; maxY: number }>();
  const noteMember = (zoneId: string, a: { x: number; y: number; w: number; h: number }) => {
    let p: string | undefined = zoneId;
    while (p && renderedZoneIds.has(p)) {
      const b = memberBox.get(p) ?? { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
      b.minX = Math.min(b.minX, a.x);
      b.minY = Math.min(b.minY, a.y);
      b.maxX = Math.max(b.maxX, a.x + a.w);
      b.maxY = Math.max(b.maxY, a.y + a.h);
      memberBox.set(p, b);
      p = zoneIndex.get(p)?.parent_id;
      while (p && !renderedZoneIds.has(p)) p = zoneIndex.get(p)?.parent_id;
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
  const depthOf = (zid: string): number => {
    let d = 0;
    let p = zoneIndex.get(zid)?.parent_id;
    while (p) {
      if (renderedZoneIds.has(p)) d++;
      p = zoneIndex.get(p)?.parent_id;
    }
    return d;
  };
  const zoneOrder = [...renderedZoneIds].sort((a, b) => depthOf(a) - depthOf(b));

  // Emit zones. For expanded containers, derive padded box from members.
  const zoneAbs = new Map<string, { x: number; y: number }>();
  for (const zid of zoneOrder) {
    const z = zoneIndex.get(zid)!;
    const collapsedRendered = isCollapsedRendered(zid);
    let x: number, y: number, w: number, h: number;
    if (collapsedRendered) {
      const a = abs.get(zid)!;
      x = a.x; y = a.y; w = a.w; h = a.h;
    } else {
      const b = memberBox.get(zid);
      if (!b || !Number.isFinite(b.minX)) continue; // empty container → skip.
      x = b.minX - ZONE_PAD_X;
      y = b.minY - ZONE_PAD_TOP;
      w = b.maxX - b.minX + ZONE_PAD_X * 2;
      h = b.maxY - b.minY + ZONE_PAD_TOP + ZONE_PAD_BOTTOM;
    }
    zoneAbs.set(zid, { x, y });

    // Parent-relative position for nested zones.
    const parent = zoneParentOf(zid);
    const pAbs = parent ? zoneAbs.get(parent) : undefined;
    const pos = pAbs ? { x: x - pAbs.x, y: y - pAbs.y } : { x, y };

    const data: CTZoneData = {
      label: z.label,
      kind: z.kind,
      zoneId: zid,
      collapsed: collapsedRendered,
      nodeCount: z.node_count,
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
      ...(parent && renderedZoneIds.has(parent) ? { parentId: parent, extent: "parent" as const } : {}),
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

  return { nodes: out, edges, capped, summaryEdgeCount };
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
