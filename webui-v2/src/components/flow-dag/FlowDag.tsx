/* ============================================================
   components/flow-dag/FlowDag.tsx — shared downstream-DAG renderer.

   A reusable React Flow view of an HTTP endpoint's DOWNSTREAM as a branching
   DAG (endpoint → handler → service → repository → pipeline, plus
   JOINS_COLLECTION / THROWS / VALIDATES side-branches). Backed by
   GET /api/v2/groups/:id/paths/:hash/downstream-dag (#4349).

   Controls:
     - H/V toggle        → tidy-tree main axis LR (horizontal) / TB (vertical).
     - depth stepper     → refetches with &depth= (clamped server-side to 1..24).
     - spine / full      → refetches with mode=spine (default; collapses
                           builder/predicate noise) / mode=full (every node).
     - expand-noise      → collapsed builder/predicate children expand inline
                           on a node, no refetch (rows ship on the payload).

   Decoupling (#4354): the component accepts EITHER a (groupId, pathHash, verb)
   triple — fetching the DAG itself via useDownstreamDAG — OR a pre-fetched
   `payload`. The future Flows-view rebuild can drive it with its own payload
   without going through the paths hook.
   ============================================================ */

import { useCallback, useMemo, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  ReactFlowProvider,
  type Node as RFNode,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  ArrowRight,
  ArrowDown,
  Minus,
  Plus,
  Loader2,
  AlertTriangle,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useDownstreamDAG } from "@/hooks/use-paths";
import type { DownstreamDAGNode, DownstreamDAGResponse } from "@/data/types";
import {
  unfoldTree,
  layoutTree,
  MAX_TREE_NODES,
  FLOW_DAG_NODE_TYPE,
  FLOW_DAG_EDGE_TYPE,
  type FlowDagDirection,
  type FlowDagNodeData,
} from "./layout";
import { routeInstanceIds } from "./route";
import { FlowDagNode } from "./FlowDagNode";
import { FlowDagEdge } from "./FlowDagEdge";
import { FlowDagLegend } from "./FlowDagLegend";

const nodeTypes: NodeTypes = { [FLOW_DAG_NODE_TYPE]: FlowDagNode };
const edgeTypes: EdgeTypes = { [FLOW_DAG_EDGE_TYPE]: FlowDagEdge };

const DEPTH_MIN = 1;
const DEPTH_MAX = 24;
const DEPTH_DEFAULT = 8;

export interface FlowDagProps {
  groupId: string;
  /** Path hash to root the DAG at. Ignored when `payload` is supplied. */
  pathHash?: string | null;
  /** Disambiguate when a path maps to several verb endpoints. */
  verb?: string;
  /** Pre-fetched payload — bypasses the internal fetch (Flows reuse, #4354). */
  payload?: DownstreamDAGResponse;
  /** Whether the internal fetch is enabled (e.g. only when a modal is open). */
  enabled?: boolean;
  /**
   * Notified when a node is clicked, with its underlying DAG node. Lets a
   * caller open a side inspector (Flows view, #4354) without forking the
   * renderer. Omit for a purely read-only canvas (Paths modal).
   */
  onNodeClick?: (node: DownstreamDAGNode) => void;
  /** Node id to highlight as selected (pairs with `onNodeClick`). */
  selectedNodeId?: string | null;
  className?: string;
}

/** Inner view — assumes a ReactFlowProvider is mounted above it. */
function FlowDagInner({
  groupId,
  pathHash,
  verb,
  payload,
  enabled = true,
  onNodeClick,
  selectedNodeId,
  className,
}: FlowDagProps) {
  // Controls — fetch params. Changing mode/depth refetches (TanStack caches
  // each combination); orientation + inline-expand are pure client state.
  const [mode, setMode] = useState<"spine" | "full">("spine");
  const [depth, setDepth] = useState<number>(payload?.depth ?? DEPTH_DEFAULT);
  const [direction, setDirection] = useState<FlowDagDirection>("LR");
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  // Click-to-highlight (#4479): the focused instance id whose route is lit, or
  // null for the normal (everything-visible) view.
  const [routeFocus, setRouteFocus] = useState<string | null>(null);

  // Only fetch when no payload was injected.
  const query = useDownstreamDAG(
    groupId,
    payload ? null : pathHash ?? null,
    { mode, depth, semantic: true, verb },
    enabled && !payload,
  );

  const data: DownstreamDAGResponse | undefined = payload ?? query.data;
  const isLoading = !payload && query.isLoading;
  const error = !payload ? query.error : null;

  const onToggleExpand = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  // Unfold the deduped DAG into a pure tree (#4479). Memoized off the payload +
  // depth; instances are path-keyed so a node reached via N paths duplicates.
  const unfold = useMemo(() => {
    if (!data) return null;
    return unfoldTree(data.root_id, data.nodes, data.edges, MAX_TREE_NODES);
  }, [data]);

  // The route highlight set: ancestors + the focus + its whole forward subtree.
  const routeSet = useMemo(() => {
    if (!unfold || routeFocus == null) return null;
    return routeInstanceIds(unfold.instances, routeFocus);
  }, [unfold, routeFocus]);

  const { nodes, edges } = useMemo(() => {
    if (!unfold) return { nodes: [], edges: [] };
    const laid = layoutTree(
      unfold.instances,
      direction,
      expanded,
      onToggleExpand,
      unfold.hasOutEdge,
    );
    for (const n of laid.nodes) {
      // Selection is driven by the ORIGINAL node id so the caller's contract
      // (Flows step inspector, #4354) is unchanged across the tree unfold; all
      // instances of a selected node light up.
      if (selectedNodeId != null) {
        n.data.selected = (n.data as FlowDagNodeData).node.id === selectedNodeId;
      }
      if (routeSet) n.data.onRoute = routeSet.has(n.id);
    }
    if (routeSet) {
      for (const e of laid.edges) {
        // An edge is on the route iff BOTH its endpoints are (the tree's single
        // in-edge per node makes this exact).
        e.data = { ...e.data, kind: e.data?.kind ?? "CALLS", onRoute: routeSet.has(e.source) && routeSet.has(e.target) };
      }
    }
    return laid;
  }, [unfold, direction, expanded, onToggleExpand, selectedNodeId, routeSet]);

  const handleNodeClick = useCallback(
    (_: React.MouseEvent, n: RFNode) => {
      // Toggle the route highlight: same instance again clears it (#4479).
      setRouteFocus((prev) => (prev === n.id ? null : n.id));
      // Preserve the caller's detail/selection behavior — pass the original node.
      onNodeClick?.((n.data as FlowDagNodeData).node);
    },
    [onNodeClick],
  );

  // Clicking the empty canvas clears the route highlight (#4479).
  const handlePaneClick = useCallback(() => setRouteFocus(null), []);

  // Effective truncation: OR the payload's flags with our node-cap clip so the
  // legend stays honest when the pure-tree unfold itself hits the cap.
  const effectiveTruncation = useMemo(() => {
    if (!data) return undefined;
    return {
      ...data.truncation,
      node_truncated: data.truncation.node_truncated || (unfold?.capped ?? false),
    };
  }, [data, unfold]);

  const setDepthClamped = (n: number) =>
    setDepth(Math.max(DEPTH_MIN, Math.min(DEPTH_MAX, n)));

  return (
    <div className={cn("flex flex-col h-full min-h-0", className)}>
      {/* Controls bar */}
      <div className="flex flex-wrap items-center gap-2 px-3 py-2 border-b border-border bg-surface">
        {/* H/V toggle → tidy-tree main axis */}
        <div className="inline-flex rounded-md border border-border overflow-hidden">
          <button
            type="button"
            onClick={() => setDirection("LR")}
            className={cn(
              "inline-flex items-center gap-1 h-7 px-2 text-xs transition-colors",
              direction === "LR" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Horizontal layout (left → right)"
          >
            <ArrowRight size={12} /> H
          </button>
          <button
            type="button"
            onClick={() => setDirection("TB")}
            className={cn(
              "inline-flex items-center gap-1 h-7 px-2 text-xs transition-colors border-l border-border",
              direction === "TB" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Vertical layout (top → bottom)"
          >
            <ArrowDown size={12} /> V
          </button>
        </div>

        {/* spine / full mode */}
        <div className="inline-flex rounded-md border border-border overflow-hidden">
          <button
            type="button"
            onClick={() => setMode("spine")}
            className={cn(
              "h-7 px-2 text-xs transition-colors",
              mode === "spine" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Spine — collapse query-builder/predicate noise into owning nodes"
          >
            Spine
          </button>
          <button
            type="button"
            onClick={() => setMode("full")}
            className={cn(
              "h-7 px-2 text-xs transition-colors border-l border-border",
              mode === "full" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Full — expand every reachable node"
          >
            Full
          </button>
        </div>

        {/* depth stepper */}
        <div className="inline-flex items-center gap-1 h-7 rounded-md border border-border px-1.5 text-xs text-text-3">
          <span className="text-text-4">depth</span>
          <button
            type="button"
            onClick={() => setDepthClamped(depth - 1)}
            disabled={depth <= DEPTH_MIN}
            className="inline-flex items-center justify-center size-5 rounded hover:bg-surface-2 disabled:opacity-40 disabled:pointer-events-none"
            title="Decrease depth"
          >
            <Minus size={11} />
          </button>
          <span className="w-4 text-center tabular-nums text-text">{depth}</span>
          <button
            type="button"
            onClick={() => setDepthClamped(depth + 1)}
            disabled={depth >= DEPTH_MAX}
            className="inline-flex items-center justify-center size-5 rounded hover:bg-surface-2 disabled:opacity-40 disabled:pointer-events-none"
            title="Increase depth"
          >
            <Plus size={11} />
          </button>
        </div>

        {/* Fetch status */}
        {isLoading && (
          <span className="inline-flex items-center gap-1 text-xs text-text-4">
            <Loader2 size={12} className="animate-spin" /> loading…
          </span>
        )}

        {/* root path/verb label */}
        {data && (
          <span className="ml-auto inline-flex items-center gap-1.5 text-xs text-text-3 font-mono truncate max-w-[40%]" title={`${data.verb} ${data.path}`}>
            <span className="font-semibold text-text-2">{data.verb}</span>
            <span className="truncate">{data.path}</span>
          </span>
        )}
      </div>

      {/* Canvas */}
      <div className="relative flex-1 min-h-0">
        {error ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-text-3">
            <AlertTriangle size={20} className="text-[var(--danger)]" />
            <p className="text-sm">Couldn't load the downstream DAG.</p>
            <p className="text-xs text-text-4">{error instanceof Error ? error.message : "Unknown error"}</p>
          </div>
        ) : data && data.nodes.length === 0 ? (
          <div className="absolute inset-0 flex items-center justify-center text-sm text-text-4">
            No downstream nodes for this endpoint.
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            onNodeClick={handleNodeClick}
            onPaneClick={handlePaneClick}
            fitView
            // The graph is a read-only visualization; disable interaction edits.
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            proOptions={{ hideAttribution: true }}
            minZoom={0.15}
            // Re-fit when orientation flips so the whole DAG stays visible.
            fitViewOptions={{ padding: 0.18 }}
          >
            <Background gap={18} size={1} color="var(--border)" />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable className="!bg-surface !border !border-border" />
          </ReactFlow>
        )}
      </div>

      {/* Legend + truncation/branch stats. Node count reflects the unfolded
          tree (instances), not the deduped payload, so it matches what's drawn. */}
      {data && effectiveTruncation && (
        <FlowDagLegend
          branchCount={data.branch_count}
          nodeCount={unfold?.instances.length ?? data.nodes.length}
          truncation={effectiveTruncation}
        />
      )}
    </div>
  );
}

/**
 * FlowDag — shared downstream-DAG renderer. Wraps the inner view in a
 * ReactFlowProvider so it is drop-in anywhere (modal today, Flows view in
 * #4354) without the caller wiring provider context.
 */
export function FlowDag(props: FlowDagProps) {
  return (
    <ReactFlowProvider>
      <FlowDagInner {...props} />
    </ReactFlowProvider>
  );
}
