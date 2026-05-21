/* ============================================================
   components/graph/node-inspector.tsx — floating right-pane inspector.

   Opens on node selection (graph.md "floating overlay" mode). Lazy-fetches
   the Tier-3 entity detail (callers / callees / community) and offers
   click-to-focus on neighbors.
   ============================================================ */

import { X } from "lucide-react";
import { Badge, Tabs, TabsList, TabsTrigger, TabsContent, Button } from "@/components/ui";
import { useEntityDetail } from "@/hooks/use-graph";
import type { GraphNode } from "@/data/types";

export interface NodeInspectorProps {
  groupId: string;
  node: GraphNode;
  onClose: () => void;
  onFocusNode: (id: string) => void;
}

export function NodeInspector({ groupId, node, onClose, onFocusNode }: NodeInspectorProps) {
  const { data, isLoading } = useEntityDetail(groupId, node.id);

  const inbound = data?.inbound_edges ?? [];
  const outbound = data?.outbound_edges ?? [];
  const neighborById = new Map((data?.neighbors ?? []).map((n) => [n.id, n]));

  return (
    <aside
      role="dialog"
      aria-label={`Inspector: ${node.label}`}
      className="absolute right-4 top-4 bottom-4 z-30 flex w-[360px] flex-col rounded-lg border border-border bg-surface/90 shadow-[var(--shadow-4)] backdrop-blur-md"
    >
      <header className="flex items-start justify-between gap-2 border-b border-border p-4">
        <div className="min-w-0">
          <div className="truncate font-mono text-[15px] text-text">{node.label}</div>
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
            <Badge tone="info">{node.kind}</Badge>
            <Badge dot="var(--accent)">{node.repo}</Badge>
          </div>
        </div>
        <button onClick={onClose} aria-label="Close inspector" className="rounded-md p-1 text-text-3 hover:bg-surface-2 hover:text-text">
          <X size={16} />
        </button>
      </header>

      <div className="border-b border-border px-4 py-2 font-mono text-xs text-text-3 break-all">
        {data?.entity.source_file
          ? `${data.entity.source_file}:${data.entity.start_line}`
          : "—"}
      </div>

      <div className="grid grid-cols-3 gap-2 border-b border-border px-4 py-3 text-center">
        <Stat label="Inbound" value={data?.in_degree ?? node.degree} />
        <Stat label="PageRank" value={node.pageRank.toFixed(4)} />
        <Stat label="Community" value={data?.community_name ?? (node.communityId ?? "—")} />
      </div>

      <Tabs defaultValue="inbound" className="flex min-h-0 flex-1 flex-col">
        <TabsList className="mx-4 mt-3">
          <TabsTrigger value="inbound">Inbound</TabsTrigger>
          <TabsTrigger value="outbound">Outbound</TabsTrigger>
          <TabsTrigger value="schema">Schema</TabsTrigger>
        </TabsList>

        <div className="ag-scroll min-h-0 flex-1 p-4">
          <TabsContent value="inbound">
            <EdgeList edges={inbound} dir="from_id" neighborById={neighborById} loading={isLoading} onFocusNode={onFocusNode} />
          </TabsContent>
          <TabsContent value="outbound">
            <EdgeList edges={outbound} dir="to_id" neighborById={neighborById} loading={isLoading} onFocusNode={onFocusNode} />
          </TabsContent>
          <TabsContent value="schema">
            <p className="text-sm text-text-3">No schema metadata for this entity.</p>
          </TabsContent>
        </div>
      </Tabs>

      <footer className="flex gap-2 border-t border-border p-3">
        <Button variant="ghost" size="sm" className="flex-1" onClick={() => onFocusNode(node.id)}>
          Focus neighbors
        </Button>
      </footer>
    </aside>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <div className="font-mono text-md tabular-nums text-text">{value}</div>
      <div className="text-xs text-text-3">{label}</div>
    </div>
  );
}

function EdgeList({
  edges,
  dir,
  neighborById,
  loading,
  onFocusNode,
}: {
  edges: { from_id: string; to_id: string; kind: string }[];
  dir: "from_id" | "to_id";
  neighborById: Map<string, { label: string }>;
  loading: boolean;
  onFocusNode: (id: string) => void;
}) {
  if (loading) return <p className="text-sm text-text-3">Loading…</p>;
  if (edges.length === 0) return <p className="text-sm text-text-3">No edges.</p>;
  const shown = edges.slice(0, 25);
  return (
    <ul className="space-y-1.5">
      {shown.map((e, i) => {
        const id = e[dir];
        const label = neighborById.get(id)?.label ?? id;
        return (
          <li key={`${id}-${i}`}>
            <button
              onClick={() => onFocusNode(id)}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-surface-2"
            >
              <Badge tone="neutral" className="shrink-0">{e.kind}</Badge>
              <span className="truncate font-mono text-sm text-text-2">{label}</span>
            </button>
          </li>
        );
      })}
      {edges.length > shown.length ? (
        <li className="px-2 text-xs text-text-3">+{edges.length - shown.length} more</li>
      ) : null}
    </ul>
  );
}
