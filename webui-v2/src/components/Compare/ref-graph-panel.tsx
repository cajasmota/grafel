/* ============================================================
   Compare/ref-graph-panel.tsx — mini graph panel showing one ref's
   entities in a scrollable list (PH5 / #2093).

   The cosmos.gl full canvas is too heavy for a side-by-side mini view.
   Instead this panel renders the entity list from the loaded graph data,
   highlighting any entity whose ID matches highlightedEntityId.

   When the graph is "warming" (COLD tier → loading) a spinner is shown.
   ============================================================ */

import { Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { DiffEntityEntry } from "@/data/types";

export interface RefGraphPanelProps {
  /** The ref name being displayed. */
  ref: string;
  /** All entities that exist in this ref (from the diff data). */
  entities: DiffEntityEntry[];
  /** Entity IDs that should be visually highlighted. */
  highlightedEntityIds?: Set<string>;
  /** Whether the graph for this ref is still loading (cold tier). */
  isWarm?: boolean;
  /** Label displayed at the top: "refA" or "refB". */
  label: "refA" | "refB";
  className?: string;
}

const LABEL_META = {
  refA: { text: "Before", bg: "bg-surface-3", border: "border-text-3" },
  refB: { text: "After", bg: "bg-accent-soft", border: "border-accent" },
};

export function RefGraphPanel({
  ref: refName,
  entities,
  highlightedEntityIds,
  isWarm,
  label,
  className,
}: RefGraphPanelProps) {
  const meta = LABEL_META[label];

  return (
    <div className={cn("flex flex-col h-full overflow-hidden border border-border rounded-lg", className)}>
      {/* Header */}
      <div
        className={cn(
          "px-3 py-2 border-b border-border flex items-center gap-2 shrink-0",
          meta.bg,
        )}
      >
        <span
          className={cn(
            "text-[10px] font-bold uppercase tracking-wider px-1.5 py-0.5 rounded border",
            meta.border,
            label === "refB" ? "text-accent" : "text-text-2",
          )}
        >
          {meta.text}
        </span>
        <code className="text-xs font-mono text-text-1 truncate flex-1">{refName}</code>
        {!isWarm && (
          <span className="text-[10px] text-text-3">{entities.length} entities</span>
        )}
      </div>

      {/* Body */}
      {isWarm ? (
        <div className="flex flex-col items-center justify-center h-full gap-2 text-text-3">
          <Loader2 className="size-5 animate-spin" />
          <span className="text-xs">Warming graph…</span>
        </div>
      ) : entities.length === 0 ? (
        <div className="flex items-center justify-center h-full text-text-3 text-xs">
          No entities
        </div>
      ) : (
        <ul className="flex-1 overflow-y-auto divide-y divide-border/40">
          {entities.map((e) => {
            const isHighlighted = highlightedEntityIds?.has(e.id);
            return (
              <li
                key={e.id}
                className={cn(
                  "px-3 py-1.5 transition-colors",
                  isHighlighted && "bg-accent-soft ring-1 ring-inset ring-accent",
                )}
              >
                <div className="flex items-start gap-2">
                  <span className="text-[10px] text-text-3 mt-0.5 shrink-0 w-[70px] truncate">
                    {e.kind}
                  </span>
                  <span
                    className={cn(
                      "text-xs font-mono truncate flex-1",
                      isHighlighted ? "text-accent font-medium" : "text-text-1",
                    )}
                  >
                    {e.name}
                  </span>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
