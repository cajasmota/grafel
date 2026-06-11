/* layout.test.ts — IaC architecture-diagram layout: nested module/tier group
   mapping + resolved-vs-unresolved edges, across the dagre and ELK (#4826)
   backends. */

import { describe, it, expect } from "vitest";
import {
  layoutIaCDiagram,
  layoutIaCDiagramElk,
  IAC_NODE_TYPE,
  IAC_GROUP_TYPE,
} from "./layout";
import type { IaCReport, IaCResource, IaCRelation } from "@/data/types";

function rel(partial: Partial<IaCRelation>): IaCRelation {
  return {
    facet: "dependency",
    kind: "DEPENDS_ON",
    direction: "out",
    target: "",
    target_resolved: true,
    target_id: "",
    ...partial,
  };
}

function res(partial: Partial<IaCResource> & { entity_id: string }): IaCResource {
  return {
    repo: "infra",
    name: partial.entity_id,
    tool: "terraform",
    category: "compute",
    properties: [],
    relations: [],
    ...partial,
  };
}

/** Two modules: `net` (1 resource) and `app` (2 resources); app→net edge,
 *  plus one unresolved relation target. */
function fixture(): IaCReport {
  const lb = res({
    entity_id: "infra/lb",
    module: "modules/network",
    category: "network",
    relations: [],
  });
  const svc = res({
    entity_id: "infra/svc",
    module: "modules/app",
    category: "compute",
    relations: [
      rel({ target_entity_id: "infra/lb", target: "lb", kind: "USES" }),
      // Unresolved: target not a rendered resource.
      rel({ target_entity_id: "", target: "external", target_resolved: false }),
    ],
  });
  const db = res({
    entity_id: "infra/db",
    module: "modules/app",
    category: "datastore",
    relations: [],
  });
  return {
    group: "g",
    total_resources: 3,
    total_grants: 0,
    total_event_sources: 0,
    total_dependencies: 1,
    total_outputs: 0,
    with_props_count: 0,
    tools: ["terraform"],
    envs: [],
    counts_by_category: {},
    groups: [{ tool: "terraform", count: 3, resources: [lb, svc, db] }],
  };
}

describe("layoutIaCDiagram (dagre fallback)", () => {
  it("maps resources into module group containers with finite positions", () => {
    const { nodes, edges, unresolvedEdges } = layoutIaCDiagram(fixture(), "LR", "module");
    const groups = nodes.filter((n) => n.type === IAC_GROUP_TYPE);
    const resources = nodes.filter((n) => n.type === IAC_NODE_TYPE);
    expect(groups.length).toBe(2); // modules/network + modules/app
    expect(resources.length).toBe(3);

    // Every resource node is parented to a rendered group container.
    for (const r of resources) {
      expect(r.parentId).toBeDefined();
      expect(groups.some((g) => g.id === r.parentId)).toBe(true);
    }
    for (const n of nodes) {
      expect(Number.isFinite(n.position.x)).toBe(true);
      expect(Number.isFinite(n.position.y)).toBe(true);
    }

    // Only the resolved svc→lb edge is drawn; the external one is unresolved.
    expect(edges.length).toBe(1);
    expect(edges[0].source).toBe("infra/svc");
    expect(edges[0].target).toBe("infra/lb");
    expect(unresolvedEdges).toBe(1);
  });

  it("groups by cloud tier in tier mode", () => {
    const { nodes } = layoutIaCDiagram(fixture(), "LR", "tier");
    const groupIds = nodes.filter((n) => n.type === IAC_GROUP_TYPE).map((n) => n.id);
    // network → Network, compute → Compute, datastore → Data.
    expect(groupIds).toContain("group:Network");
    expect(groupIds).toContain("group:Compute");
    expect(groupIds).toContain("group:Data");
  });

  it("returns empty for an empty report", () => {
    const { nodes, edges } = layoutIaCDiagram(undefined, "LR", "module");
    expect(nodes).toEqual([]);
    expect(edges).toEqual([]);
  });
});

describe("layoutIaCDiagramElk (#4826 — ELK backend)", () => {
  it("produces the same nested-group render plan as dagre with finite positions", async () => {
    const { nodes, edges, unresolvedEdges } = await layoutIaCDiagramElk(
      fixture(),
      "LR",
      "module",
    );
    const groups = nodes.filter((n) => n.type === IAC_GROUP_TYPE);
    const resources = nodes.filter((n) => n.type === IAC_NODE_TYPE);
    expect(groups.length).toBe(2);
    expect(resources.length).toBe(3);

    // Native nested mapping: each resource is a child of its group container,
    // and ELK sizes the container to fit its children.
    for (const r of resources) {
      const parent = groups.find((g) => g.id === r.parentId);
      expect(parent).toBeDefined();
      expect(Number(parent!.width)).toBeGreaterThan(0);
      expect(Number(parent!.height)).toBeGreaterThan(0);
    }
    for (const n of nodes) {
      expect(Number.isFinite(n.position.x)).toBe(true);
      expect(Number.isFinite(n.position.y)).toBe(true);
    }

    expect(edges.length).toBe(1);
    expect(unresolvedEdges).toBe(1);
  });

  it("returns empty for an empty report", async () => {
    const { nodes, edges } = await layoutIaCDiagramElk(undefined, "LR", "module");
    expect(nodes).toEqual([]);
    expect(edges).toEqual([]);
  });
});
