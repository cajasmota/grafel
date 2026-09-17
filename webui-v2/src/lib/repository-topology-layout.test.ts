import { describe, expect, it } from "vitest";
import type {
  RepositoryTopologyEdge,
  RepositoryTopologyNode,
} from "@/data/types";
import {
  layoutRepositoryTopology,
  repositoryEdgeDash,
  repositoryEdgeWidth,
  repositoryProtocolColor,
} from "./repository-topology-layout";

const nodes: RepositoryTopologyNode[] = [
  {
    id: "client",
    repository: "client",
    label: "CLIENT",
    primary_language: "Java",
    entity_count: 120,
    module_count: 4,
    inbound_relationships: 2,
    outbound_relationships: 7,
    connected_repositories: 2,
    evidence: { confirmed: 6, inferred: 1, dangling: 0, ambiguous: 0, external: 0 },
    modules: ["client-api", "client-service"],
  },
  {
    id: "rule",
    repository: "rule",
    label: "Rule",
    primary_language: "Java",
    entity_count: 80,
    module_count: 2,
    inbound_relationships: 5,
    outbound_relationships: 1,
    connected_repositories: 2,
    evidence: { confirmed: 5, inferred: 0, dangling: 0, ambiguous: 0, external: 0 },
    modules: ["rule-api", "rule-service"],
  },
  {
    id: "gateway",
    repository: "gateway",
    label: "Gateway",
    primary_language: "Java",
    entity_count: 60,
    module_count: 1,
    inbound_relationships: 1,
    outbound_relationships: 3,
    connected_repositories: 2,
    evidence: { confirmed: 3, inferred: 0, dangling: 0, ambiguous: 0, external: 0 },
    modules: ["gateway"],
  },
];

const edges: RepositoryTopologyEdge[] = [
  {
    id: "gateway->client:http",
    source: "gateway",
    target: "client",
    channel: "http",
    relationship_count: 3,
    contract_count: 2,
    evidence: { confirmed: 3, inferred: 0, dangling: 0, ambiguous: 0, external: 0 },
    labels: ["POST /orders"],
    samples: [],
    has_more: false,
  },
  {
    id: "client->rule:dubbo",
    source: "client",
    target: "rule",
    channel: "dubbo",
    relationship_count: 5,
    contract_count: 1,
    evidence: { confirmed: 4, inferred: 1, dangling: 0, ambiguous: 0, external: 0 },
    labels: ["RuleService"],
    samples: [],
    has_more: true,
  },
];

function normalizePositions(result: ReturnType<typeof layoutRepositoryTopology>) {
  return result.nodes
    .map((node) => ({
      id: node.id,
      position: node.position,
      sourcePosition: node.sourcePosition,
      targetPosition: node.targetPosition,
    }))
    .sort((left, right) => left.id.localeCompare(right.id));
}

describe("repository topology layout", () => {
  it("is stable across input order", () => {
    const first = layoutRepositoryTopology(nodes, edges, "LR");
    const second = layoutRepositoryTopology([...nodes].reverse(), [...edges].reverse(), "LR");

    expect(normalizePositions(second)).toEqual(normalizePositions(first));
  });

  it("keeps parallel protocol edges selectable", () => {
    const result = layoutRepositoryTopology(nodes.slice(0, 2), [
      edges[1],
      { ...edges[1], id: "client->rule:http", channel: "http" },
    ], "LR");

    expect(result.edges.map((edge) => edge.id).sort()).toEqual([
      "client->rule:dubbo",
      "client->rule:http",
    ]);
    expect(result.edges.every((edge) => edge.selectable)).toBe(true);
  });

  it("uses logarithmic edge widths with a hard cap", () => {
    expect(repositoryEdgeWidth(0)).toBe(1.5);
    expect(repositoryEdgeWidth(2)).toBe(2.5);
    expect(repositoryEdgeWidth(1_000_000)).toBe(8);
  });

  it("maps protocols and evidence to stable visual styles", () => {
    expect(repositoryProtocolColor("dubbo")).toBe("#8b5cf6");
    expect(repositoryProtocolColor("http")).toBe("#0ea5e9");
    expect(repositoryProtocolColor("kafka")).toBe("#f59e0b");
    expect(repositoryProtocolColor("rabbitmq")).toBe("#22c55e");
    expect(repositoryProtocolColor("other")).toBe("#94a3b8");

    expect(repositoryEdgeDash({ confirmed: 2, inferred: 0, dangling: 0, ambiguous: 0, external: 0 })).toBeUndefined();
    expect(repositoryEdgeDash({ confirmed: 1, inferred: 2, dangling: 0, ambiguous: 0, external: 0 })).toBe("8 5");
    expect(repositoryEdgeDash({ confirmed: 0, inferred: 0, dangling: 1, ambiguous: 0, external: 0 })).toBe("2 5");
  });
});
