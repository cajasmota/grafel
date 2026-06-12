/* ============================================================
   components/compound-topology/UnifiedTopology.tsx

   Model 3 of the compound-topology epic (#4810) — the "unified infra+code
   architecture diagram". A SINGLE compound canvas that interleaves infra
   resources AND code modules/services together (vs Model 2's two side-by-side
   lenses).

   How the unification is real (no synthesised links):

     - We render the INFRA lens payload (group_by=infra). That payload already
       places EVERY entity — IaC resources and the code that uses them — inside
       ONE infra containment hierarchy (cloud → network → service, with code
       nested where it belongs). So the interleaving is already in the graph; we
       draw it as a single picture instead of two.

     - Each leaf node is classified infra vs code (unify.ts, derived from
       kind→tier) and rendered with a distinct glyph/shape so the two layers are
       legible — the TEAM-reference architecture-diagram style.

     - The typed usage edges that CROSS the code↔infra boundary (service →
       writes → queue, module → reads → table) are the REAL cross-links Model 2
       surfaced. Here they're drawn with emphasis so you SEE the wiring.

   Reuses the Model-1/2 foundation untouched: the ELK compound layout, #4866
   zone styling, #4887 centered handles, and the CTNode/CTZone/CTEdge renderers.

   Limitation (honest): backend DEPLOYS/RUNS_ON deployment edges are missing
   (#4983), so the only cross-layer links shown are the real usage edges
   (reads/writes/invokes/consumes/routes/depends). We never fabricate deployment
   placement — when there are no cross-boundary edges the legend says so.
   ============================================================ */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Boxes, Database, Box, Link2, AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import { useCompoundTopology } from "@/hooks/use-topology";
import {
  layoutCompoundTopology,
  layoutCompoundTopologyElk,
  defaultLayoutEngine,
  CT_NODE_TYPE,
  CT_ZONE_TYPE,
  CT_EDGE_TYPE,
  type CTLayoutResult,
} from "./layout";
import { classifyNodes, isCrossBoundary, unifiedStats } from "./unify";
import { CTNode } from "./CTNode";
import { CTZone } from "./CTZone";
import { CTEdge } from "./CTEdge";

const nodeTypes: NodeTypes = {
  [CT_NODE_TYPE]: CTNode,
  [CT_ZONE_TYPE]: CTZone,
};
const edgeTypes: EdgeTypes = { [CT_EDGE_TYPE]: CTEdge };

interface UnifiedTopologyProps {
  groupId: string;
  className?: string;
}

function UnifiedTopologyInner({ groupId, className }: UnifiedTopologyProps) {
  // Per-zone collapse set.
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const onToggle = useCallback((zoneId: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(zoneId)) next.delete(zoneId);
      else next.add(zoneId);
      return next;
    });
  }, []);

  // The infra lens already interleaves code + infra in one containment tree.
  const { data, isLoading, isError } = useCompoundTopology(groupId, "infra");

  const engine = useMemo(() => defaultLayoutEngine(), []);
  const EMPTY: CTLayoutResult = useMemo(
    () => ({ nodes: [], edges: [], capped: false, summaryEdgeCount: 0 }),
    [],
  );

  const dagreResult = useMemo(
    () => (engine === "dagre" ? layoutCompoundTopology(data, collapsed, onToggle) : EMPTY),
    [engine, data, collapsed, onToggle, EMPTY],
  );

  const [elkResult, setElkResult] = useState<CTLayoutResult>(EMPTY);
  const [elkLaidOut, setElkLaidOut] = useState(false);
  const elkRunId = useRef(0);
  useEffect(() => {
    if (engine !== "elk") return;
    const myRun = ++elkRunId.current;
    let cancelled = false;
    setElkLaidOut(false);
    layoutCompoundTopologyElk(data, collapsed, onToggle)
      .then((res) => {
        if (cancelled || myRun !== elkRunId.current) return;
        setElkResult(res);
        setElkLaidOut(true);
      })
      .catch(() => {
        if (cancelled || myRun !== elkRunId.current) return;
        setElkResult(layoutCompoundTopology(data, collapsed, onToggle));
        setElkLaidOut(true);
      });
    return () => {
      cancelled = true;
    };
  }, [engine, data, collapsed, onToggle]);

  const base = engine === "dagre" ? dagreResult : elkResult;
  const layingOut = engine === "elk" && !elkLaidOut;

  // Classify the WHOLE node set (independent of collapse) so stats + edge
  // boundary detection are stable.
  const classes = useMemo(() => classifyNodes(data?.nodes), [data]);
  const stats = useMemo(() => unifiedStats(data?.nodes, data?.edges), [data]);

  // Stamp the unified node class onto each rendered leaf node so CTNode draws
  // the infra/code distinction. Zone boxes are untouched. Positions untouched.
  const nodes: Node[] = useMemo(() => {
    return base.nodes.map((n) => {
      if (n.type !== CT_NODE_TYPE) return n;
      return { ...n, data: { ...n.data, nodeClass: classes.get(n.id) } };
    });
  }, [base.nodes, classes]);

  // Stamp the cross-boundary flag onto each rendered edge so CTEdge emphasises
  // real code↔infra usage links. A summary edge (collapsed zone) endpoint is a
  // zone id, not a node id — those won't be in `classes`, so they read as
  // intra-layer, which is correct (we can't assert a boundary we can't see).
  const edges: Edge[] = useMemo(() => {
    return base.edges.map((e) => {
      const cross = isCrossBoundary(
        { source: e.source, target: e.target },
        classes,
      );
      return { ...e, data: { ...e.data, crossBoundary: cross } };
    });
  }, [base.edges, classes]);

  const noCrossLinks = !layingOut && nodes.length > 0 && stats.crossEdges === 0;

  return (
    <div className={cn("flex h-full min-h-0 flex-col", className)}>
      {/* Header / banner */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2 text-xs">
        <Boxes size={13} className="text-accent" />
        <span className="font-medium text-text-2">Unified architecture</span>
        <span className="text-text-4">
          infra resources and the code that uses them, in one diagram
        </span>
        <div className="ml-auto flex items-center gap-3 text-text-4 tabular-nums">
          <span className="inline-flex items-center gap-1">
            <Database size={11} className="text-success" /> {stats.infraNodes} infra
          </span>
          <span className="inline-flex items-center gap-1">
            <Box size={11} className="text-accent" /> {stats.codeNodes} code
          </span>
          <span className="inline-flex items-center gap-1">
            <Link2 size={11} className="text-info" /> {stats.crossEdges} code↔infra link
            {stats.crossEdges === 1 ? "" : "s"}
          </span>
        </div>
      </div>

      {/* Canvas */}
      <div className="relative min-h-0 flex-1">
        {isLoading || layingOut ? (
          <div className="flex h-full items-center justify-center text-sm text-text-4">
            {isLoading ? "Loading…" : "Laying out…"}
          </div>
        ) : isError ? (
          <div className="flex h-full flex-col items-center justify-center gap-1 text-center">
            <p className="text-md font-semibold text-text">Couldn&apos;t load topology</p>
            <p className="text-sm text-text-3">Make sure the daemon is running.</p>
          </div>
        ) : nodes.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-text-4">
            No architecturally-significant nodes.
          </div>
        ) : (
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            fitView
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            proOptions={{ hideAttribution: true }}
            minZoom={0.08}
            fitViewOptions={{ padding: 0.16 }}
          >
            <Background gap={18} size={1} color="var(--border)" />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable className="!border !border-border !bg-surface" />
          </ReactFlow>
        )}
      </div>

      {/* Legend */}
      <div className="flex flex-wrap items-center gap-3 border-t border-border bg-surface px-3 py-1.5 text-[10px] text-text-4">
        <span className="inline-flex items-center gap-1">
          <Database size={11} className="text-success" /> infra resource (datastore · queue · function · network…)
        </span>
        <span className="inline-flex items-center gap-1">
          <Box size={11} className="text-accent" /> code (module · service · endpoint…)
        </span>
        <span className="inline-flex items-center gap-1">
          <span className="inline-block h-0.5 w-4" style={{ background: "var(--info)" }} />
          code↔infra usage edge (reads/writes/invokes…) — real graph data, none inferred
        </span>
        {noCrossLinks ? (
          <span className="inline-flex items-center gap-1 text-warning">
            <AlertTriangle size={11} />
            no code↔infra usage edges in this graph; deployment links (DEPLOYS/RUNS_ON) are not yet extracted (#4983)
          </span>
        ) : (
          <span className="text-text-4">
            deployment links (DEPLOYS/RUNS_ON) not yet extracted (#4983) — only real usage edges are drawn
          </span>
        )}
      </div>
    </div>
  );
}

/** Model-3 unified infra+code architecture diagram, wrapped in a provider. */
export function UnifiedTopology(props: UnifiedTopologyProps) {
  return (
    <ReactFlowProvider>
      <UnifiedTopologyInner {...props} />
    </ReactFlowProvider>
  );
}
