/**
 * GroupSwitcher — left-sidebar section for navigating between indexing groups.
 *
 * Features:
 *  - Search/filter input
 *  - Pinned groups float to top (localStorage, max 3)
 *  - Health status dot: green = healthy (≤5% unresolved), amber = degraded
 *    (5–15%), red = critical (>15%), grey = not yet indexed.
 *    Driven by GroupMeta.bug_rate via shared deriveHealthBucket / healthTooltip.
 *  - Active group: bg-slate-200 dark:bg-slate-800 + sky-500 left border +
 *    checkmark icon (separate from health colour)
 *  - Switching preserves current surface (e.g. /flows/A → /flows/B)
 */

import { useState, useMemo, useCallback } from 'react'
import { useNavigate, useParams, useLocation } from 'react-router-dom'
import { Search, Pin, PinOff, Check } from 'lucide-react'
import type { GroupMeta } from '@/types/api'
import { getPinnedGroups, togglePin } from '@/lib/groupPins'
import { deriveHealthBucket, HEALTH_DOT_CLASS, healthTooltip } from '@/lib/groupHealth'

interface GroupSwitcherProps {
  groups: GroupMeta[]
  onNavigate?: () => void   // called after navigation (e.g. close mobile drawer)
}

/** Extracts the current surface prefix from a pathname like "/flows/fixture-a" → "flows" */
function surfaceFromPath(pathname: string): string {
  const seg = pathname.split('/').filter(Boolean)[0]
  return seg ?? 'graph'
}

export function GroupSwitcher({ groups, onNavigate }: GroupSwitcherProps) {
  const { group: activeGroup = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()

  const [query, setQuery] = useState('')
  const [pinnedIds, setPinnedIds] = useState<string[]>(() => getPinnedGroups())

  const surface = surfaceFromPath(location.pathname)

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q
      ? groups.filter(
          (g) =>
            g.id.toLowerCase().includes(q) ||
            g.display_name.toLowerCase().includes(q),
        )
      : groups
  }, [groups, query])

  // Pinned groups float to top; unpinned follow alphabetically
  const sorted = useMemo(() => {
    const pinnedSet = new Set(pinnedIds)
    const pinned = filtered.filter((g) => pinnedSet.has(g.id))
    const rest = filtered.filter((g) => !pinnedSet.has(g.id))
    return [...pinned, ...rest]
  }, [filtered, pinnedIds])

  const handleSelect = useCallback(
    (groupId: string) => {
      navigate(`/${surface}/${groupId}`)
      onNavigate?.()
    },
    [navigate, surface, onNavigate],
  )

  const handleTogglePin = useCallback(
    (e: React.MouseEvent, groupId: string) => {
      e.stopPropagation()
      const next = togglePin(groupId)
      setPinnedIds(next)
    },
    [],
  )

  return (
    <div className="flex flex-col gap-1">
      {/* Section label */}
      <p className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-600 font-semibold px-2 pb-1 select-none">
        Groups
      </p>

      {/* Search input */}
      <div className="relative px-2 mb-1">
        <Search
          className="absolute left-4 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-500 dark:text-slate-600 pointer-events-none"
          aria-hidden
        />
        <input
          type="search"
          placeholder="Filter groups…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Filter groups"
          className={[
            'w-full bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 rounded text-xs',
            'pl-7 pr-2 py-1 text-slate-700 dark:text-slate-300 placeholder-slate-600',
            'focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500',
          ].join(' ')}
        />
      </div>

      {/* Group list */}
      <ul
        className="overflow-y-auto"
        style={{ maxHeight: '60vh' }}
        role="listbox"
        aria-label="Groups"
      >
        {sorted.length === 0 && (
          <li className="px-3 py-2 text-xs text-slate-500 dark:text-slate-600 select-none">
            No groups match
          </li>
        )}

        {sorted.map((g) => {
          const isActive = g.id === activeGroup
          const isPinned = pinnedIds.includes(g.id)
          const bucket = deriveHealthBucket(g)
          const tip = healthTooltip(g)

          return (
            <li key={g.id}>
              <button
                type="button"
                role="option"
                aria-selected={isActive}
                onClick={() => handleSelect(g.id)}
                className={[
                  'group w-full flex items-center gap-2 px-3 py-1.5 text-xs text-left transition-colors',
                  'border-l-2',
                  isActive
                    ? 'bg-slate-200 dark:bg-slate-800 border-sky-500 text-slate-900 dark:text-slate-100'
                    : 'border-transparent text-slate-400 dark:text-slate-400 hover:bg-slate-200/60 dark:hover:bg-slate-800/60 hover:text-slate-700 dark:hover:text-slate-300',
                ].join(' ')}
              >
                {/* Current-project checkmark (separate affordance from health colour) */}
                <span
                  aria-hidden
                  className={[
                    'w-3 h-3 flex-shrink-0',
                    isActive ? 'text-sky-400' : 'invisible',
                  ].join(' ')}
                >
                  {isActive && <Check className="w-3 h-3" />}
                </span>

                {/* Group name */}
                <span className="flex-1 font-mono truncate" title={g.display_name}>
                  {g.display_name}
                </span>

                {/* Pin toggle — only show on hover or when pinned.
                    Uses <span role="button"> to avoid nested <button> (invalid HTML). */}
                <span
                  role="button"
                  tabIndex={0}
                  onClick={(e) => handleTogglePin(e, g.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.stopPropagation()
                      handleTogglePin(e as unknown as React.MouseEvent, g.id)
                    }
                  }}
                  aria-label={isPinned ? `Unpin ${g.display_name}` : `Pin ${g.display_name}`}
                  title={isPinned ? 'Unpin' : 'Pin to top'}
                  className={[
                    'p-0.5 rounded transition-colors cursor-pointer',
                    isPinned
                      ? 'text-sky-400 opacity-100'
                      : 'text-slate-500 dark:text-slate-600 opacity-0 group-hover:opacity-100',
                    'hover:text-sky-300',
                  ].join(' ')}
                >
                  {isPinned ? (
                    <PinOff className="w-3 h-3" />
                  ) : (
                    <Pin className="w-3 h-3" />
                  )}
                </span>

                {/* Health status dot — encodes fidelity, NOT active state */}
                <span
                  title={tip}
                  aria-label={tip}
                  className={[
                    'w-2 h-2 rounded-full flex-shrink-0',
                    HEALTH_DOT_CLASS[bucket],
                  ].join(' ')}
                />
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
