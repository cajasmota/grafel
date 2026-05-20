/**
 * GroupSelector — top-right dropdown for navigating between indexing groups.
 *
 * Desktop: dropdown panel anchored below the trigger button.
 * Mobile (≤640 px): full-screen sheet from the top.
 *
 * Features:
 *  - Trigger: current group display name + ChevronDown
 *  - Search input at top of panel
 *  - Pinned groups section (max 3, star toggle per row)
 *  - Other groups section
 *  - Unresolved edges status dot: green ≤5% / amber 5–15% / red >15%
 *  - Entity-count tooltip
 *  - Selected row visually highlighted
 *  - Closes on outside-click or Escape
 *  - Switching preserves the current surface (path replacement)
 */

import {
  useState,
  useMemo,
  useCallback,
  useRef,
  useEffect,
  KeyboardEvent as ReactKeyboardEvent,
} from 'react'
import { useNavigate, useParams, useLocation } from 'react-router-dom'
import { Search, Pin, PinOff, ChevronDown, X } from 'lucide-react'
import type { GroupMeta } from '@/types/api'
import { getPinnedGroups, togglePin } from '@/lib/groupPins'

interface GroupSelectorProps {
  groups: GroupMeta[]
}

/** Derive a synthetic unresolved edges bucket from entity_count for mock data. */
function bugRateBucket(g: GroupMeta): 'green' | 'amber' | 'red' {
  const sum = g.id.split('').reduce((acc, c) => acc + c.charCodeAt(0), 0)
  const pct = sum % 30
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

export function GroupSelector({ groups }: GroupSelectorProps) {
  const { group: activeGroup = '' } = useParams()
  const navigate = useNavigate()
  const location = useLocation()

  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [pinnedIds, setPinnedIds] = useState<string[]>(() => getPinnedGroups())

  const containerRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const surface = surfaceFromPath(location.pathname)

  // Find display name for the active group
  const activeDisplay = useMemo(() => {
    return groups.find((g) => g.id === activeGroup)?.display_name ?? activeGroup
  }, [groups, activeGroup])

  // Filter groups by search query
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return groups
    return groups.filter(
      (g) =>
        g.id.toLowerCase().includes(q) ||
        g.display_name.toLowerCase().includes(q),
    )
  }, [groups, query])

  // Split into pinned / other
  const { pinned, others } = useMemo(() => {
    const pinnedSet = new Set(pinnedIds)
    return {
      pinned: filtered.filter((g) => pinnedSet.has(g.id)),
      others: filtered.filter((g) => !pinnedSet.has(g.id)),
    }
  }, [filtered, pinnedIds])

  // Open/close helpers
  const openPanel = useCallback(() => {
    setOpen(true)
    // Focus search after paint
    requestAnimationFrame(() => searchRef.current?.focus())
  }, [])

  const closePanel = useCallback(() => {
    setOpen(false)
    setQuery('')
  }, [])

  // Close on outside-click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        closePanel()
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open, closePanel])

  // Close on Escape
  useEffect(() => {
    if (!open) return
    const handler = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') closePanel()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [open, closePanel])

  const handleSelect = useCallback(
    (groupId: string) => {
      navigate(`/${surface}/${groupId}`)
      closePanel()
    },
    [navigate, surface, closePanel],
  )

  const handleTogglePin = useCallback(
    (e: React.MouseEvent | ReactKeyboardEvent, groupId: string) => {
      e.stopPropagation()
      const next = togglePin(groupId)
      setPinnedIds(next)
    },
    [],
  )

  const isMobile = typeof window !== 'undefined' && window.innerWidth < 640

  return (
    <div ref={containerRef} className="relative" data-testid="group-selector">
      {/* Trigger button */}
      <button
        type="button"
        aria-label="Select group"
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => (open ? closePanel() : openPanel())}
        className={[
          'flex items-center gap-1.5 px-3 py-1.5 rounded text-sm transition-colors',
          'border border-slate-700',
          open
            ? 'bg-slate-800 text-slate-200 border-sky-500'
            : 'bg-slate-900 text-slate-400 hover:bg-slate-800 hover:text-slate-200',
        ].join(' ')}
        data-testid="group-selector-trigger"
      >
        <span className="font-mono max-w-[12rem] truncate" data-testid="group-selector-label">
          {activeDisplay || 'Select group'}
        </span>
        <ChevronDown
          className={[
            'w-3.5 h-3.5 flex-shrink-0 transition-transform',
            open ? 'rotate-180' : '',
          ].join(' ')}
          aria-hidden
        />
      </button>

      {/* Dropdown / sheet panel */}
      {open && (
        <div
          className={[
            // Mobile: fixed full-screen sheet from top
            'sm:absolute sm:right-0 sm:top-full sm:mt-1 sm:w-72',
            'sm:max-h-[min(480px,80vh)] sm:rounded-lg sm:shadow-xl sm:border sm:border-slate-700',
            // Mobile overrides applied below via fixed
            'fixed sm:static inset-x-0 top-12 bottom-0 sm:inset-auto',
            'bg-slate-950 z-50 flex flex-col',
          ].join(' ')}
          role="dialog"
          aria-modal="false"
          aria-label="Group selector"
          data-testid="group-selector-panel"
        >
          {/* Mobile close strip */}
          <div className="flex items-center justify-between px-3 py-2 border-b border-slate-800 sm:hidden">
            <span className="text-xs font-semibold text-slate-400 uppercase tracking-wider">
              Switch group
            </span>
            <button
              type="button"
              aria-label="Close"
              onClick={closePanel}
              className="p-1 rounded text-slate-500 hover:text-slate-300"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          {/* Search input */}
          <div className="relative px-3 py-2 border-b border-slate-800">
            <Search
              className="absolute left-5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-600 pointer-events-none"
              aria-hidden
            />
            <input
              ref={searchRef}
              type="search"
              placeholder="Filter groups…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              aria-label="Filter groups"
              className={[
                'w-full bg-slate-900 border border-slate-700 rounded text-xs',
                'pl-7 pr-2 py-1.5 text-slate-300 placeholder-slate-600',
                'focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500',
              ].join(' ')}
              data-testid="group-selector-search"
            />
          </div>

          {/* Scrollable list */}
          <div className="overflow-y-auto flex-1" role="listbox" aria-label="Groups">
            {pinned.length === 0 && others.length === 0 && (
              <p className="px-4 py-3 text-xs text-slate-600 select-none">
                No groups match
              </p>
            )}

            {/* Pinned section */}
            {pinned.length > 0 && (
              <GroupSection label="Pinned" groups={pinned} activeGroup={activeGroup} pinnedIds={pinnedIds} onSelect={handleSelect} onTogglePin={handleTogglePin} />
            )}

            {/* Other section */}
            {others.length > 0 && (
              <GroupSection label={pinned.length > 0 ? 'Other' : undefined} groups={others} activeGroup={activeGroup} pinnedIds={pinnedIds} onSelect={handleSelect} onTogglePin={handleTogglePin} />
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ─── GroupSection ─────────────────────────────────────────────────────────────

interface GroupSectionProps {
  label?: string
  groups: GroupMeta[]
  activeGroup: string
  pinnedIds: string[]
  onSelect: (id: string) => void
  onTogglePin: (e: React.MouseEvent | ReactKeyboardEvent, id: string) => void
}

function GroupSection({ label, groups, activeGroup, pinnedIds, onSelect, onTogglePin }: GroupSectionProps) {
  return (
    <div>
      {label && (
        <p className="px-3 pt-2 pb-1 text-[10px] uppercase tracking-wider text-slate-600 font-semibold select-none">
          {label}
        </p>
      )}
      <ul>
        {groups.map((g) => {
          const isActive = g.id === activeGroup
          const isPinned = pinnedIds.includes(g.id)
          const bucket = bugRateBucket(g)

          return (
            <li key={g.id}>
              <button
                type="button"
                role="option"
                aria-selected={isActive}
                onClick={() => onSelect(g.id)}
                className={[
                  'group w-full flex items-center gap-2 px-3 py-2 text-xs text-left transition-colors',
                  'border-l-2',
                  isActive
                    ? 'bg-slate-800 border-sky-500 text-slate-100'
                    : 'border-transparent text-slate-400 hover:bg-slate-800/60 hover:text-slate-300',
                ].join(' ')}
              >
                {/* Group name */}
                <span className="flex-1 font-mono truncate" title={g.display_name}>
                  {g.display_name}
                </span>

                {/* Pin toggle */}
                <span
                  role="button"
                  tabIndex={0}
                  onClick={(e) => onTogglePin(e, g.id)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.stopPropagation()
                      onTogglePin(e, g.id)
                    }
                  }}
                  aria-label={isPinned ? `Unpin ${g.display_name}` : `Pin ${g.display_name}`}
                  title={isPinned ? 'Unpin' : 'Pin to top'}
                  className={[
                    'p-0.5 rounded transition-colors cursor-pointer',
                    isPinned
                      ? 'text-sky-400 opacity-100'
                      : 'text-slate-600 opacity-0 group-hover:opacity-100',
                    'hover:text-sky-300',
                  ].join(' ')}
                >
                  {isPinned ? <PinOff className="w-3 h-3" /> : <Pin className="w-3 h-3" />}
                </span>

                {/* Unresolved edges dot */}
                <span
                  title={`${bucketLabel[bucket]} — ${g.entity_count.toLocaleString()} entities`}
                  aria-label={bucketLabel[bucket]}
                  className={['w-2 h-2 rounded-full flex-shrink-0', bucketClass[bucket]].join(' ')}
                />
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
