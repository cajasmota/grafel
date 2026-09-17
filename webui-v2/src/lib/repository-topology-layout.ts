import dagre from "dagre";
import {
  MarkerType,
  Position,
  type Edge,
  type Node,
} from "@xyflow/react";
import type {
  RepositoryChannel,
  RepositoryEvidenceCounts,
  RepositoryTopologyEdge,
  RepositoryTopologyNode,
} from "@/data/types";

export type RepositoryTopologyDirection = "LR" | "TB";

export const REPOSITORY_NODE_TYPE = "repositoryTopologyNode";
export const REPOSITORY_EDGE_TYPE = "repositoryTopologyEdge";
export const REPOSITORY_NODE_WIDTH = 232;
export const REPOSITORY_NODE_HEIGHT = 112;

export interface RepositoryNodeData {
  repository: RepositoryTopologyNode;
  [key: string]: unknown;
}

export interface RepositoryEdgeData {
  relationship: RepositoryTopologyEdge;
  parallelIndex: number;
  parallelCount: number;
  [key: string]: unknown;
}

export interface RepositoryTopologyLayout {
  nodes: Node<RepositoryNodeData>[];
  edges: Edge<RepositoryEdgeData>[];
}

const PROTOCOL_COLORS: Record<RepositoryChannel, string> = {
  dubbo: "#8b5cf6",
  http: "#0ea5e9",
  kafka: "#f59e0b",
  rabbitmq: "#22c55e",
  other: "#94a3b8",
};

export function repositoryProtocolColor(channel: RepositoryChannel): string {
  return PROTOCOL_COLORS[channel];
}

export function repositoryEdgeWidth(count: number): number {
  return Math.min(8, 1.5 + Math.log2(Math.max(1, count)));
}

export function repositoryEdgeDash(evidence: RepositoryEvidenceCounts): string | undefined {
  if (evidence.dangling > 0 || evidence.ambiguous > 0 || evidence.external > 0) {
    return "2 5";
  }
  if (evidence.inferred > 0) return "8 5";
  return undefined;
}

function parallelPositions(edges: RepositoryTopologyEdge[]): Map<string, { index: number; count: number }> {
  const groups = new Map<string, RepositoryTopologyEdge[]>();
  for (const edge of edges) {
    const key = `${edge.source}\u0000${edge.target}`;
    const group = groups.get(key) ?? [];
    group.push(edge);
    groups.set(key, group);
  }

  const result = new Map<string, { index: number; count: number }>();
  for (const group of groups.values()) {
    group.sort((left, right) => left.id.localeCompare(right.id));
    group.forEach((edge, index) => result.set(edge.id, { index, count: group.length }));
  }
  return result;
}

export function layoutRepositoryTopology(
  inputNodes: RepositoryTopologyNode[],
  inputEdges: RepositoryTopologyEdge[],
  rankdir: RepositoryTopologyDirection,
): RepositoryTopologyLayout {
  const nodes = [...inputNodes].sort((left, right) => left.id.localeCompare(right.id));
  const nodeIds = new Set(nodes.map((node) => node.id));
  const edges = [...inputEdges]
    .filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target))
    .sort((left, right) => left.id.localeCompare(right.id));

  const graph = new dagre.graphlib.Graph({ multigraph: true });
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({ rankdir, ranksep: 96, nodesep: 56, edgesep: 28, marginx: 24, marginy: 24 });

  for (const node of nodes) {
    graph.setNode(node.id, { width: REPOSITORY_NODE_WIDTH, height: REPOSITORY_NODE_HEIGHT });
  }
  for (const edge of edges) {
    graph.setEdge(edge.source, edge.target, {}, edge.id);
  }
  dagre.layout(graph);

  const horizontal = rankdir === "LR";
  const sourcePosition = horizontal ? Position.Right : Position.Bottom;
  const targetPosition = horizontal ? Position.Left : Position.Top;
  const positions = parallelPositions(edges);

  return {
    nodes: nodes.map((repository) => {
      const position = graph.node(repository.id);
      return {
        id: repository.id,
        type: REPOSITORY_NODE_TYPE,
        position: {
          x: position.x - REPOSITORY_NODE_WIDTH / 2,
          y: position.y - REPOSITORY_NODE_HEIGHT / 2,
        },
        sourcePosition,
        targetPosition,
        width: REPOSITORY_NODE_WIDTH,
        height: REPOSITORY_NODE_HEIGHT,
        draggable: false,
        connectable: false,
        selectable: true,
        ariaLabel: `Repository ${repository.label || repository.repository}`,
        data: { repository },
      };
    }),
    edges: edges.map((relationship) => {
      const parallel = positions.get(relationship.id) ?? { index: 0, count: 1 };
      const color = repositoryProtocolColor(relationship.channel);
      return {
        id: relationship.id,
        type: REPOSITORY_EDGE_TYPE,
        source: relationship.source,
        target: relationship.target,
        selectable: true,
        focusable: true,
        animated: false,
        ariaLabel: `${relationship.channel} from ${relationship.source} to ${relationship.target}, ${relationship.relationship_count} relationships`,
        markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
        style: {
          stroke: color,
          strokeWidth: repositoryEdgeWidth(relationship.relationship_count),
          strokeDasharray: repositoryEdgeDash(relationship.evidence),
        },
        data: {
          relationship,
          parallelIndex: parallel.index,
          parallelCount: parallel.count,
        },
      };
    }),
  };
}
