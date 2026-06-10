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

   Layout uses dagre as a COMPOUND graph (setParent) so children cluster under
   their module; we then derive each group node's bounding box from its laid-out
   children. Unresolved edge targets (#4495 target_resolved=false, or a target
   that is not itself a rendered node) are NOT drawn as dead edges — the caller
   surfaces them separately as external/unresolved chips on the node.
   ============================================================ */

import dagre from "dagre";
import { Position, type Node, type Edge } from "@xyflow/react";
import type { IaCReport, IaCResource } from "@/data/types";

export type IaCDiagramDirection = "LR" | "TB";

export const IAC_NODE_TYPE = "iacResource";
export const IAC_GROUP_TYPE = "iacModule";
export const IAC_EDGE_TYPE = "iacRelation";

/** Resource-node box fed to dagre (the renderer matches these dims). */
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

/**
 * Build a positioned, module-grouped React Flow graph from the IaC report.
 *
 * @param report     the IaCReport (already fetched)
 * @param direction  "LR" (horizontal) | "TB" (vertical) → dagre rankdir
 */
export function layoutIaCDiagram(
  report: IaCReport | undefined,
  direction: IaCDiagramDirection,
): IaCLayoutResult {
  const all = flattenResources(report);
  const capped = all.length > MAX_DIAGRAM_NODES;
  const resources = capped ? all.slice(0, MAX_DIAGRAM_NODES) : all;

  if (resources.length === 0) {
    return { nodes: [], edges: [], capped, unresolvedEdges: 0 };
  }

  // Index rendered resources by their slug-prefixed entity id so edges can join.
  const byEntityId = new Map<string, IaCResource>();
  for (const r of resources) byEntityId.set(r.entity_id, r);

  // Assign each resource a module bucket. "" module → a synthetic "(ungrouped)"
  // bucket so every node still has a parent container (keeps layout uniform).
  const moduleOf = (r: IaCResource) => r.module || "(ungrouped)";
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
  interface RawEdge { from: string; to: string; facet: string; kind: string; detail?: string }
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
    const groupId = `group:${module}`;
    // Cluster node — dagre sizes it to fit children; we recompute the box below.
    g.setNode(groupId, { label: module });
    for (const r of members) {
      g.setNode(r.entity_id, { width: NODE_W, height: NODE_H });
      g.setParent(r.entity_id, groupId);
    }
  }
  for (const e of rawEdges) g.setEdge(e.from, e.to);

  dagre.layout(g);

  const sourcePos = direction === "LR" ? Position.Right : Position.Bottom;
  const targetPos = direction === "LR" ? Position.Left : Position.Top;

  // Resource node absolute positions (dagre centers; RF uses top-left).
  const absPos = new Map<string, { x: number; y: number }>();
  for (const r of resources) {
    const n = g.node(r.entity_id);
    absPos.set(r.entity_id, {
      x: (n?.x ?? 0) - NODE_W / 2,
      y: (n?.y ?? 0) - NODE_H / 2,
    });
  }

  // Group bounding boxes derived from member positions (padded). React Flow
  // child positions are RELATIVE to the parent, so we offset each child by its
  // group's top-left.
  const nodes: Node[] = [];

  for (const [module, members] of modules) {
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const r of members) {
      const p = absPos.get(r.entity_id)!;
      minX = Math.min(minX, p.x);
      minY = Math.min(minY, p.y);
      maxX = Math.max(maxX, p.x + NODE_W);
      maxY = Math.max(maxY, p.y + NODE_H);
    }
    const gx = minX - GROUP_PAD_X;
    const gy = minY - GROUP_PAD_TOP;
    const gw = maxX - minX + GROUP_PAD_X * 2;
    const gh = maxY - minY + GROUP_PAD_TOP + GROUP_PAD_BOTTOM;

    const groupId = `group:${module}`;
    const groupData: IaCGroupData = {
      module,
      shortLabel: module === "(ungrouped)" ? "ungrouped" : shortModuleLabel(module),
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
      const p = absPos.get(r.entity_id)!;
      const unresolvedCount = r.relations.filter(
        (rel) => !rel.target_entity_id || !byEntityId.has(rel.target_entity_id),
      ).length;
      const nodeData: IaCNodeData = { resource: r, unresolvedCount };
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

  return { nodes, edges, capped, unresolvedEdges };
}
