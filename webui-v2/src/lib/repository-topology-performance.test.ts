import { describe, expect, it } from "vitest";
import type { RepositoryTopologyEdge, RepositoryTopologyNode } from "@/data/types";
import { layoutRepositoryTopology } from "./repository-topology-layout";

function node(index: number): RepositoryTopologyNode {
  const id = `repo-${String(index).padStart(3, "0")}`;
  return { id, repository: id, label: id, entity_count: 100, module_count: 2, inbound_relationships: 4, outbound_relationships: 4, connected_repositories: 8, evidence: { confirmed: 8, inferred: 0, dangling: 0, ambiguous: 0, external: 0 }, modules: [] };
}

function edge(index: number): RepositoryTopologyEdge {
  const source = `repo-${String(index % 500).padStart(3, "0")}`;
  const target = `repo-${String((index * 17 + 1) % 500).padStart(3, "0")}`;
  const channel = (["dubbo", "http", "kafka", "rabbitmq"] as const)[index % 4];
  return { id: `${source}->${target}:${channel}:${index}`, source, target, channel, relationship_count: index + 1, contract_count: 1, evidence: { confirmed: 1, inferred: 0, dangling: 0, ambiguous: 0, external: 0 }, labels: [], samples: [], has_more: false };
}

describe("repository topology bounded transformations", () => {
  it("preserves bounded node and edge cardinality deterministically", () => {
    const nodes = Array.from({ length: 500 }, (_, index) => node(index));
    const edges = Array.from({ length: 2_000 }, (_, index) => edge(index));
    const first = layoutRepositoryTopology(nodes, edges, "LR");
    const second = layoutRepositoryTopology([...nodes].reverse(), [...edges].reverse(), "LR");
    expect(first.nodes).toHaveLength(500);
    expect(first.edges).toHaveLength(2_000);
    expect(new Set(first.edges.map((item) => item.id)).size).toBe(2_000);
    expect(second.nodes.map((item) => item.id)).toEqual(first.nodes.map((item) => item.id));
    expect(second.edges.map((item) => item.id)).toEqual(first.edges.map((item) => item.id));
  });
});
