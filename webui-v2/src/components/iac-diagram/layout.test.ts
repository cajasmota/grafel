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

/** #4862 — ownership-as-containment fixture: a module INSTANCE that
 *  instantiates two definition resources (which carry parent_id = the instance),
 *  plus a real resource→resource architectural edge to keep. */
function instantiationFixture(): IaCReport {
  const inst = res({
    entity_id: "infra/inst",
    name: "module.worker_prod",
    category: "module",
    module: "envs/prod",
    relations: [
      // instantiates edges fan out from the instance to each definition resource.
      rel({
        facet: "instantiates",
        kind: "INSTANTIATES",
        target_entity_id: "infra/task",
        target: "task",
      }),
      rel({
        facet: "instantiates",
        kind: "INSTANTIATES",
        target_entity_id: "infra/queue",
        target: "queue",
      }),
    ],
  });
  const task = res({
    entity_id: "infra/task",
    name: "aws_ecs_task.worker",
    module: "modules/worker-service",
    category: "compute",
    parent_id: "infra/inst",
    relations: [
      // a real resource→resource architectural edge (task → queue) — KEEP it.
      rel({
        facet: "dependency",
        kind: "USES",
        target_entity_id: "infra/queue",
        target: "queue",
      }),
    ],
  });
  const queue = res({
    entity_id: "infra/queue",
    name: "aws_sqs_queue.work",
    module: "modules/worker-service",
    category: "queue",
    parent_id: "infra/inst",
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
    envs: ["prod"],
    counts_by_category: {},
    groups: [{ tool: "terraform", count: 3, resources: [inst, task, queue] }],
  };
}

describe("ownership-as-containment (#4862)", () => {
  it("nests instantiated resources inside their module instance and drops instantiates edges", () => {
    const { nodes, edges, unresolvedEdges } = layoutIaCDiagram(
      instantiationFixture(),
      "LR",
      "module",
    );
    const groups = nodes.filter((n) => n.type === IAC_GROUP_TYPE);
    const resources = nodes.filter((n) => n.type === IAC_NODE_TYPE);

    // One owner-instance container holds the instance + both definitions.
    expect(groups.length).toBe(1);
    expect(resources.length).toBe(3);
    const owner = groups[0];
    // All three resource nodes share the single owner container as their parent.
    for (const r of resources) expect(r.parentId).toBe(owner.id);

    // The two redundant instantiates edges are dropped (nesting expresses them);
    // only the real task→queue architectural edge remains.
    expect(edges.length).toBe(1);
    expect(edges[0].source).toBe("infra/task");
    expect(edges[0].target).toBe("infra/queue");
    expect((edges[0].data as { facet: string }).facet).toBe("dependency");

    // Dropped instantiates edges are NOT counted as unresolved.
    expect(unresolvedEdges).toBe(0);
  });

  it("ELK backend produces the same nested containment plan", async () => {
    const { nodes, edges, unresolvedEdges } = await layoutIaCDiagramElk(
      instantiationFixture(),
      "LR",
      "module",
    );
    const groups = nodes.filter((n) => n.type === IAC_GROUP_TYPE);
    const resources = nodes.filter((n) => n.type === IAC_NODE_TYPE);
    expect(groups.length).toBe(1);
    expect(resources.length).toBe(3);
    expect(edges.length).toBe(1);
    expect(unresolvedEdges).toBe(0);
  });

  it("does not nest by instance in tier mode (parent_id ignored)", () => {
    const { nodes } = layoutIaCDiagram(instantiationFixture(), "LR", "tier");
    const groupIds = nodes
      .filter((n) => n.type === IAC_GROUP_TYPE)
      .map((n) => n.id);
    // tier mode buckets by cloud tier: module→Compute, compute→Compute,
    // queue→Messaging — never an instance: bucket.
    expect(groupIds.some((id) => id.includes("instance:"))).toBe(false);
    expect(groupIds).toContain("group:Compute");
    expect(groupIds).toContain("group:Messaging");
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

  // #4887: leaf resources opt into centered direction-aware ports, so a (even
  // cross-container) edge leaves/enters its leaf endpoint at the centered
  // leading/trailing face — right/left in LR, bottom/top in TB — not the side.
  // The fixture's svc→lb edge crosses TWO module containers; ELK routes through
  // them but the endpoints still anchor at the resources' centered faces.
  for (const dir of ["LR", "TB"] as const) {
    it(`anchors edges at centered leaf faces (${dir})`, async () => {
      const { nodes, edges } = await layoutIaCDiagramElk(fixture(), dir, "module");
      const groups = new Map(
        nodes.filter((n) => n.type === IAC_GROUP_TYPE).map((g) => [g.id, g]),
      );
      // Absolute top-left of a resource = its group's pos + its parent-relative pos.
      const absOf = (id: string) => {
        const r = nodes.find((n) => n.id === id && n.type === IAC_NODE_TYPE)!;
        const g = groups.get(r.parentId as string)!;
        return {
          x: g.position.x + r.position.x,
          y: g.position.y + r.position.y,
          w: Number(r.width),
          h: Number(r.height),
        };
      };
      expect(edges.length).toBe(1);
      const ed = edges[0];
      const pts = (ed.data as { elkPoints?: { x: number; y: number }[] }).elkPoints;
      expect(pts && pts.length >= 2).toBe(true);
      const start = pts![0];
      const end = pts![pts!.length - 1];
      const s = absOf(ed.source);
      const t = absOf(ed.target);
      if (dir === "LR") {
        expect(start.x).toBeCloseTo(s.x + s.w, 1); // right-edge center
        expect(start.y).toBeCloseTo(s.y + s.h / 2, 1);
        expect(end.x).toBeCloseTo(t.x, 1); // left-edge center
        expect(end.y).toBeCloseTo(t.y + t.h / 2, 1);
      } else {
        expect(start.x).toBeCloseTo(s.x + s.w / 2, 1); // bottom-edge center
        expect(start.y).toBeCloseTo(s.y + s.h, 1);
        expect(end.x).toBeCloseTo(t.x + t.w / 2, 1); // top-edge center
        expect(end.y).toBeCloseTo(t.y, 1);
      }
    });
  }
});
