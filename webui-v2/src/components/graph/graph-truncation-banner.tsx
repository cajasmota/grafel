import type { GraphPayload } from "@/data/types";

export function GraphTruncationBanner({ data }: { data?: GraphPayload }) {
  if (!data || (!data.nodeTruncated && !data.edgeTruncated)) return null;

  return (
    <div className="rounded border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
      Bounded result: {data.nodes.length.toLocaleString()} / {data.totalNodeCount.toLocaleString()} nodes and {data.edges.length.toLocaleString()} / {(data.totalEdgeCount ?? data.edges.length).toLocaleString()} edges.
      {data.limits ? ` Caps: ${data.limits.nodeCap.toLocaleString()} nodes, ${data.limits.edgeCap.toLocaleString()} edges.` : ""}
    </div>
  );
}
