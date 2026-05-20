/**
 * GroupSwitcher — left-sidebar section for navigating between indexing groups.
 *
 * Features:
 *  - Search/filter input
 *  - Pinned groups float to top (localStorage, max 3)
 *  - Unresolved edges status dot: green ≤5% / amber 5-15% / red >15%
 *    (uses entity_count as proxy until a real unresolved_edges field ships)
 *  - Entity count in tooltip
 *  - Active group: bg-slate-200 dark:bg-slate-800 + sky-500 left border
 *  - Switching preserves current surface (e.g. /flows/A → /flows/B)
 */

import { useState, useMemo, useCallback } from 'react'
import { useNavigate, useParams, useLocation } from 'react-router-dom'
import { Search, Pin, PinOff } from 'lucide-react'
import type { GroupMeta } from '@/types/api'
import { getPinnedGroups, togglePin } from '@/lib/groupPins'

interface GroupSwitcherProps {
  groups: GroupMeta[]
  onNavigate?: () => void   // called after navigation (e.g. close mobile drawer)
}

/** Derive a synthetic unresolved edges bucket from entity_count for mock data. */
function bugRateBucket(g: GroupMeta): 'green' | 'amber' | 'red' {
  // Real API will eventually carry a `unresolved_edges` field.
  // Until then we use a deterministic hash of the id so fixture groups get
  // stable (but varied) colours in the UI.
  const sum = g.id.split('').reduce((acc, c) => acc + c.charCodeAt(0), 0)
  const pct = sum % 30   // 0-29
  if (pct <= 5) return 'green'
  if (pct <= 15) return 'amber'
  return 'red'
}

const bucketClass: Record<'green' | 'amber' | 'red', string> = {
  green: 'bg-emerald-500',
  amber: 'bg-amber-400',
  red: 'bg-red-500',
}

const bucketLabel: Record<'green' | 'amber' | 'red', string> = {
  green: '≤5% unresolved edges',
  amber: '5–15% unresolved edges',
  red: '>15% unresolved edges',
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
          const bucket = bugRateBucket(g)

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

                {/* Unresolved edges status dot */}
                <span
                  title={`${bucketLabel[bucket]} — ${g.entity_count.toLocaleString()} entities`}
                  aria-label={bucketLabel[bucket]}
                  className={[
                    'w-2 h-2 rounded-full flex-shrink-0',
                    bucketClass[bucket],
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
