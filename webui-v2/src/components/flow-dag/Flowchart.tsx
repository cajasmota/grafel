/* ============================================================
   components/flow-dag/Flowchart.tsx — the Flowchart VIEW of the Downstream-flow
   modal (#4819, control-flow epic #4820).

   Where <FlowDag> renders the endpoint's downstream CALL TREE, <Flowchart>
   renders the on-demand CONTROL-FLOW GRAPH (CFG) of the endpoint's HANDLER
   function as a classic flowchart: rounded start/end terminals, diamond decision
   nodes (carrying the condition text), rectangle process steps (badged with
   their effects at the +Data / Full detail levels), loop-back edges, and labelled
   branch_true/false / return / throw exit edges.

   It REUSES the same flow canvas + ELK orthogonal layout the tree view uses
   (`layoutWithElk` from lib/elk-layout, the shared `layered` backend), with
   flowchart-specific node shapes (FlowchartNode) and edge labels (FlowchartEdge).

   Data source: GET /api/v2/groups/:id/paths/:hash/control-flow (#4819 backend),
   parameterised by the `detail` slider. When the backend reports
   `supported=false` (a language without a CFG block detector, or an
   unresolved/unreadable handler) we render a graceful "flowchart not available"
   state instead of a degenerate diagram.

   The View toggle (Tree | Flowchart) and the Detail slider live in the parent
   modal (paths.tsx); this component is driven purely by (detail, direction) +
   its own fetch, so the Tree view keeps its own controls untouched.
   ============================================================ */

import { useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  ReactFlowProvider,
  Position,
  MarkerType,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  ArrowRight,
  ArrowDown,
  Loader2,
  AlertTriangle,
  GitBranch,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  layoutWithElk,
  type ElkLayoutNode,
  type ElkLayoutEdge,
} from "@/lib/elk-layout";
import { useControlFlow } from "@/hooks/use-paths";
import type {
  ControlFlowDetail,
  ControlFlowResponse,
} from "@/data/types";
import type { FlowDagDirection } from "./layout";
import { FlowchartNode, type FlowchartNodeData } from "./FlowchartNode";
import { FlowchartEdge, type FlowchartEdgeData } from "./FlowchartEdge";
import { boxFor, defaultCaption } from "./flowchart-shapes";

const FLOWCHART_NODE_TYPE = "flowchart";
const FLOWCHART_EDGE_TYPE = "flowchart";

const nodeTypes: NodeTypes = { [FLOWCHART_NODE_TYPE]: FlowchartNode };
const edgeTypes: EdgeTypes = { [FLOWCHART_EDGE_TYPE]: FlowchartEdge };

function handlePositions(direction: FlowDagDirection): {
  source: Position;
  target: Position;
} {
  return direction === "LR"
    ? { source: Position.Right, target: Position.Left }
    : { source: Position.Bottom, target: Position.Top };
}

export interface FlowchartProps {
  groupId: string;
  pathHash?: string | null;
  verb?: string;
  detail: ControlFlowDetail;
  /** Whether the internal fetch is enabled (only when modal open + view active). */
  enabled?: boolean;
  className?: string;
}

interface LaidOut {
  nodes: RFNode<FlowchartNodeData>[];
  edges: RFEdge<FlowchartEdgeData>[];
}

const EMPTY: LaidOut = { nodes: [], edges: [] };

function FlowchartInner({
  groupId,
  pathHash,
  verb,
  detail,
  enabled = true,
  className,
}: FlowchartProps) {
  const [direction, setDirection] = useState<FlowDagDirection>("TB");

  const query = useControlFlow(groupId, pathHash ?? null, detail, verb, enabled);
  const data: ControlFlowResponse | undefined = query.data;
  const isLoading = query.isLoading;
  const error = query.error;

  // Build the React Flow nodes/edges (data only) from the CFG payload, then run
  // ELK to position them with orthogonal routing — same engine as the tree view.
  const built = useMemo(() => {
    if (!data || data.nodes.length === 0) return null;
    const handles = handlePositions(direction);
    const nodes: RFNode<FlowchartNodeData>[] = data.nodes.map((n) => {
      const box = boxFor(n.shape);
      return {
        id: n.id,
        type: FLOWCHART_NODE_TYPE,
        position: { x: 0, y: 0 },
        width: box.w,
        height: box.h,
        sourcePosition: handles.source,
        targetPosition: handles.target,
        data: {
          shape: n.shape,
          caption: defaultCaption(n),
          condition: n.condition,
          effects: n.effects,
          line: n.line,
          sourcePos: handles.source,
          targetPos: handles.target,
        },
      };
    });
    const edges: RFEdge<FlowchartEdgeData>[] = data.edges.map((e, i) => ({
      id: `cfe_${i}_${e.from}_${e.to}_${e.kind}`,
      source: e.from,
      target: e.to,
      type: FLOWCHART_EDGE_TYPE,
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
      data: { kind: e.kind },
    }));
    return { nodes, edges };
  }, [data, direction]);

  const [laidOut, setLaidOut] = useState<LaidOut>(EMPTY);
  const [layingOut, setLayingOut] = useState(false);
  const runId = useRef(0);

  useEffect(() => {
    if (!built) {
      setLaidOut(EMPTY);
      setLayingOut(false);
      return;
    }
    const myRun = ++runId.current;
    let cancelled = false;
    setLayingOut(true);

    const elkNodes: ElkLayoutNode[] = built.nodes.map((n) => ({
      id: n.id,
      width: n.width,
      height: n.height,
    }));
    const elkEdges: ElkLayoutEdge[] = built.edges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
    }));

    layoutWithElk(elkNodes, elkEdges, {
      direction: direction === "LR" ? "RIGHT" : "DOWN",
      algorithm: "layered",
      edgeRouting: "ORTHOGONAL",
      nodeSpacing: 34,
      layerSpacing: 64,
      // #4874: centered leading/trailing-face ports so ELK's routed endpoints
      // line up with the direction-aware centered handles (no side-escaping).
      centeredPorts: true,
    })
      .then(({ nodes: positions, edges: routes }) => {
        if (cancelled || myRun !== runId.current) return;
        const positioned = built.nodes.map((n) => {
          const p = positions.get(n.id);
          return p ? { ...n, position: { x: p.x, y: p.y } } : n;
        });
        const routed = built.edges.map((e) => {
          const route = routes.get(e.id);
          if (route && route.points.length >= 2) {
            return { ...e, data: { ...(e.data as FlowchartEdgeData), elkPoints: route.points } };
          }
          return e;
        });
        setLaidOut({ nodes: positioned, edges: routed });
        setLayingOut(false);
      })
      .catch(() => {
        if (cancelled || myRun !== runId.current) return;
        // ELK failure → render with un-positioned (0,0) nodes rather than blank;
        // smooth-step edges still connect them.
        setLaidOut({ nodes: built.nodes, edges: built.edges });
        setLayingOut(false);
      });

    return () => {
      cancelled = true;
    };
  }, [built, direction]);

  const unsupported = data && !data.supported;

  return (
    <div className={cn("flex flex-col h-full min-h-0", className)}>
      {/* Controls bar — orientation toggle + handler/complexity label. The View
          toggle + Detail slider live in the parent modal. */}
      <div className="flex flex-wrap items-center gap-2 px-3 py-2 border-b border-border bg-surface">
        <div className="inline-flex rounded-md border border-border overflow-hidden">
          <button
            type="button"
            onClick={() => setDirection("TB")}
            className={cn(
              "inline-flex items-center gap-1 h-7 px-2 text-xs transition-colors",
              direction === "TB" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Vertical layout (top → bottom) — classic flowchart"
          >
            <ArrowDown size={12} /> V
          </button>
          <button
            type="button"
            onClick={() => setDirection("LR")}
            className={cn(
              "inline-flex items-center gap-1 h-7 px-2 text-xs transition-colors border-l border-border",
              direction === "LR" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Horizontal layout (left → right)"
          >
            <ArrowRight size={12} /> H
          </button>
        </div>

        {(isLoading || layingOut) && (
          <span className="inline-flex items-center gap-1 text-xs text-text-4">
            <Loader2 size={12} className="animate-spin" />
            {isLoading ? "loading…" : "laying out…"}
          </span>
        )}

        {data && data.supported && (
          <span
            className="inline-flex items-center gap-1 text-xs text-text-3"
            title="McCabe cyclomatic complexity of the handler"
          >
            <GitBranch size={12} className="text-text-4" />
            <span className="font-semibold text-text-2 tabular-nums">{data.cyclomatic_complexity}</span>
            <span className="text-text-4">complexity</span>
          </span>
        )}

        {data?.handler && (
          <span
            className="ml-auto inline-flex items-center gap-1.5 text-xs text-text-3 font-mono truncate max-w-[45%]"
            title={`${data.handler.name}${data.handler.file ? ` — ${data.handler.file}:${data.handler.line ?? ""}` : ""}`}
          >
            <span className="font-semibold text-text-2 truncate">{data.handler.name}</span>
          </span>
        )}
      </div>

      {/* Canvas */}
      <div className="relative flex-1 min-h-0">
        {error ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-text-3">
            <AlertTriangle size={20} className="text-[var(--danger)]" />
            <p className="text-sm">Couldn't load the flowchart.</p>
            <p className="text-xs text-text-4">{error instanceof Error ? error.message : "Unknown error"}</p>
          </div>
        ) : unsupported ? (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-center px-6 text-text-3">
            <GitBranch size={20} className="text-text-4" />
            <p className="text-sm">Flowchart not available for this handler.</p>
            <p className="text-xs text-text-4 max-w-[480px]">
              {data?.note ||
                (data?.language
                  ? `Control-flow extraction isn't supported for ${data.language} yet.`
                  : "The handler could not be resolved for this endpoint.")}
            </p>
          </div>
        ) : data && data.nodes.length === 0 ? (
          <div className="absolute inset-0 flex items-center justify-center text-sm text-text-4">
            No control flow to show for this handler.
          </div>
        ) : (
          <ReactFlow
            nodes={laidOut.nodes}
            edges={laidOut.edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            fitView
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={false}
            proOptions={{ hideAttribution: true }}
            minZoom={0.15}
            fitViewOptions={{ padding: 0.2 }}
          >
            <Background gap={18} size={1} color="var(--border)" />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable className="!bg-surface !border !border-border" />
          </ReactFlow>
        )}
      </div>
    </div>
  );
}

/**
 * Flowchart — the control-flow / flowchart view of the Downstream-flow modal.
 * Wraps the inner view in a ReactFlowProvider so it is a drop-in sibling of
 * <FlowDag> in the modal (the parent toggles which one is mounted).
 */
export function Flowchart(props: FlowchartProps) {
  return (
    <ReactFlowProvider>
      <FlowchartInner {...props} />
    </ReactFlowProvider>
  );
}
