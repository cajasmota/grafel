/* ============================================================
   components/iac-diagram/IaCGroupNode.tsx — module container (#4526).

   A non-interactive container that visually clusters the resources of one
   module / construct / stack. This is the key affordance for modularized IaC:
   archigraph flattens modules to the resolved resource graph, so grouping the
   flattened resources back by `module` reconstructs the stack structure as
   labelled boxes. Header shows the short module label + the resource count;
   the full module path is on hover.
   ============================================================ */

import { memo } from "react";
import { Layers } from "lucide-react";
import type { NodeProps } from "@xyflow/react";
import type { IaCGroupData } from "./layout";

function IaCGroupNodeImpl({ data }: NodeProps) {
  const { module, shortLabel, count } = data as IaCGroupData;
  return (
    <div
      className="h-full w-full rounded-lg border border-dashed border-border bg-surface-2/40"
      title={module}
    >
      <div className="flex items-center gap-1.5 px-2.5 pt-1.5 text-text-3">
        <Layers size={11} className="shrink-0 text-text-4" />
        <span className="truncate font-mono text-[10px] font-medium" title={module}>
          {shortLabel}
        </span>
        <span className="ml-auto shrink-0 text-[10px] tabular-nums text-text-4">{count}</span>
      </div>
    </div>
  );
}

export const IaCGroupNode = memo(IaCGroupNodeImpl);
