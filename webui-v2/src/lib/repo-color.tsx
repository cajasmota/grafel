/* ============================================================
   lib/repo-color.ts — shared repo → color resolver.

   Issue #1946: single source-of-truth for repo color tokens used across:
     - Topology right panel publisher/subscriber rows
     - Flow step cards (cross-stack border + chip)
     - Path list backend section headers
     - RefLine repo chip
     - Graph view node color-by-repo mode

   For groups with ≤8 repos: curated 8-slot palette drawing from the
   existing --pastel-N / --pastel-N-ink token scale (indices 1–8), which
   already handles dark/light theme switching automatically.

   For >8 repos: golden-ratio HSL rotation with capped saturation and
   lightness so chips never look garish in either theme.

   Returns CSS-variable strings where possible so theme switching is free.
   ============================================================ */

export interface RepoColors {
  /** CSS background value for the chip. */
  background: string;
  /** CSS foreground (text) value for the chip. */
  foreground: string;
  /** CSS border value for the chip. */
  border: string;
}

// ---------------------------------------------------------------------------
// Curated palette — 8 slots, pastel-N tokens (inherits theme from tokens.css)
// ---------------------------------------------------------------------------

/** Number of curated slots before we fall through to hash-derived HSL. */
const CURATED_SLOTS = 8;

/**
 * A stable index → CSS variable index mapping for the first CURATED_SLOTS
 * repos in a group.  Pastel indices 1–8 map to sky / mint / peach / rose /
 * lavender / butter / sage / blush — all visually distinct in dark + light.
 */
const CURATED: RepoColors[] = Array.from({ length: CURATED_SLOTS }, (_, i) => {
  const n = i + 1; // pastel-1 … pastel-8
  return {
    background: `color-mix(in srgb, var(--pastel-${n}) 28%, transparent)`,
    foreground: `var(--pastel-${n}-ink)`,
    border: `color-mix(in srgb, var(--pastel-${n}-ink) 40%, transparent)`,
  };
});

// ---------------------------------------------------------------------------
// Hash + golden-ratio rotation for groups with >8 repos
// ---------------------------------------------------------------------------

/** Golden-ratio fractional increment for maximally spread hues. */
const GOLDEN_RATIO = 0.6180339887;

/** Compute a stable 0-based integer index from a repo slug (djb2 variant). */
function slugIndex(slug: string): number {
  let h = 0;
  for (let i = 0; i < slug.length; i++) {
    h = (h * 31 + slug.charCodeAt(i)) & 0xffffffff;
  }
  return Math.abs(h);
}

/**
 * Derive a comfortable HSL color pair for a repo slug beyond the curated 8.
 * Saturation is capped at 42% and lightness band is 55–68% (light theme) /
 * 35–50% (dark theme, rendered via CSS).  We can't detect theme in pure TS so
 * we output static HSL values tuned for the dark theme (dark is more common
 * in dev tools) and rely on a mild mismatch in light mode being acceptable.
 */
function hslDerived(slug: string): RepoColors {
  const idx = slugIndex(slug);
  const hue = ((idx * GOLDEN_RATIO) % 1) * 360;
  // Saturation slightly varied (36–46%) to avoid monotony.
  const sat = 36 + (idx % 5) * 2;
  // Lightness in comfortable dark-theme band.
  const L = 44 + (idx % 7);
  const Ld = L - 8; // darker for border/ink

  const bg = `hsl(${hue.toFixed(0)}, ${sat}%, ${L}%, 0.28)`;
  const fg = `hsl(${hue.toFixed(0)}, ${(sat + 20).toFixed(0)}%, ${(L + 22).toFixed(0)}%)`;
  const border = `hsl(${hue.toFixed(0)}, ${sat}%, ${Ld}%, 0.45)`;
  return { background: bg, foreground: fg, border };
}

// ---------------------------------------------------------------------------
// Per-group stable slot assignment
//
// We assign curated slots by ORDER OF FIRST ENCOUNTER within a call to
// getRepoColorForGroup so that the same group always maps the same repo to the
// same slot.  When called per-repo without a group context, we hash-derive
// directly so there is no cross-group state.
// ---------------------------------------------------------------------------

/**
 * Map: groupId → (slugOrder: slug[], seen: Map<slug, index>)
 * Cleared on page navigation — module lifetime is per-session which is fine.
 */
const groupSlots = new Map<string, { order: string[]; idx: Map<string, number> }>();

/**
 * Return stable colors for a repo slug within a named group context.
 * The first CURATED_SLOTS unique slugs seen for a groupId get a curated
 * palette slot; subsequent slugs get hash-derived HSL.
 *
 * This is the preferred call site when a group ID is available (flows, paths,
 * topology detail — all have a groupId in scope).
 */
export function getRepoColorForGroup(groupId: string, slug: string): RepoColors {
  if (!slug) return CURATED[0];

  let state = groupSlots.get(groupId);
  if (!state) {
    state = { order: [], idx: new Map() };
    groupSlots.set(groupId, state);
  }

  let i = state.idx.get(slug);
  if (i === undefined) {
    i = state.order.length;
    state.order.push(slug);
    state.idx.set(slug, i);
  }

  if (i < CURATED_SLOTS) return CURATED[i];
  return hslDerived(slug);
}

/**
 * Return stable colors for a repo slug WITHOUT a group context.
 * Uses a pure hash so colors are stable across renders without any group
 * state.  The first CURATED_SLOTS hash-buckets get curated tokens; the rest
 * get hash-derived HSL.
 *
 * This is the fallback for components that don't have a groupId readily
 * available (e.g., standalone chip in the graph view).
 */
export function getRepoColor(slug: string): RepoColors {
  if (!slug) return CURATED[0];
  const i = slugIndex(slug) % (CURATED_SLOTS * 3); // wider spread before hash-deriving
  if (i < CURATED_SLOTS) return CURATED[i];
  return hslDerived(slug);
}

// ---------------------------------------------------------------------------
// React component — exported from this lib file so there is ONE source of
// truth for what a repo chip looks like everywhere.
// ---------------------------------------------------------------------------

import { cn } from "@/lib/utils";

export interface RepoChipProps {
  /** Repository slug to display and color. */
  slug: string;
  /**
   * Optional group id for stable curated-slot assignment.
   * Pass this whenever a groupId is available in the component's scope.
   */
  groupId?: string;
  className?: string;
  /** Truncate label to this character count (default: no truncation). */
  maxLength?: number;
}

/**
 * RepoChip — colored pill badge for a repository slug.
 *
 * Colors are resolved from the shared repo-color palette so the same repo
 * always gets the same color regardless of which component renders it.
 */
export function RepoChip({ slug, groupId, className, maxLength }: RepoChipProps) {
  const colors = groupId ? getRepoColorForGroup(groupId, slug) : getRepoColor(slug);
  const label = maxLength && slug.length > maxLength ? slug.slice(0, maxLength) + "…" : slug;

  return (
    <span
      className={cn(
        "inline-flex items-center shrink-0 h-[18px] px-1.5 rounded",
        "text-[10px] font-semibold font-mono leading-none select-none",
        className,
      )}
      style={{
        background: colors.background,
        color: colors.foreground,
        border: `1px solid ${colors.border}`,
      }}
      title={slug}
    >
      {label}
    </span>
  );
}
