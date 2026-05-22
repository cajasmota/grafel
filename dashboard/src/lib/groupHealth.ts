/**
 * groupHealth — shared health/fidelity helpers for the project status dot.
 *
 * The dot encodes health via the group's unresolved-edges rate (bug_rate):
 *   green  — healthy   ≤5%   unresolved edges
 *   amber  — degraded  5–15% unresolved edges
 *   red    — critical  >15%  unresolved edges
 *   grey   — unknown   bug_rate absent (group not yet indexed)
 *
 * This module is the single source of truth. All three surfaces that show the
 * dot (landing cards, project-switcher list, top-bar breadcrumb dropdown) import
 * from here so the colour logic is never duplicated.
 */

import type { GroupMeta } from '@/types/api'

export type HealthBucket = 'healthy' | 'degraded' | 'critical' | 'unknown'

/**
 * Derive the health bucket from a GroupMeta object.
 * Uses the real bug_rate field; falls back to 'unknown' when absent.
 */
export function deriveHealthBucket(g: GroupMeta): HealthBucket {
  if (g.bug_rate === undefined) return 'unknown'
  if (g.bug_rate <= 5) return 'healthy'
  if (g.bug_rate <= 15) return 'degraded'
  return 'critical'
}

/** Tailwind bg class for the dot, per bucket. */
export const HEALTH_DOT_CLASS: Record<HealthBucket, string> = {
  healthy:  'bg-emerald-500',
  degraded: 'bg-amber-400',
  critical: 'bg-red-500',
  unknown:  'bg-slate-400 dark:bg-slate-600',
}

/** Human-readable label shown in tooltips. */
export function healthTooltip(g: GroupMeta): string {
  const bucket = deriveHealthBucket(g)
  const fidelityPct =
    g.bug_rate !== undefined
      ? ` — Fidelity ${(100 - g.bug_rate).toFixed(0)}%`
      : ''

  switch (bucket) {
    case 'healthy':
      return `Health: healthy${fidelityPct}`
    case 'degraded':
      return `Health: degraded${fidelityPct}`
    case 'critical':
      return `Health: critical${fidelityPct}`
    default:
      return 'Health: unknown — not yet indexed'
  }
}
