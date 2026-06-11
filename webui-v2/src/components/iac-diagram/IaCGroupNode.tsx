/* ============================================================
   components/iac-diagram/IaCGroupNode.tsx — module container (#4526).

   A non-interactive container that visually clusters the resources of one
   module / construct / stack. This is the key affordance for modularized IaC:
   archigraph flattens modules to the resolved resource graph, so grouping the
   flattened resources back by `module` reconstructs the stack structure as
   labelled boxes. Header shows the short module label + the resource count;
   the full module path is on hover.

   #4866 — the container box now reads clearly as a grouping frame: a solid,
   higher-contrast border (heavier than the surrounding resource cards), a faint
   per-category background tint, and a prominent header chip. Nesting depth is
   distinguished by a stronger tint/border at outer levels. All colors come from
   the theme tone palette (categoryStyle / CSS vars) so light + dark both track.
   ============================================================ */

import { memo } from "react";
import type { NodeProps } from "@xyflow/react";
import type { IaCGroupData } from "./layout";
import { categoryStyle } from "./categoryStyle";

function IaCGroupNodeImpl({ data }: NodeProps) {
  const { module, shortLabel, count, categoryKey, depth } = data as IaCGroupData;
  const style = categoryStyle(categoryKey);
  const Icon = style.Icon;

  // Outer containers get a marginally stronger frame so a nested box stays
  // legible inside its parent (#4866). depth 0 = outermost.
  const outer = (depth ?? 0) === 0;
  // Faint fill distinct from the canvas, tinted by the dominant category.
  const fill = `color-mix(in srgb, ${style.color} ${outer ? 7 : 11}%, transparent)`;
  const borderColor = `color-mix(in srgb, ${style.color} ${outer ? 55 : 70}%, var(--border-strong))`;

  return (
    <div
      className="h-full w-full rounded-lg border-2"
      style={{
        borderColor,
        backgroundColor: fill,
        boxShadow: "inset 0 0 0 1px color-mix(in srgb, var(--surface) 55%, transparent)",
      }}
      title={module}
    >
      <div
        className="m-1 flex items-center gap-1.5 rounded-md px-2 py-1 text-text-2"
        style={{ backgroundColor: `color-mix(in srgb, ${style.color} 16%, var(--surface))` }}
      >
        <Icon size={12} className="shrink-0" style={{ color: style.color }} />
        <span
          className="truncate font-mono text-[11px] font-semibold tracking-tight"
          title={module}
        >
          {shortLabel}
        </span>
        <span
          className="ml-auto shrink-0 rounded-full px-1.5 text-[10px] font-medium tabular-nums text-text-3"
          style={{ backgroundColor: "color-mix(in srgb, var(--surface-2) 80%, transparent)" }}
        >
          {count}
        </span>
      </div>
    </div>
  );
}

export const IaCGroupNode = memo(IaCGroupNodeImpl);
