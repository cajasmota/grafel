/* ============================================================
   components/iac-diagram/IaCDiagram.tsx — IaC architecture diagram (#4526).

   Renders the resolved IaC resource graph (IaCReport) as a cloud-architecture
   diagram with React Flow + dagre:

     - Nodes  = IaC resources, colored + iconed by resource_category.
     - Edges  = DEPENDS_ON / USES relations (grant / event-source / dependency /
                topology), directional, styled by facet.
     - Groups = module containers — resources sharing a `module` cluster into a
                labelled box, so a modularized Terraform / CDK stack reads as
                grouped boxes. This works across modularized IaC because
                archigraph flattens modules to the resolved resource graph.

   Controls: H/V layout toggle, zoom/fit (React Flow Controls), MiniMap. Click a
   resource node → its source peek (file:line) via the node renderer. Unresolved
   relation targets (#4495) are not drawn as dead edges; they surface as a count
   chip on the owning node and in the legend footer.
   ============================================================ */

import { useMemo, useState } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  type NodeTypes,
  type EdgeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { ArrowRight, ArrowDown, Unlink } from "lucide-react";
import { cn } from "@/lib/utils";
import type { IaCReport } from "@/data/types";
import {
  layoutIaCDiagram,
  IAC_NODE_TYPE,
  IAC_GROUP_TYPE,
  IAC_EDGE_TYPE,
  MAX_DIAGRAM_NODES,
  type IaCDiagramDirection,
} from "./layout";
import { IaCNode } from "./IaCNode";
import { IaCGroupNode } from "./IaCGroupNode";
import { IaCEdge } from "./IaCEdge";
import { categoryStyle } from "./categoryStyle";

const nodeTypes: NodeTypes = {
  [IAC_NODE_TYPE]: IaCNode,
  [IAC_GROUP_TYPE]: IaCGroupNode,
};
const edgeTypes: EdgeTypes = { [IAC_EDGE_TYPE]: IaCEdge };

// Legend swatches — the categories that actually carry distinct colors.
const LEGEND_CATEGORIES = [
  "compute",
  "datastore",
  "queue",
  "function",
  "network",
  "secret",
  "other",
];

interface IaCDiagramProps {
  report: IaCReport;
  className?: string;
}

function IaCDiagramInner({ report, className }: IaCDiagramProps) {
  const [direction, setDirection] = useState<IaCDiagramDirection>("LR");

  const { nodes, edges, capped, unresolvedEdges } = useMemo(
    () => layoutIaCDiagram(report, direction),
    [report, direction],
  );

  const moduleCount = useMemo(
    () => nodes.filter((n) => n.type === IAC_GROUP_TYPE).length,
    [nodes],
  );
  const resourceCount = nodes.length - moduleCount;

  return (
    <div className={cn("flex h-full min-h-0 flex-col", className)}>
      {/* Controls bar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2">
        <div className="inline-flex overflow-hidden rounded-md border border-border">
          <button
            type="button"
            onClick={() => setDirection("LR")}
            className={cn(
              "inline-flex h-7 items-center gap-1 px-2 text-xs transition-colors",
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
              "inline-flex h-7 items-center gap-1 border-l border-border px-2 text-xs transition-colors",
              direction === "TB" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
            )}
            title="Vertical layout (top → bottom)"
          >
            <ArrowDown size={12} /> V
          </button>
        </div>

        <span className="text-xs text-text-4 tabular-nums">
          {resourceCount} {resourceCount === 1 ? "resource" : "resources"}
          {" · "}
          {moduleCount} {moduleCount === 1 ? "module" : "modules"}
        </span>

        {capped && (
          <span className="text-xs text-warning" title={`Only the first ${MAX_DIAGRAM_NODES} resources are drawn.`}>
            showing first {MAX_DIAGRAM_NODES}
          </span>
        )}

        {/* Category legend */}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {LEGEND_CATEGORIES.map((cat) => {
            const s = categoryStyle(cat);
            const Icon = s.Icon;
            return (
              <span key={cat} className="inline-flex items-center gap-1 text-[10px] text-text-4" title={s.label}>
                <span
                  className="inline-flex size-3.5 items-center justify-center rounded"
                  style={{ color: s.color, background: s.tint }}
                >
                  <Icon size={9} />
                </span>
                <span className="uppercase">{cat}</span>
              </span>
            );
          })}
        </div>
      </div>

      {/* Canvas */}
      <div className="relative min-h-0 flex-1">
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
          minZoom={0.1}
          fitViewOptions={{ padding: 0.16 }}
        >
          <Background gap={18} size={1} color="var(--border)" />
          <Controls showInteractive={false} />
          <MiniMap pannable zoomable className="!border !border-border !bg-surface" />
        </ReactFlow>
      </div>

      {/* Footer: unresolved-target honesty note (#4495). */}
      {unresolvedEdges > 0 && (
        <div className="flex items-center gap-1.5 border-t border-border bg-surface px-3 py-1.5 text-[11px] text-text-4">
          <Unlink size={11} />
          {unresolvedEdges} relation {unresolvedEdges === 1 ? "target" : "targets"} could
          not be resolved to a rendered resource and {unresolvedEdges === 1 ? "is" : "are"} not
          drawn as edges (shown as a chip on the owning node).
        </div>
      )}
    </div>
  );
}

/** IaCDiagram — wraps the inner view in a ReactFlowProvider. */
export function IaCDiagram(props: IaCDiagramProps) {
  return (
    <ReactFlowProvider>
      <IaCDiagramInner {...props} />
    </ReactFlowProvider>
  );
}
