/* ============================================================
   components/flow-dag/FlowchartNode.tsx — custom React Flow node for the
   Flowchart view (#4819).

   Renders a CFG node with its CLASSIC flowchart glyph:
     - start / end      → rounded "terminal" pill (entry / exit)
     - return / throw    → rounded exit terminal, tinted (throw = danger)
     - decision / loop   → DIAMOND carrying the predicate/condition text
     - process           → rectangle, optionally badged with its effects

   The layout box is a fixed rectangle (ELK lays the graph out on these boxes);
   the diamond is drawn INSIDE that box as a rotated square so edges still dock to
   the box's mid-sides cleanly. Handle positions follow the layout direction
   (TB → top/bottom, LR → left/right) so connectors leave/enter the right faces.
   ============================================================ */

import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Database, Globe, FileCog, Mail, HardDrive } from "lucide-react";
import { cn } from "@/lib/utils";
import type { ControlFlowShape, ControlFlowEffect } from "@/data/types";

export interface FlowchartNodeData extends Record<string, unknown> {
  shape: ControlFlowShape;
  /** Short caption (label at full detail, else a shape-derived default). */
  caption: string;
  /** Predicate text for decision/loop nodes. */
  condition?: string;
  /** Effect annotations for process nodes (data detail+). */
  effects?: ControlFlowEffect[];
  /** 1-indexed source line, surfaced as a small corner ref. */
  line?: number;
  /** Handle faces for the active layout direction. */
  sourcePos: Position;
  targetPos: Position;
  /** #4883 — inlined-function frame id this node belongs to ("f0"=handler). */
  func?: string;
  /** #4883 — a 0-based color index for the node's frame (0 = handler, no tint). */
  frameIndex?: number;
  /** #4883 — set on the FIRST node of an inlined (non-handler) frame so the
   *  renderer can label the boundary (e.g. "↳ OrderService.fetch"). */
  frameLabel?: string;
  /** #4883 — a call node whose callee is external/library (a leaf terminal). */
  external?: boolean;
}

/** Per-frame accent colors for inlined-function boundaries (#4883). Index 0 is
 *  the handler (no tint); inlined frames cycle through these. */
const FRAME_COLORS = [
  "transparent",
  "var(--accent)",
  "#7c9cff",
  "#d4a72c",
  "#3fb27f",
  "#c678dd",
  "#e06c75",
  "#56b6c2",
];

function frameColor(idx: number | undefined): string {
  if (!idx || idx <= 0) return "transparent";
  return FRAME_COLORS[idx % FRAME_COLORS.length] || "var(--accent)";
}

/** Small icon per effect family so a process step badges "DB write" etc. */
function effectIcon(effect: string) {
  const e = effect.toLowerCase();
  if (e.startsWith("db")) return <Database size={10} />;
  if (e.startsWith("http") || e.startsWith("net")) return <Globe size={10} />;
  if (e.startsWith("fs") || e.startsWith("file")) return <HardDrive size={10} />;
  if (e.startsWith("queue") || e.startsWith("event") || e.startsWith("mail"))
    return <Mail size={10} />;
  return <FileCog size={10} />;
}

function EffectBadge({ effect }: { effect: ControlFlowEffect }) {
  return (
    <span
      className="inline-flex items-center gap-0.5 rounded px-1 py-px text-[9px] font-medium leading-none border"
      style={{
        color: "var(--accent)",
        borderColor: "color-mix(in srgb, var(--accent) 40%, transparent)",
        background: "color-mix(in srgb, var(--accent) 8%, transparent)",
      }}
      title={effect.sink ? `${effect.effect} → ${effect.sink}` : effect.effect}
    >
      {effectIcon(effect.effect)}
      {effect.effect}
    </span>
  );
}

function FlowchartNodeImpl({ data }: NodeProps) {
  const d = data as FlowchartNodeData;
  const isDiamond = d.shape === "decision" || d.shape === "loop";
  const isTerminal =
    d.shape === "start" ||
    d.shape === "end" ||
    d.shape === "return" ||
    d.shape === "throw";
  const isThrow = d.shape === "throw";

  // Terminal palette: start/end neutral, return accented, throw danger.
  const terminalColor = isThrow
    ? "var(--danger)"
    : d.shape === "return"
      ? "var(--accent)"
      : "var(--text-3)";

  // #4883 — inlined-function frame tint: a colored left rail + optional boundary
  // label on the frame's entry node, so a hop's nodes read as one group.
  const fColor = frameColor(d.frameIndex);
  const tinted = fColor !== "transparent";

  return (
    <div className="relative w-full h-full flex items-center justify-center">
      {tinted && (
        <span
          className="absolute left-0 top-0 bottom-0 w-[3px] rounded-l"
          style={{ background: fColor }}
          aria-hidden
        />
      )}
      {d.frameLabel && (
        <span
          className="absolute -top-4 left-0 inline-flex items-center gap-0.5 text-[8px] font-medium uppercase tracking-wide truncate max-w-[140%] pointer-events-none"
          style={{ color: fColor }}
          title={`inlined: ${d.frameLabel}`}
        >
          ↳ {d.frameLabel}
        </span>
      )}
      {d.external && (
        <span
          className="absolute -top-3.5 right-0 rounded px-1 text-[8px] font-semibold uppercase tracking-wide border pointer-events-none"
          style={{
            color: "var(--text-4)",
            borderColor: "var(--border)",
            background: "var(--surface-2)",
          }}
          title="External / library call — control flow stops here"
        >
          ext
        </span>
      )}
      <Handle
        type="target"
        position={d.targetPos}
        className="!w-1.5 !h-1.5 !border-0 !bg-[var(--border)]"
      />

      {isDiamond ? (
        // Diamond: a rotated square sized to fit inside the layout box, with the
        // condition text laid OVER it (un-rotated) so it stays readable.
        <>
          <div
            className="absolute rotate-45 rounded-[3px] border-2"
            style={{
              width: "62%",
              height: "62%",
              borderColor:
                d.shape === "loop" ? "var(--warning, #d4a72c)" : "var(--accent)",
              background: "var(--surface)",
            }}
          />
          <div className="relative z-10 max-w-[78%] text-center px-1">
            <div className="text-[9px] uppercase tracking-wide text-text-4 leading-none mb-0.5">
              {d.shape === "loop" ? "loop" : "if"}
            </div>
            <div
              className="text-[10px] font-mono text-text leading-tight line-clamp-3 break-words"
              title={d.condition || d.caption}
            >
              {d.condition || d.caption}
            </div>
          </div>
        </>
      ) : isTerminal ? (
        <div
          className="flex items-center justify-center w-full h-full rounded-full border-2 px-3 text-center"
          style={{
            borderColor: terminalColor,
            background: "var(--surface)",
            color: terminalColor,
          }}
        >
          <span className="text-[11px] font-semibold uppercase tracking-wide truncate">
            {d.caption}
          </span>
        </div>
      ) : (
        // Process: a plain rectangle with the caption + optional effect badges.
        <div className="flex flex-col items-center justify-center w-full h-full rounded-md border border-border bg-surface px-2 py-1 gap-1">
          <span
            className="text-[10px] font-mono text-text-2 leading-tight line-clamp-2 text-center break-words"
            title={d.caption}
          >
            {d.caption}
          </span>
          {d.effects && d.effects.length > 0 && (
            <div className="flex flex-wrap items-center justify-center gap-0.5">
              {d.effects.slice(0, 3).map((ef, i) => (
                <EffectBadge key={`${ef.effect}-${i}`} effect={ef} />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Source-line corner ref (omitted for the synthetic start/end). */}
      {d.line ? (
        <span
          className={cn(
            "absolute -bottom-4 right-0 text-[8px] tabular-nums text-text-4 pointer-events-none",
          )}
        >
          L{d.line}
        </span>
      ) : null}

      <Handle
        type="source"
        position={d.sourcePos}
        className="!w-1.5 !h-1.5 !border-0 !bg-[var(--border)]"
      />
    </div>
  );
}

export const FlowchartNode = memo(FlowchartNodeImpl);
