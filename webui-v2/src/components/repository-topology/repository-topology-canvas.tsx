import { useEffect, useMemo, useRef } from "react";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useReactFlow,
  type EdgeTypes,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type {
  RepositoryTopologyEdge,
  RepositoryTopologyNode,
} from "@/data/types";
import {
  layoutRepositoryTopology,
  REPOSITORY_EDGE_TYPE,
  REPOSITORY_NODE_TYPE,
  type RepositoryEdgeData,
  type RepositoryNodeData,
  type RepositoryTopologyDirection,
} from "@/lib/repository-topology-layout";
import { cn } from "@/lib/utils";
import { RepositoryEdge } from "./repository-edge";
import { RepositoryNode } from "./repository-node";

const nodeTypes: NodeTypes = { [REPOSITORY_NODE_TYPE]: RepositoryNode };
const edgeTypes: EdgeTypes = { [REPOSITORY_EDGE_TYPE]: RepositoryEdge };

export interface RepositoryTopologyCanvasProps {
  nodes: RepositoryTopologyNode[];
  edges: RepositoryTopologyEdge[];
  rankdir?: RepositoryTopologyDirection;
  selectedRepositoryId?: string;
  selectedEdgeId?: string;
  onRepositorySelect?: (repository: RepositoryTopologyNode) => void;
  onEdgeSelect?: (edge: RepositoryTopologyEdge) => void;
  className?: string;
}

function RepositoryTopologyCanvasInner({
  nodes: inputNodes,
  edges: inputEdges,
  rankdir = "LR",
  selectedRepositoryId,
  selectedEdgeId,
  onRepositorySelect,
  onEdgeSelect,
  className,
}: RepositoryTopologyCanvasProps) {
  const { fitView } = useReactFlow();
  const nodesInitialized = useNodesInitialized();
  const fittedSignature = useRef("");
  const layout = useMemo(
    () => layoutRepositoryTopology(inputNodes, inputEdges, rankdir),
    [inputNodes, inputEdges, rankdir],
  );
  const signature = useMemo(
    () => `${rankdir}:${layout.nodes.map((node) => node.id).join(",")}:${layout.edges.map((edge) => edge.id).join(",")}`,
    [layout.edges, layout.nodes, rankdir],
  );
  const nodes = useMemo(
    () => layout.nodes.map((node) => ({
      ...node,
      selected: node.id === selectedRepositoryId,
    })),
    [layout.nodes, selectedRepositoryId],
  );
  const edges = useMemo(
    () => layout.edges.map((edge) => ({
      ...edge,
      selected: edge.id === selectedEdgeId,
    })),
    [layout.edges, selectedEdgeId],
  );

  useEffect(() => {
    if (!nodesInitialized || fittedSignature.current === signature || nodes.length === 0) return;
    fittedSignature.current = signature;
    const frame = requestAnimationFrame(() => {
      void fitView({ padding: 0.18, duration: 250, maxZoom: 1.25 });
    });
    return () => cancelAnimationFrame(frame);
  }, [fitView, nodes.length, nodesInitialized, signature]);

  return (
    <div className={cn("h-full min-h-[420px] w-full overflow-hidden rounded-lg border border-border bg-surface-0", className)}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        edgesReconnectable={false}
        elementsSelectable
        deleteKeyCode={null}
        minZoom={0.08}
        maxZoom={2}
        onNodeClick={(_, node) => onRepositorySelect?.((node.data as RepositoryNodeData).repository)}
        onEdgeClick={(_, edge) => onEdgeSelect?.((edge.data as RepositoryEdgeData).relationship)}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={24} size={1} color="var(--border)" />
        <Controls showInteractive={false} />
        <MiniMap
          pannable
          zoomable
          nodeColor="var(--accent)"
          maskColor="color-mix(in srgb, var(--surface-0) 78%, transparent)"
        />
      </ReactFlow>
    </div>
  );
}

export function RepositoryTopologyCanvas(props: RepositoryTopologyCanvasProps) {
  return (
    <ReactFlowProvider>
      <RepositoryTopologyCanvasInner {...props} />
    </ReactFlowProvider>
  );
}
