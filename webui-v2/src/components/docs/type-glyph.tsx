/* ============================================================
   type-glyph.tsx — TypeGlyph pill + TypeBadge used in the
   Docs tree and entity article header.

   TypeGlyph: a small 2-3 letter mono pill ("fn", "cmp", "hk", …)
              color-tinted by entity type.
   TypeBadge:  a wider pill with a colored dot + full type label.
   ============================================================ */

import { Folder } from "lucide-react";
import type { DocsEntityKind } from "@/data/types";

interface TypeMeta {
  label: string;
  glyph: string;
  /** Index into the pastel scale (1–6), or 0 for muted grey. */
  paletteIdx: number;
}

const TYPE_META: Record<DocsEntityKind, TypeMeta> = {
  function:      { label: "function",  glyph: "fn",  paletteIdx: 6 },
  component:     { label: "component", glyph: "cmp", paletteIdx: 1 },
  hook:          { label: "hook",      glyph: "hk",  paletteIdx: 2 },
  class:         { label: "class",     glyph: "cls", paletteIdx: 3 },
  method:        { label: "method",    glyph: "mth", paletteIdx: 4 },
  http_endpoint: { label: "endpoint",  glyph: "ep",  paletteIdx: 5 },
  module:        { label: "module",    glyph: "",    paletteIdx: 0 },
  folder:        { label: "folder",    glyph: "",    paletteIdx: 0 },
  repo:          { label: "repo",      glyph: "",    paletteIdx: 0 },
};

// Pastel scale: matches design tokens var(--pastel-N) and var(--pastel-N-ink).
// We use inline styles to stay true to the token values instead of
// embedding them as Tailwind utilities (tokens are not available at build time).
const PASTEL_COLORS: Record<number, { bg: string; fg: string }> = {
  0: { bg: "var(--text-5)",    fg: "var(--text-3)" },
  1: { bg: "var(--pastel-1)",  fg: "var(--pastel-1-ink)" },
  2: { bg: "var(--pastel-2)",  fg: "var(--pastel-2-ink)" },
  3: { bg: "var(--pastel-3)",  fg: "var(--pastel-3-ink)" },
  4: { bg: "var(--pastel-4)",  fg: "var(--pastel-4-ink)" },
  5: { bg: "var(--pastel-5)",  fg: "var(--pastel-5-ink)" },
  6: { bg: "var(--pastel-6)",  fg: "var(--pastel-6-ink)" },
};

/** Small 2-3 letter mono pill used in the tree. aria-hidden — decorative. */
export function TypeGlyph({ type }: { type: DocsEntityKind }) {
  const meta = TYPE_META[type] ?? TYPE_META.module;
  const colors = PASTEL_COLORS[meta.paletteIdx];

  if (!meta.glyph) {
    return (
      <span
        className="inline-flex items-center justify-center w-5 h-5 rounded shrink-0"
        style={{ color: "var(--text-3)" }}
        aria-hidden="true"
      >
        <Folder size={11} />
      </span>
    );
  }

  return (
    <span
      className="inline-flex items-center justify-center shrink-0 font-mono text-[10px] font-semibold rounded px-1 py-px leading-none"
      style={{
        background: `color-mix(in srgb, ${colors.bg} 28%, transparent)`,
        color: colors.fg,
        minWidth: "2ch",
      }}
      aria-hidden="true"
    >
      {meta.glyph}
    </span>
  );
}

/** Wider pill with colored dot + full type label. Used in the entity head. */
export function TypeBadge({ type }: { type: DocsEntityKind }) {
  const meta = TYPE_META[type] ?? TYPE_META.module;
  const colors = PASTEL_COLORS[meta.paletteIdx];
  return (
    <span
      className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-xs font-medium"
      style={{
        background: `color-mix(in srgb, ${colors.bg} 16%, transparent)`,
        color: colors.fg,
        border: `1px solid color-mix(in srgb, ${colors.bg} 40%, transparent)`,
      }}
    >
      <span
        className="w-1.5 h-1.5 rounded-full shrink-0"
        style={{ background: colors.bg }}
        aria-hidden="true"
      />
      {meta.label}
    </span>
  );
}
