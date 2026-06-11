/* ============================================================
   components/compound-topology/CompoundTopology.tsx

   Model 1 of the compound-topology epic (#4810/#4811). Renders the indexed
   group as an AWS-architecture-diagram-style COMPOUND graph — provider-/stack-
   agnostic: nested containment zones + tier lanes + typed relationship edges,
   with collapsible zones.

     - Group by: Infra / Modules / Tier  — re-fetches the same nodes re-grouped
       server-side (the zone hierarchy changes; tier facets + edges are stable).
     - Collapsible zones — click a zone header to collapse it to a single box;
       a collapsed zone's members' cross-zone edges fold into summary edges
       (drawn thicker with a ×N count) rather than vanishing silently.

   Layout uses React Flow parent/child sub-flows positioned by a compound dagre
   pass (layout.ts). Names/zones are auto-derived — zero config.
   ============================================================ */

import { useCallback, useMemo, useState } from "react";
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
import { Cloud, Boxes, Layers, ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import { cn } from "@/lib/utils";
import type { CompoundGroupBy } from "@/data/types";
import { useCompoundTopology } from "@/hooks/use-topology";
import {
  layoutCompoundTopology,
  CT_NODE_TYPE,
  CT_ZONE_TYPE,
  CT_EDGE_TYPE,
  MAX_NODES,
  TIER_ORDER,
} from "./layout";
import { CTNode } from "./CTNode";
import { CTZone } from "./CTZone";
import { CTEdge } from "./CTEdge";
import { tierStyle } from "./tierStyle";

const nodeTypes: NodeTypes = {
  [CT_NODE_TYPE]: CTNode,
  [CT_ZONE_TYPE]: CTZone,
};
const edgeTypes: EdgeTypes = { [CT_EDGE_TYPE]: CTEdge };

const GROUP_BY_OPTIONS: { id: CompoundGroupBy; label: string; Icon: typeof Cloud }[] = [
  { id: "infra", label: "Infra", Icon: Cloud },
  { id: "modules", label: "Modules", Icon: Boxes },
  { id: "tier", label: "Tier", Icon: Layers },
];

interface CompoundTopologyProps {
  groupId: string;
  className?: string;
}

function CompoundTopologyInner({ groupId, className }: CompoundTopologyProps) {
  const [groupBy, setGroupBy] = useState<CompoundGroupBy>("modules");
  // Per-group_by collapse sets — switching lenses keeps each lens' own state.
  const [collapsedByLens, setCollapsedByLens] = useState<Record<CompoundGroupBy, Set<string>>>({
    infra: new Set(),
    modules: new Set(),
    tier: new Set(),
  });
  const collapsed = collapsedByLens[groupBy];

  const { data, isLoading, isError } = useCompoundTopology(groupId, groupBy);

  const onToggle = useCallback(
    (zoneId: string) => {
      setCollapsedByLens((prev) => {
        const next = new Set(prev[groupBy]);
        if (next.has(zoneId)) next.delete(zoneId);
        else next.add(zoneId);
        return { ...prev, [groupBy]: next };
      });
    },
    [groupBy],
  );

  const collapseAll = useCallback(() => {
    setCollapsedByLens((prev) => {
      // Collapse only root zones — their descendants are subsumed.
      const roots = (data?.zones ?? []).filter((z) => !z.parent_id).map((z) => z.id);
      return { ...prev, [groupBy]: new Set(roots) };
    });
  }, [data, groupBy]);

  const expandAll = useCallback(() => {
    setCollapsedByLens((prev) => ({ ...prev, [groupBy]: new Set() }));
  }, [groupBy]);

  const { nodes, edges, capped, summaryEdgeCount } = useMemo(
    () => layoutCompoundTopology(data, collapsed, onToggle),
    [data, collapsed, onToggle],
  );

  const zoneCount = useMemo(
    () => nodes.filter((n) => n.type === CT_ZONE_TYPE).length,
    [nodes],
  );
  const nodeCount = nodes.length - zoneCount;

  // Tier lanes actually present (for the legend).
  const presentTiers = useMemo(() => {
    const set = new Set((data?.nodes ?? []).map((n) => n.tier));
    return TIER_ORDER.filter((t) => set.has(t));
  }, [data]);

  return (
    <div className={cn("flex h-full min-h-0 flex-col", className)}>
      {/* Controls bar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2">
        {/* Group-by toggle */}
        <div className="inline-flex overflow-hidden rounded-md border border-border">
          {GROUP_BY_OPTIONS.map(({ id, label, Icon }, idx) => (
            <button
              key={id}
              type="button"
              onClick={() => setGroupBy(id)}
              className={cn(
                "inline-flex h-7 items-center gap-1 px-2.5 text-xs transition-colors",
                idx > 0 && "border-l border-border",
                groupBy === id ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
              )}
              title={`Group by ${label}`}
              aria-pressed={groupBy === id}
            >
              <Icon size={12} /> {label}
            </button>
          ))}
        </div>

        {/* Collapse / expand all */}
        {groupBy !== "tier" && (
          <div className="inline-flex overflow-hidden rounded-md border border-border">
            <button
              type="button"
              onClick={collapseAll}
              className="inline-flex h-7 items-center gap-1 bg-surface px-2 text-xs text-text-3 transition-colors hover:bg-surface-2"
              title="Collapse all zones"
            >
              <ChevronsDownUp size={12} /> Collapse
            </button>
            <button
              type="button"
              onClick={expandAll}
              className="inline-flex h-7 items-center gap-1 border-l border-border bg-surface px-2 text-xs text-text-3 transition-colors hover:bg-surface-2"
              title="Expand all zones"
            >
              <ChevronsUpDown size={12} /> Expand
            </button>
          </div>
        )}

        <span className="text-xs text-text-4 tabular-nums">
          {nodeCount} {nodeCount === 1 ? "node" : "nodes"}
          {groupBy !== "tier" && ` · ${zoneCount} ${zoneCount === 1 ? "zone" : "zones"}`}
          {summaryEdgeCount > 0 && ` · ${summaryEdgeCount} summary ${summaryEdgeCount === 1 ? "edge" : "edges"}`}
        </span>

        {capped && (
          <span className="text-xs text-warning" title={`Only the first ${MAX_NODES} nodes are drawn.`}>
            showing first {MAX_NODES}
          </span>
        )}

        {/* Tier legend */}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {presentTiers.map((t) => {
            const s = tierStyle(t);
            return (
              <span key={t} className="inline-flex items-center gap-1 text-[10px] text-text-4" title={s.label}>
                <span className="inline-block size-2.5 rounded-sm" style={{ background: s.color }} />
                <span>{s.label}</span>
              </span>
            );
          })}
        </div>
      </div>

      {/* Canvas */}
      <div className="relative min-h-0 flex-1">
        {isLoading ? (
          <div className="flex h-full items-center justify-center text-sm text-text-4">Loading topology…</div>
        ) : isError ? (
          <div className="flex h-full flex-col items-center justify-center gap-1 text-center">
            <p className="text-md font-semibold text-text">Couldn&apos;t load topology</p>
            <p className="text-sm text-text-3">Make sure the daemon is running.</p>
          </div>
        ) : nodes.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-text-4">
            No architecturally-significant nodes in this group.
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
    </div>
  );
}

/** CompoundTopology — wraps the inner view in a ReactFlowProvider. */
export function CompoundTopology(props: CompoundTopologyProps) {
  return (
    <ReactFlowProvider>
      <CompoundTopologyInner {...props} />
    </ReactFlowProvider>
  );
}
