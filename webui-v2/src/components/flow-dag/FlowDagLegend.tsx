/* ============================================================
   components/flow-dag/FlowDagLegend.tsx — role + edge-kind legend.

   A compact key so the role tints and semantic edge styles are legible. Also
   surfaces the truncation flags + branch count from the payload so the user
   knows when the DAG was capped (honest truncation, never a silent drop).
   ============================================================ */

import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";
import { NODE_BUCKET_STYLE, EDGE_STYLE, type NodeBucket } from "./style";
import type { DownstreamDAGTruncation } from "@/data/types";

interface FlowDagLegendProps {
  branchCount: number;
  nodeCount: number;
  truncation: DownstreamDAGTruncation;
}

/** Human label for whichever truncation flags fired. */
function truncationLabels(t: DownstreamDAGTruncation): string[] {
  const out: string[] = [];
  if (t.depth_truncated) out.push("depth capped");
  if (t.fanout_truncated) out.push("fan-out capped");
  if (t.node_truncated) out.push("node cap hit");
  return out;
}

/**
 * Display order of the node taxonomy buckets in the legend (#4566): the spine
 * left→right, then the special-cased states (exception/external/terminal). The
 * neutral 'node' fallback is shown last.
 */
const LEGEND_BUCKETS: NodeBucket[] = [
  "endpoint",
  "handler",
  "service",
  "repository",
  "schema",
  "function",
  "collection",
  "terminal",
  "exception",
  "external",
  "node",
];

export function FlowDagLegend({ branchCount, nodeCount, truncation }: FlowDagLegendProps) {
  const trunc = truncationLabels(truncation);

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-2 text-[10px] text-text-3 border-t border-border bg-bg-soft">
      {/* Node taxonomy buckets (#4566): endpoint→…→data, plus exception (#4556,
          red), external (#4558/#4564, muted), terminal end-cap (#4561). */}
      {LEGEND_BUCKETS.map((b) => NODE_BUCKET_STYLE[b]).map((rs) => (
        <span key={rs.label} className="inline-flex items-center gap-1">
          <span
            className="size-2.5 rounded-sm shrink-0"
            style={{
              // Match the card (#4617): swatch border is an intense shade of
              // the bucket's OWN hue, not a neutral/brown frame.
              background: `color-mix(in srgb, ${rs.bg} 35%, var(--surface))`,
              border: `1px solid color-mix(in srgb, ${rs.border} 72%, var(--text))`,
            }}
          />
          {rs.label}
        </span>
      ))}

      <span className="h-3 w-px bg-border" aria-hidden />

      {/* Edge kinds */}
      {Object.values(EDGE_STYLE).map((es) => (
        <span key={es.label} className="inline-flex items-center gap-1">
          <svg width="16" height="6" viewBox="0 0 16 6" className="shrink-0">
            <line
              x1="0"
              y1="3"
              x2="16"
              y2="3"
              stroke={es.stroke}
              strokeWidth="1.75"
              strokeDasharray={es.dash}
            />
          </svg>
          {es.label}
        </span>
      ))}

      {/* Stats + truncation */}
      <span className="ml-auto inline-flex items-center gap-2 tabular-nums">
        <span>{nodeCount} nodes</span>
        <span>{branchCount} branches</span>
        {trunc.length > 0 && (
          <span
            className={cn(
              "inline-flex items-center gap-1 rounded px-1.5 py-px font-medium",
              "bg-[var(--warn-soft,var(--surface-2))] text-[var(--warn,var(--text-3))]",
            )}
            title="The DAG was capped — some downstream nodes are not shown."
          >
            <AlertTriangle size={10} />
            {trunc.join(" · ")}
          </span>
        )}
      </span>
    </div>
  );
}
