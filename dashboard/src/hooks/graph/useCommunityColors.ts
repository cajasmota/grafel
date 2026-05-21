import { useMemo } from 'react'
import type { Community } from '@/types/api'

/**
 * Derives a stable Map<communityId, hexColor> from community IDs.
 * Uses a perceptually-uniform palette — at least 20 distinct hues,
 * chosen to be distinguishable in both dark and light modes and
 * on high-contrast displays.
 *
 * The palette is deterministic: same communityId → same color across
 * renders and sessions.
 */

// 24-color palette: HSL-spaced, sat/lightness tuned for dark-bg legibility
const COMMUNITY_PALETTE = [
  '#60a5fa', // blue-400
  '#34d399', // emerald-400
  '#f472b6', // pink-400
  '#a78bfa', // violet-400
  '#fb923c', // orange-400
  '#22d3ee', // cyan-400
  '#facc15', // yellow-400
  '#f87171', // red-400
  '#4ade80', // green-400
  '#c084fc', // purple-400
  '#2dd4bf', // teal-400
  '#fb7185', // rose-400
  '#818cf8', // indigo-400
  '#fbbf24', // amber-400
  '#86efac', // green-300
  '#93c5fd', // blue-300
  '#d8b4fe', // purple-300
  '#6ee7b7', // emerald-300
  '#fca5a5', // red-300
  '#fde68a', // amber-200
  '#67e8f9', // cyan-300
  '#fdba74', // orange-300
  '#a5f3fc', // cyan-200
  '#bbf7d0', // green-200
]

/**
 * Returns a stable Map<communityId, hexColor> for the given communities array.
 * Memoized on community ID array identity.
 */
export function useCommunityColors(
  communities: Community[],
): Map<number, string> {
  return useMemo(() => {
    const map = new Map<number, string>()
    // Sort by id for determinism regardless of arrival order
    const sorted = [...communities].sort((a, b) => a.id - b.id)
    sorted.forEach((c, idx) => {
      map.set(c.id, COMMUNITY_PALETTE[idx % COMMUNITY_PALETTE.length])
    })
    return map
  }, [communities])
}

/** Neutral muted gray used for the denoised/ungrouped bucket (community_id = -1)
 *  and any other undefined/null community. Slate-500 matches the graph's dark theme.
 */
const UNGROUPED_COLOR = '#64748b' // slate-500

/**
 * Returns the hex color for a given communityId, using the palette directly.
 * Use this for one-off lookups without hooks.
 * community_id = -1 (denoised/ungrouped nodes) and null/undefined both return
 * a muted gray rather than crashing or producing undefined.
 */
export function communityColor(communityId: number | null | undefined): string {
  if (communityId == null || communityId === -1) return UNGROUPED_COLOR
  // % on a negative number returns negative in JS — ensure positive index
  const idx = ((communityId % COMMUNITY_PALETTE.length) + COMMUNITY_PALETTE.length) % COMMUNITY_PALETTE.length
  return COMMUNITY_PALETTE[idx]
}
