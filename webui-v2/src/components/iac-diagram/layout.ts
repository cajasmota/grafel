/* ============================================================
   components/iac-diagram/layout.ts — IaC resource-graph → React Flow layout.

   Turns the resolved IaC resource graph (IaCReport, handlers_iac.go) into a
   positioned React Flow graph for the architecture-diagram view (#4526):

     - nodes  = IaC resources, styled by resource_category.
     - edges  = DEPENDS_ON / USES relations (grants / event-sources /
                dependencies / topology), drawn between two RENDERED resource
                nodes by joining IaCRelation.target_entity_id against
                IaCResource.entity_id (the slug-prefixed key the backend adds).
     - groups = module containers: resources sharing a `module` are laid out as
                children of a React Flow parent (group) node, so a modularized
                Terraform / CDK stack reads as grouped boxes. This is the key
                ask — archigraph flattens modules to the resolved resource
                graph, so grouping by module reconstructs the stack structure.

   Layout engine: as of the elkjs epic (#4824/#4826) the default backend is ELK
   (elk.hierarchyHandling INCLUDE_CHILDREN + orthogonal routing) via the shared
   `layoutWithElk` helper (lib/elk-layout.ts) — it lays out the nested module/
   tier groups NATIVELY rather than dagre's faked compound (setParent + manual
   bounding boxes). The legacy dagre compound pass is KEPT as a synchronous
   fallback behind `VITE_IAC_LAYOUT_ENGINE=dagre` for a visual revert.

   The "planning" stage (which resources render, which group each belongs to,
   the drawn vs unresolved edges) is engine-agnostic and shared by both
   backends; only the positioning differs.
   ============================================================ */

import dagre from "dagre";
import { Position, type Node, type Edge } from "@xyflow/react";
import type { IaCReport, IaCResource } from "@/data/types";
import {
  layoutWithElk,
  type ElkLayoutNode,
  type ElkLayoutEdge,
} from "@/lib/elk-layout";

export type IaCDiagramDirection = "LR" | "TB";

/**
 * Grouping mode for the diagram's container boxes (#4625):
 *   - "module" — group by the Terraform module / construct / stack directory.
 *   - "tier"   — group by cloud architecture tier (Compute / Messaging /
 *                Observability / Security & IAM / Data / Network / Other),
 *                derived from resource_category, so the diagram reads as a
 *                layered cloud-architecture view regardless of module layout.
 */
export type IaCGroupMode = "module" | "tier";

/**
 * Layout engine selection. Defaults to ELK (#4826); set the env flag
 * `VITE_IAC_LAYOUT_ENGINE=dagre` (or pass engine="dagre") to use the legacy
 * dagre fallback for a visual revert.
 */
export type IaCLayoutEngine = "elk" | "dagre";
export function defaultLayoutEngine(): IaCLayoutEngine {
  const env =
    typeof import.meta !== "undefined"
      ? (import.meta as { env?: Record<string, string | undefined> }).env
      : undefined;
  return env?.VITE_IAC_LAYOUT_ENGINE === "dagre" ? "dagre" : "elk";
}

/**
 * cloudTier maps a resource_category to a coarse cloud-architecture tier used by
 * the "tier" grouping mode. Categories that don't map fall to "Other".
 */
export function cloudTier(category?: string): string {
  switch ((category || "").toLowerCase()) {
    case "compute":
    case "function":
    case "module":
      return "Compute";
    case "queue":
    case "topic":
    case "stream":
      return "Messaging";
    case "datastore":
    case "cache":
    case "storage":
      return "Data";
    case "network":
      return "Network";
    case "secret":
      return "Security & IAM";
    default:
      return "Other";
  }
}

export const IAC_NODE_TYPE = "iacResource";
export const IAC_GROUP_TYPE = "iacModule";
export const IAC_EDGE_TYPE = "iacRelation";

/** Resource-node box fed to the layout engine (the renderer matches these dims). */
const NODE_W = 220;
const NODE_H = 64;

/** Padding inside a module group container (top leaves room for the header). */
const GROUP_PAD_X = 18;
const GROUP_PAD_TOP = 40;
const GROUP_PAD_BOTTOM = 18;

/** Hard ceiling so a huge stack can't hang the browser. */
export const MAX_DIAGRAM_NODES = 500;

export interface IaCEdgeData {
  facet: string;
  kind: string;
  detail?: string;
  [key: string]: unknown;
}

export interface IaCNodeData {
  resource: IaCResource;
  /** Count of relation targets that could not be joined to a rendered node. */
  unresolvedCount: number;
  [key: string]: unknown;
}

export interface IaCGroupData {
  module: string;
  /** Short trailing segment of the module path, for the header label. */
  shortLabel: string;
  count: number;
  [key: string]: unknown;
}

export interface IaCLayoutResult {
  nodes: Node[];
  edges: Edge[];
  /** True when the resource set was clipped at MAX_DIAGRAM_NODES. */
  capped: boolean;
  /** Number of relation endpoints that resolved to no rendered node. */
  unresolvedEdges: number;
}

/** Flatten the tool-grouped report into a single resource list. */
export function flattenResources(report: IaCReport | undefined): IaCResource[] {
  if (!report) return [];
  const out: IaCResource[] = [];
  for (const g of report.groups) {
    for (const r of g.resources) out.push(r);
  }
  return out;
}

/** Trailing path segment of a module key, for a compact group header. */
function shortModuleLabel(module: string): string {
  const cleaned = module.replace(/\/+$/, "");
  const i = cleaned.lastIndexOf("/");
  return i >= 0 ? cleaned.slice(i + 1) : cleaned;
}

interface RawEdge { from: string; to: string; facet: string; kind: string; detail?: string }

/**
 * IaCPlan is the engine-agnostic intermediate produced from the report: the
 * rendered resources, their group bucket, the group order/labels, and the drawn
 * (resolved) edges. Both the dagre and ELK positioners consume this identically.
 */
interface IaCPlan {
  capped: boolean;
  unresolvedEdges: number;
  resources: IaCResource[];
  /** Insertion-ordered group id → members. */
  modules: Map<string, IaCResource[]>;
  /** group bucket key for a resource (module path or cloud tier). */
  moduleOf: (r: IaCResource) => string;
  /** Unresolved relation count for a single resource (for its node chip). */
  unresolvedCountFor: (r: IaCResource) => number;
  rawEdges: RawEdge[];
}

/** group id for a module/tier bucket name (the React Flow parent node id). */
function groupIdFor(module: string): string {
  return `group:${module}`;
}

/**
 * planIaCDiagram computes the engine-agnostic render plan: capped resource set,
 * group buckets, and resolved (drawn) vs unresolved edges. This is the
 * IaC-specific logic shared by both the ELK and dagre backends.
 */
function planIaCDiagram(
  report: IaCReport | undefined,
  groupMode: IaCGroupMode,
): IaCPlan | undefined {
  const all = flattenResources(report);
  const capped = all.length > MAX_DIAGRAM_NODES;
  const resources = capped ? all.slice(0, MAX_DIAGRAM_NODES) : all;

  if (resources.length === 0) return undefined;

  // Index rendered resources by their slug-prefixed entity id so edges can join.
  const byEntityId = new Map<string, IaCResource>();
  for (const r of resources) byEntityId.set(r.entity_id, r);

  // Assign each resource a container bucket. In "module" mode that is the
  // module/stack directory ("" → "(ungrouped)"); in "tier" mode it is the cloud
  // architecture tier derived from resource_category (#4625). Either way every
  // node gets a parent container so the compound layout stays uniform.
  const moduleOf = (r: IaCResource) =>
    groupMode === "tier" ? cloudTier(r.category) : r.module || "(ungrouped)";
  const modules = new Map<string, IaCResource[]>();
  for (const r of resources) {
    const m = moduleOf(r);
    const list = modules.get(m);
    if (list) list.push(r);
    else modules.set(m, [r]);
  }

  // ── Edges. Draw only edges between two rendered nodes, de-duplicated by
  // (from,to,kind). We follow the "out" direction so each edge is drawn once;
  // unresolved targets (no target_entity_id, or not a rendered node) are
  // counted, not drawn. ────────────────────────────────────────────────────
  const rawEdges: RawEdge[] = [];
  const seen = new Set<string>();
  let unresolvedEdges = 0;

  for (const r of resources) {
    for (const rel of r.relations) {
      if (rel.direction !== "out") continue; // "in" is the mirror of some "out".
      const targetEntity = rel.target_entity_id;
      if (!targetEntity || !byEntityId.has(targetEntity)) {
        unresolvedEdges++;
        continue;
      }
      const key = `${r.entity_id}|${targetEntity}|${rel.kind}`;
      if (seen.has(key)) continue;
      seen.add(key);
      rawEdges.push({
        from: r.entity_id,
        to: targetEntity,
        facet: rel.facet,
        kind: rel.kind,
        detail: rel.detail,
      });
    }
  }

  const unresolvedCountFor = (r: IaCResource) =>
    r.relations.filter(
      (rel) => !rel.target_entity_id || !byEntityId.has(rel.target_entity_id),
    ).length;

  return {
    capped,
    unresolvedEdges,
    resources,
    modules,
    moduleOf,
    unresolvedCountFor,
    rawEdges,
  };
}

/**
 * materialize turns an IaCPlan + absolute boxes for every laid-out resource and
 * group into the final React-Flow node/edge lists (parent-relative positions,
 * derived group boxes). Engine-agnostic: both dagre and ELK feed it their
 * absolute boxes. `groupBox` is optional per group — when an engine sizes the
 * container natively (ELK) it is passed through; otherwise the box is derived
 * from member bounding boxes (dagre).
 */
function materialize(
  plan: IaCPlan,
  groupMode: IaCGroupMode,
  /** Absolute top-left position + size of each rendered resource. */
  resourceAbs: Map<string, { x: number; y: number }>,
  /** Optional engine-provided absolute group box; else derived from members. */
  groupAbs: Map<string, { x: number; y: number; w: number; h: number }> | undefined,
  direction: IaCDiagramDirection,
): IaCLayoutResult {
  const { resources, modules, unresolvedCountFor, rawEdges, capped, unresolvedEdges } = plan;

  const sourcePos = direction === "LR" ? Position.Right : Position.Bottom;
  const targetPos = direction === "LR" ? Position.Left : Position.Top;

  const nodes: Node[] = [];

  for (const [module, members] of modules) {
    const groupId = groupIdFor(module);

    // Group bounding box: engine-provided (ELK) or derived from members (dagre).
    let gx: number, gy: number, gw: number, gh: number;
    const provided = groupAbs?.get(groupId);
    if (provided) {
      gx = provided.x;
      gy = provided.y;
      gw = provided.w;
      gh = provided.h;
    } else {
      let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
      for (const r of members) {
        const p = resourceAbs.get(r.entity_id);
        if (!p) continue;
        minX = Math.min(minX, p.x);
        minY = Math.min(minY, p.y);
        maxX = Math.max(maxX, p.x + NODE_W);
        maxY = Math.max(maxY, p.y + NODE_H);
      }
      if (!Number.isFinite(minX)) continue; // empty container → skip.
      gx = minX - GROUP_PAD_X;
      gy = minY - GROUP_PAD_TOP;
      gw = maxX - minX + GROUP_PAD_X * 2;
      gh = maxY - minY + GROUP_PAD_TOP + GROUP_PAD_BOTTOM;
    }

    const groupData: IaCGroupData = {
      module,
      shortLabel:
        module === "(ungrouped)"
          ? "ungrouped"
          : groupMode === "tier"
            ? module
            : shortModuleLabel(module),
      count: members.length,
    };
    nodes.push({
      id: groupId,
      type: IAC_GROUP_TYPE,
      position: { x: gx, y: gy },
      data: groupData,
      width: gw,
      height: gh,
      draggable: false,
      selectable: false,
      // Render behind children.
      zIndex: 0,
    });

    for (const r of members) {
      const p = resourceAbs.get(r.entity_id);
      if (!p) continue;
      const nodeData: IaCNodeData = {
        resource: r,
        unresolvedCount: unresolvedCountFor(r),
      };
      nodes.push({
        id: r.entity_id,
        type: IAC_NODE_TYPE,
        parentId: groupId,
        extent: "parent",
        // Position relative to the parent group.
        position: { x: p.x - gx, y: p.y - gy },
        data: nodeData,
        sourcePosition: sourcePos,
        targetPosition: targetPos,
        width: NODE_W,
        height: NODE_H,
        zIndex: 1,
      });
    }
  }

  const edges: Edge[] = rawEdges.map((e, i) => {
    const data: IaCEdgeData = { facet: e.facet, kind: e.kind, detail: e.detail };
    return {
      id: `e:${e.from}->${e.to}:${e.kind}:${i}`,
      source: e.from,
      target: e.to,
      type: IAC_EDGE_TYPE,
      data,
      zIndex: 2,
    };
  });

  // resources param kept referenced for clarity/parity with the plan.
  void resources;

  return { nodes, edges, capped, unresolvedEdges };
}

/* ============================================================
   ELK backend (default, async) — #4826.
   ============================================================ */

/**
 * layoutIaCDiagramElk builds a positioned, module-grouped React Flow graph using
 * the shared ELK helper. ELK lays out the nested group containers NATIVELY
 * (hierarchyHandling INCLUDE_CHILDREN, handled inside layoutWithElk) and routes
 * edges orthogonally (the architecture-diagram look). Async — ELK layout is
 * Promise-based.
 *
 * @param report     the IaCReport (already fetched)
 * @param direction  "LR" (horizontal) | "TB" (vertical) → ELK direction
 * @param groupMode  module vs cloud-tier container buckets
 */
export async function layoutIaCDiagramElk(
  report: IaCReport | undefined,
  direction: IaCDiagramDirection,
  groupMode: IaCGroupMode = "module",
): Promise<IaCLayoutResult> {
  const plan = planIaCDiagram(report, groupMode);
  if (!plan) {
    return {
      nodes: [],
      edges: [],
      capped: flattenResources(report).length > MAX_DIAGRAM_NODES,
      unresolvedEdges: 0,
    };
  }

  const { modules, rawEdges } = plan;

  // ── Build ELK inputs: one container per group, resources as its children. ──
  const elkNodes: ElkLayoutNode[] = [];
  for (const [module, members] of modules) {
    const groupId = groupIdFor(module);
    elkNodes.push({ id: groupId, isContainer: true });
    for (const r of members) {
      elkNodes.push({
        id: r.entity_id,
        parentId: groupId,
        width: NODE_W,
        height: NODE_H,
      });
    }
  }

  const elkEdges: ElkLayoutEdge[] = rawEdges.map((e, i) => ({
    id: `elk-e:${i}`,
    source: e.from,
    target: e.to,
  }));

  const positions = await layoutWithElk(elkNodes, elkEdges, {
    direction: direction === "LR" ? "RIGHT" : "DOWN",
    edgeRouting: "ORTHOGONAL",
    nodeSpacing: direction === "LR" ? 26 : 40,
    layerSpacing: direction === "LR" ? 80 : 64,
    padding: {
      top: GROUP_PAD_TOP,
      right: GROUP_PAD_X,
      bottom: GROUP_PAD_BOTTOM,
      left: GROUP_PAD_X,
    },
    defaultNodeWidth: NODE_W,
    defaultNodeHeight: NODE_H,
  });

  // ELK positions are parent-relative. Resources nest exactly one level under
  // their group, so a resource's absolute position = group pos + resource pos.
  const finite = (v: number | undefined) =>
    Number.isFinite(v) ? (v as number) : 0;

  const groupAbs = new Map<string, { x: number; y: number; w: number; h: number }>();
  for (const module of modules.keys()) {
    const groupId = groupIdFor(module);
    const gp = positions.get(groupId);
    groupAbs.set(groupId, {
      x: finite(gp?.x),
      y: finite(gp?.y),
      w: finite(gp?.width),
      h: finite(gp?.height),
    });
  }

  const resourceAbs = new Map<string, { x: number; y: number }>();
  for (const [module, members] of modules) {
    const g = groupAbs.get(groupIdFor(module))!;
    for (const r of members) {
      const rp = positions.get(r.entity_id);
      resourceAbs.set(r.entity_id, {
        x: g.x + finite(rp?.x),
        y: g.y + finite(rp?.y),
      });
    }
  }

  return materialize(plan, groupMode, resourceAbs, groupAbs, direction);
}

/* ============================================================
   dagre backend (legacy fallback, synchronous) — kept for visual revert.
   ============================================================ */

/**
 * layoutIaCDiagram builds a positioned, module-grouped React Flow graph using
 * the legacy dagre compound pass (setParent + member bounding boxes). Kept as a
 * synchronous fallback (see defaultLayoutEngine / VITE_IAC_LAYOUT_ENGINE=dagre).
 *
 * @param report     the IaCReport (already fetched)
 * @param direction  "LR" (horizontal) | "TB" (vertical) → dagre rankdir
 */
export function layoutIaCDiagram(
  report: IaCReport | undefined,
  direction: IaCDiagramDirection,
  groupMode: IaCGroupMode = "module",
): IaCLayoutResult {
  const plan = planIaCDiagram(report, groupMode);
  if (!plan) {
    return {
      nodes: [],
      edges: [],
      capped: flattenResources(report).length > MAX_DIAGRAM_NODES,
      unresolvedEdges: 0,
    };
  }

  const { resources, modules, rawEdges } = plan;

  // ── Compound dagre layout: children grouped under their module cluster. ───
  const g = new dagre.graphlib.Graph({ compound: true });
  g.setGraph({
    rankdir: direction,
    nodesep: direction === "LR" ? 26 : 40,
    ranksep: direction === "LR" ? 80 : 64,
    marginx: 20,
    marginy: 20,
  });
  g.setDefaultEdgeLabel(() => ({}));

  for (const [module, members] of modules) {
    const groupId = groupIdFor(module);
    // Cluster node — dagre sizes it to fit children; we recompute the box below.
    g.setNode(groupId, { label: module });
    for (const r of members) {
      g.setNode(r.entity_id, { width: NODE_W, height: NODE_H });
      g.setParent(r.entity_id, groupId);
    }
  }
  for (const e of rawEdges) g.setEdge(e.from, e.to);

  dagre.layout(g);

  // Resource node absolute positions (dagre centers; RF uses top-left).
  const resourceAbs = new Map<string, { x: number; y: number }>();
  for (const r of resources) {
    const n = g.node(r.entity_id);
    resourceAbs.set(r.entity_id, {
      x: (n?.x ?? 0) - NODE_W / 2,
      y: (n?.y ?? 0) - NODE_H / 2,
    });
  }

  // dagre doesn't give us reliable padded group boxes; let materialize derive
  // them from member bounding boxes (pass groupAbs = undefined).
  return materialize(plan, groupMode, resourceAbs, undefined, direction);
}
