import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { useParams, Outlet, useNavigate } from 'react-router-dom'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ChevronUp, ChevronDown, List, Globe, Columns2, FileDown } from 'lucide-react'
import { PathRow } from '@/components/paths/PathRow'
import { PathsGroup } from '@/components/paths/PathsGroup'
import { BackendGroup, readCollapsed, writeCollapsed } from '@/components/paths/BackendGroup'
import { PathSearchInput } from '@/components/paths/PathSearchInput'
import { OrphanCallersTab } from '@/components/paths/OrphanCallersTab'
import { EmptyState } from '@/components/shared/EmptyState'
import { PathListSkeleton } from '@/components/shared/LoadingState'
import { ErrorBoundary } from '@/components/shared/ErrorBoundary'
import { usePathList } from '@/hooks/paths/usePathList'
import { usePathFilters } from '@/hooks/paths/usePathFilters'
import { useOrphanCallers } from '@/hooks/paths/useOrphanCallers'
import { groupPaths } from '@/lib/groupPaths'
import type { PathRow as PathRowType, BackendInfo } from '@/types/api'
import type { PathGroup } from '@/lib/groupPaths'

// ─── localStorage key for flat/grouped preference ────────────────────────────
const LS_FLAT_KEY = 'paths-view-flat'

// ─── Default collapse threshold for backends (#1219) ─────────────────────────
// Backends with fewer than this many endpoints default to collapsed.
const BACKEND_SMALL_THRESHOLD = 5

function readFlatPref(): boolean {
  try {
    return localStorage.getItem(LS_FLAT_KEY) === 'true'
  } catch {
    return false
  }
}

// ─── Estimated row heights for react-virtual ─────────────────────────────────
const FLAT_ROW_HEIGHT = 38
const GROUP_HEADER_HEIGHT = 37
const PATH_ROW_HEIGHT = 38
const BACKEND_HEADER_HEIGHT = 42
const BACKEND_EMPTY_HEIGHT = 40

// ─── Tab type ─────────────────────────────────────────────────────────────────
type TabId = 'endpoints' | 'orphan-callers'

// ─── Virtual item descriptors for grouped list ───────────────────────────────

type VirtualItem =
  | { kind: 'backend-header'; backend: BackendInfo }
  | { kind: 'backend-empty'; backend: BackendInfo }
  | { kind: 'group-header'; group: PathGroup; groupKey: string }
  | { kind: 'path-row'; path: PathRowType; groupKey: string }

/**
 * Build a flat array of VirtualItem descriptors from the current expand state.
 * This is the "table of contents" the virtualizer indexes into.
 */
function buildVirtualItems(
  groups: PathGroup[],
  expandedGroups: Record<string, boolean>,
  backends: BackendInfo[] | undefined,
  expandedBackends: Record<string, boolean>,
  isMultiBackend: boolean,
  groupPathsFn: (paths: PathRowType[]) => PathGroup[],
): VirtualItem[] {
  const items: VirtualItem[] = []

  if (isMultiBackend && backends) {
    for (const backend of backends) {
      items.push({ kind: 'backend-header', backend })
      if (!expandedBackends[backend.name]) continue

      const isEmpty = backend.paths.length === 0
      if (isEmpty) {
        items.push({ kind: 'backend-empty', backend })
        continue
      }

      const backendGroups = groupPathsFn(backend.paths)
      for (const g of backendGroups) {
        const gKey = `${backend.name}::${g.name}`
        items.push({ kind: 'group-header', group: g, groupKey: gKey })
        if (!expandedGroups[gKey]) continue
        for (const path of g.paths) {
          items.push({ kind: 'path-row', path, groupKey: gKey })
        }
      }
    }
  } else {
    for (const g of groups) {
      items.push({ kind: 'group-header', group: g, groupKey: g.name })
      if (!expandedGroups[g.name]) continue
      for (const path of g.paths) {
        items.push({ kind: 'path-row', path, groupKey: g.name })
      }
    }
  }

  return items
}

function estimateItemSize(item: VirtualItem): number {
  switch (item.kind) {
    case 'backend-header': return BACKEND_HEADER_HEIGHT
    case 'backend-empty':  return BACKEND_EMPTY_HEIGHT
    case 'group-header':   return GROUP_HEADER_HEIGHT
    case 'path-row':       return PATH_ROW_HEIGHT
  }
}

/**
 * VirtualFlatList — renders only visible PathRows using @tanstack/react-virtual.
 * Handles 1000+ paths without jank.
 */
function VirtualFlatList({
  paths,
  group,
  onSelect,
}: {
  paths: PathRowType[]
  group: string
  onSelect: (hash: string) => void
}) {
  const parentRef = useRef<HTMLDivElement>(null)

  const virtualizer = useVirtualizer({
    count: paths.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => FLAT_ROW_HEIGHT,
    overscan: 10,
  })

  return (
    <div
      ref={parentRef}
      className="flex-1 overflow-y-auto"
      role="grid"
      aria-label="API paths"
    >
      <div
        role="rowgroup"
        style={{ height: virtualizer.getTotalSize(), position: 'relative' }}
      >
        {virtualizer.getVirtualItems().map((virtualRow) => {
          const path = paths[virtualRow.index]
          return (
            <div
              key={path.path_hash}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${virtualRow.start}px)`,
              }}
            >
              <PathRow
                path={path}
                group={group}
                onSelect={() => onSelect(path.path_hash)}
              />
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ─── VirtualGroupedList ───────────────────────────────────────────────────────

interface VirtualGroupedListProps {
  items: VirtualItem[]
  group: string
  isMultiBackend: boolean
  expandedGroups: Record<string, boolean>
  expandedBackends: Record<string, boolean>
  onToggleGroup: (key: string) => void
  onToggleBackend: (name: string) => void
  onSelectPath: (hash: string) => void
  listRef: React.RefObject<HTMLDivElement | null>
}

/**
 * VirtualGroupedList — virtualized grouped list for the Paths surface (#1303).
 *
 * Architecture:
 *   All group-headers and path-rows are flattened into a single VirtualItem[].
 *   useVirtualizer renders only the visible window + overscan, keeping DOM node
 *   count bounded regardless of endpoint count.
 *
 * Sticky headers:
 *   We track the "current section header" at the top of the visible window and
 *   render it in a sticky overlay (`position: sticky; top: 0`) above the scroll
 *   container. This gives true CSS-sticky behaviour without breaking the virtual
 *   absolute-layout.
 *
 * Keyboard navigation:
 *   The parent's ArrowUp/ArrowDown handler queries [role="row"] in the scroll
 *   container — only currently-rendered rows are in DOM, which is correct for
 *   virtualized lists.
 */
function VirtualGroupedList({
  items,
  group,
  isMultiBackend,
  expandedGroups,
  expandedBackends,
  onToggleGroup,
  onToggleBackend,
  onSelectPath,
  listRef,
}: VirtualGroupedListProps) {
  const parentRef = useRef<HTMLDivElement>(null)

  // Expose scroll container ref to parent for keyboard nav
  useEffect(() => {
    if (listRef && 'current' in listRef) {
      ;(listRef as React.MutableRefObject<HTMLDivElement | null>).current =
        parentRef.current
    }
  }, [listRef])

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index: number) => estimateItemSize(items[index]),
    overscan: 8,
  })

  // ── Sticky header tracking ────────────────────────────────────────────────
  // Find the last header item whose `start` position is <= scrollTop.
  // Render it as a sticky overlay at the top of the container.
  const [scrollTop, setScrollTop] = useState(0)
  const onScroll = useCallback((e: React.UIEvent<HTMLDivElement>) => {
    setScrollTop((e.target as HTMLDivElement).scrollTop)
  }, [])

  // The visible virtual rows used for rendering
  const allVirtualRows = virtualizer.getVirtualItems()

  // We need all item start positions, not just visible ones.
  // react-virtual exposes virtualizer.range for visible indices.
  // For sticky header we look at ALL items whose start <= scrollTop and find last header.
  // Find the sticky header: the last section header whose BOTTOM edge has scrolled
  // past the top of the viewport (i.e., it is fully above scrollTop).
  // When a header is still visible in the scroll area, we don't overlay it.
  const stickyHeader = useMemo(() => {
    if (scrollTop === 0) return null

    let lastHeader: VirtualItem | null = null
    let accOffset = 0
    for (let i = 0; i < items.length; i++) {
      const size = estimateItemSize(items[i])
      const bottom = accOffset + size
      // Header has fully scrolled out of view
      if (bottom <= scrollTop) {
        if (items[i].kind === 'backend-header' || items[i].kind === 'group-header') {
          lastHeader = items[i]
        }
      } else {
        break
      }
      accOffset += size
    }
    return lastHeader
  }, [items, scrollTop])

  return (
    <div className="flex-1 overflow-hidden flex flex-col" style={{ position: 'relative' }}>
      {/* ── Sticky header overlay ──────────────────────────────────────────── */}
      {stickyHeader && stickyHeader.kind === 'backend-header' && (
        <div className="flex-shrink-0 z-20" style={{ position: 'sticky', top: 0 }}>
          <BackendGroup
            backend={stickyHeader.backend}
            isExpanded={!!expandedBackends[stickyHeader.backend.name]}
            onToggle={() => onToggleBackend(stickyHeader.backend.name)}
          >
            {null}
          </BackendGroup>
        </div>
      )}
      {stickyHeader && stickyHeader.kind === 'group-header' && (
        <div className="flex-shrink-0 z-10" style={{ position: 'sticky', top: 0 }}>
          <PathsGroup
            group={stickyHeader.group}
            isExpanded={!!expandedGroups[stickyHeader.groupKey]}
            onToggle={() => onToggleGroup(stickyHeader.groupKey)}
          >
            {null}
          </PathsGroup>
        </div>
      )}

      {/* ── Virtual scroll container ───────────────────────────────────────── */}
      <div
        ref={parentRef}
        className="flex-1 overflow-y-auto"
        role="grid"
        aria-label={isMultiBackend ? 'API paths grouped by backend' : 'API paths'}
        onScroll={onScroll}
      >
        <div
          style={{ height: virtualizer.getTotalSize(), position: 'relative' }}
        >
          {allVirtualRows.map((virtualRow) => {
            const item = items[virtualRow.index]

            // ── Backend header ──────────────────────────────────────────────
            if (item.kind === 'backend-header') {
              return (
                <div
                  key={`bh:${item.backend.name}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                >
                  <BackendGroup
                    backend={item.backend}
                    isExpanded={!!expandedBackends[item.backend.name]}
                    onToggle={() => onToggleBackend(item.backend.name)}
                  >
                    {null}
                  </BackendGroup>
                </div>
              )
            }

            // ── Empty backend hint ──────────────────────────────────────────
            if (item.kind === 'backend-empty') {
              return (
                <div
                  key={`be:${item.backend.name}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                >
                  <div className="px-4 py-3 text-xs text-slate-400 dark:text-slate-500 italic">
                    {item.backend.count} endpoint{item.backend.count !== 1 ? 's' : ''} defined here — index this backend to see details.
                  </div>
                </div>
              )
            }

            // ── Group header ────────────────────────────────────────────────
            if (item.kind === 'group-header') {
              return (
                <div
                  key={`gh:${item.groupKey}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                >
                  <PathsGroup
                    group={item.group}
                    isExpanded={!!expandedGroups[item.groupKey]}
                    onToggle={() => onToggleGroup(item.groupKey)}
                  >
                    {null}
                  </PathsGroup>
                </div>
              )
            }

            // ── Path row ────────────────────────────────────────────────────
            if (item.kind === 'path-row') {
              return (
                <div
                  key={`pr:${item.path.path_hash}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${virtualRow.start}px)`,
                  }}
                >
                  <PathRow
                    path={item.path}
                    group={group}
                    onSelect={() => onSelectPath(item.path.path_hash)}
                  />
                </div>
              )
            }

            return null
          })}
        </div>
      </div>
    </div>
  )
}

// ─── Tab bar ──────────────────────────────────────────────────────────────────

interface TabBarProps {
  activeTab: TabId
  onTabChange: (tab: TabId) => void
  endpointCount: number
  orphanCount: number
  orphanLoading: boolean
}

function TabBar({ activeTab, onTabChange, endpointCount, orphanCount, orphanLoading }: TabBarProps) {
  const tabs: { id: TabId; label: string; count: number | null; loading?: boolean }[] = [
    { id: 'endpoints', label: 'Endpoints', count: endpointCount },
    { id: 'orphan-callers', label: 'Orphan Callers', count: orphanLoading ? null : orphanCount, loading: orphanLoading },
  ]

  return (
    <div className="flex items-center gap-0 border-b border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950 flex-shrink-0">
      {tabs.map((tab) => {
        const isActive = activeTab === tab.id
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={isActive}
            onClick={() => onTabChange(tab.id)}
            className={[
              'flex items-center gap-1.5 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
              isActive
                ? 'border-sky-500 text-sky-600 dark:text-sky-400'
                : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 hover:border-slate-300 dark:hover:border-slate-600',
            ].join(' ')}
          >
            {tab.label}
            {tab.loading ? (
              <span className="inline-flex items-center justify-center px-1.5 py-0.5 text-[10px] rounded-full bg-slate-100 dark:bg-slate-800 text-slate-400 min-w-[20px]">
                …
              </span>
            ) : tab.count !== null && tab.count > 0 ? (
              <span
                className={[
                  'inline-flex items-center justify-center px-1.5 py-0.5 text-[10px] rounded-full min-w-[20px]',
                  isActive
                    ? 'bg-sky-100 dark:bg-sky-900/50 text-sky-700 dark:text-sky-300'
                    : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400',
                ].join(' ')}
              >
                {tab.count}
              </span>
            ) : null}
          </button>
        )
      })}
    </div>
  )
}

/**
 * Surface 4 — API & Contracts Explorer.
 *
 * Layout:
 *   [PathList + detail panel via Outlet]
 *
 * Tab structure (v2 — #1093):
 *   Endpoints tab (default):
 *     - Backend handler definitions only (no frontend FETCH call-site rows)
 *     - Grouped by controller / module / framework (from #918)
 *     - Flat list toggle (virtualized via @tanstack/react-virtual)
 *     - Free-text search
 *     - NO chip filters (dropped per #1082 user feedback)
 *
 *   Orphan Callers tab:
 *     - Frontend FETCH call sites with no backend handler match
 *     - Sorted by severity (no_handler_found > dynamic_baseurl > template_literal)
 *     - Click → navigate to Pending surface with candidate pre-selected
 *     - Gracefully handles 404 (backend #1091 pending)
 *
 * Keyboard shortcuts:
 *   /  → focus search input (Endpoints tab only)
 *   ↑↓ → move selection in path list
 *   Enter → drill into selected path
 *
 * Performance (#1303):
 *   Both flat and grouped views are virtualized via @tanstack/react-virtual.
 *   The grouped view flattens the group-header + path-row tree into a single
 *   VirtualItem[] array, rendering only visible rows + overscan buffer (~8 rows).
 *   A sticky header overlay tracks the current section during scroll.
 */
export function PathsRoute() {
  const { group = 'fixture-a' } = useParams<{ group: string }>()
  const { filters, setFilter, clearFilters } = usePathFilters()
  const { data, isLoading, isFetching } = usePathList(group, filters)
  const {
    data: orphanData,
    isLoading: orphanLoading,
  } = useOrphanCallers(group)
  const navigate = useNavigate()
  const searchRef = useRef<HTMLDivElement>(null)
  const groupedListRef = useRef<HTMLDivElement>(null)

  // ── Active tab ─────────────────────────────────────────────────────────────
  const [activeTab, setActiveTab] = useState<TabId>('endpoints')

  // Reset to endpoints tab when navigating to a different group
  const prevGroupRef = useRef<string>(group)
  useEffect(() => {
    if (group !== prevGroupRef.current) {
      prevGroupRef.current = group
      setActiveTab('endpoints')
    }
  }, [group])

  // ── View mode (flat / grouped) ─────────────────────────────────────────────
  const [isFlat, setIsFlat] = useState<boolean>(readFlatPref)

  const toggleFlat = useCallback(() => {
    setIsFlat((prev) => {
      const next = !prev
      try { localStorage.setItem(LS_FLAT_KEY, String(next)) } catch { /* noop */ }
      return next
    })
  }, [])

  // ── Group expand/collapse state ───────────────────────────────────────────
  // Map: groupKey → expanded boolean. Default: all collapsed.
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})

  const toggleGroup = useCallback((key: string) => {
    setExpandedGroups((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  // ── Backend expand/collapse state (#1219) ─────────────────────────────────
  // Map: backendName → expanded boolean.
  // Initialised lazily from localStorage; small backends default to collapsed.
  const [expandedBackends, setExpandedBackends] = useState<Record<string, boolean>>({})

  const initBackendExpanded = useCallback((backends: BackendInfo[]) => {
    const next: Record<string, boolean> = {}
    for (const b of backends) {
      const defaultCollapsed = b.count < BACKEND_SMALL_THRESHOLD
      next[b.name] = !readCollapsed(b.name, defaultCollapsed)
    }
    setExpandedBackends(next)
  }, [])

  const toggleBackend = useCallback((name: string) => {
    setExpandedBackends((prev) => {
      const next = { ...prev, [name]: !prev[name] }
      writeCollapsed(name, !next[name])
      return next
    })
  }, [])

  // ── Compare backends stub (#1219) ─────────────────────────────────────────
  const [compareOpen, setCompareOpen] = useState(false)

  // Filter to backend definitions only — drop any frontend-only FETCH call-site rows.
  // PathRow.endpoints is the discriminant: entries with is_webhook=false and at least one
  // handler in a backend framework are backend defs. In practice the backend already
  // scopes /api/paths to handler entities; we keep the client-side guard for clarity.
  const allPaths = data?.paths ?? []
  const paths = allPaths  // backend already filters; no extra client filter needed

  const totalLabel = data ? `${data.total} paths` : ''

  // ── Backend info (#1218/#1219) ────────────────────────────────────────────
  // Use the backends[] array from Sub-B if available, else undefined (single-backend fallback).
  const backends = data?.backends

  // Derive whether we are in multi-backend mode
  const isMultiBackend = backends !== undefined && backends.length > 1

  // Initialise backend expand state when backends change
  const prevBackendNamesRef = useRef<string>('')
  useEffect(() => {
    if (!backends) return
    const key = backends.map((b) => b.name).join(',')
    if (key !== prevBackendNamesRef.current) {
      prevBackendNamesRef.current = key
      initBackendExpanded(backends)
    }
  }, [backends, initBackendExpanded])

  // ── Group computation ─────────────────────────────────────────────────────
  const groups = useMemo(() => groupPaths(paths), [paths])

  // When the filter (q) changes, auto-expand groups that have matches.
  // When the filter is cleared, collapse all groups.
  const filterQ = filters.q?.toLowerCase() ?? ''
  useEffect(() => {
    if (isFlat) return
    if (!filterQ) {
      setExpandedGroups({})
      return
    }
    const updates: Record<string, boolean> = {}
    for (const g of groups) {
      if (g.paths.some((p) => p.path.toLowerCase().includes(filterQ))) {
        updates[g.name] = true
      }
    }
    // For multi-backend, also expand matching backend → controller pairs
    if (isMultiBackend && backends) {
      for (const b of backends) {
        const bGroups = groupPaths(b.paths)
        for (const g of bGroups) {
          if (g.paths.some((p) => p.path.toLowerCase().includes(filterQ))) {
            updates[`${b.name}::${g.name}`] = true
          }
        }
      }
    }
    setExpandedGroups(updates)
  }, [filterQ, groups, isFlat, isMultiBackend, backends])

  // Reset expand state when groups change substantially
  const prevGroupNamesRef = useRef<string>('')
  useEffect(() => {
    const key = groups.map((g) => g.name).join(',')
    if (key !== prevGroupNamesRef.current) {
      prevGroupNamesRef.current = key
      if (!filterQ) {
        setExpandedGroups({})
      }
    }
  }, [groups, filterQ])

  const expandAll = useCallback(() => {
    const all: Record<string, boolean> = {}
    for (const g of groups) all[g.name] = true
    if (isMultiBackend && backends) {
      for (const b of backends) {
        const bGroups = groupPaths(b.paths)
        for (const g of bGroups) {
          all[`${b.name}::${g.name}`] = true
        }
      }
    }
    setExpandedGroups(all)
  }, [groups, isMultiBackend, backends])

  const collapseAll = useCallback(() => setExpandedGroups({}), [])

  // ── Virtual items for grouped list ────────────────────────────────────────
  // Recomputed whenever group/expand state changes. O(n) over visible items only.
  const virtualItems = useMemo(() => {
    if (isFlat) return []
    return buildVirtualItems(
      groups,
      expandedGroups,
      backends,
      expandedBackends,
      isMultiBackend,
      groupPaths,
    )
  }, [isFlat, groups, expandedGroups, backends, expandedBackends, isMultiBackend])

  // ── Keyboard shortcuts ────────────────────────────────────────────────────
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === '/' && document.activeElement?.tagName !== 'INPUT') {
        e.preventDefault()
        const input = searchRef.current?.querySelector('input')
        input?.focus()
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  // Arrow-key navigation across visible [role="row"] elements in the grouped list
  useEffect(() => {
    const list = groupedListRef.current
    if (!list) return
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
      const rows = Array.from(list.querySelectorAll('[role="row"]')) as HTMLElement[]
      const focused = document.activeElement as HTMLElement
      const idx = rows.indexOf(focused)
      if (idx === -1) { rows[0]?.focus(); return }
      if (e.key === 'ArrowDown' && idx < rows.length - 1) { e.preventDefault(); rows[idx + 1].focus() }
      if (e.key === 'ArrowUp' && idx > 0) { e.preventDefault(); rows[idx - 1].focus() }
    }
    list.addEventListener('keydown', handler)
    return () => list.removeEventListener('keydown', handler)
  }, [])

  const orphanCallers = orphanData?.callers ?? []
  const orphanTotal = orphanData?.total ?? 0
  // If we got a response but it's empty AND not loading, we can't distinguish
  // "backend returned 0" from "404 returned empty". The client.ts graceful handler
  // logs a console.info hint; we treat isLoading=false + total=0 as potentially pending
  // only when the data key is missing from the query cache.
  const orphanBackendPending = !orphanLoading && orphanTotal === 0 && orphanCallers.length === 0

  return (
    <div className="flex h-full overflow-hidden">
      {/* Main content — list + detail (full width, no prefix-tree sidebar) */}
      <div className="flex flex-1 overflow-hidden">
        {/* Path list panel */}
        <div className="flex flex-col w-[520px] flex-shrink-0 border-r border-slate-200 dark:border-slate-800 overflow-hidden">
          {/* Search + header */}
          <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-200 dark:border-slate-800 bg-slate-100/80 dark:bg-slate-900/80">
            <div ref={searchRef} className="flex-1">
              <PathSearchInput
                value={filters.q ?? ''}
                onChange={(q) => setFilter('q', q || undefined)}
              />
            </div>
            {activeTab === 'endpoints' && totalLabel && (
              <span className="text-xs text-slate-400 dark:text-slate-500 flex-shrink-0 tabular-nums">
                {isFetching && !isLoading ? '↻ ' : ''}{totalLabel}
              </span>
            )}
            {activeTab === 'endpoints' && (data?.total ?? 0) > 0 && (
              <a
                href={`/api/export/${group}/openapi?format=yaml`}
                download={`${group}-openapi.yaml`}
                title="Download OpenAPI 3.0 spec (YAML)"
                className="flex items-center gap-1 text-xs text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors flex-shrink-0"
                aria-label="Download OpenAPI spec"
              >
                <FileDown className="w-3.5 h-3.5" aria-hidden />
                OpenAPI
              </a>
            )}
          </div>

          {/* Tab bar */}
          <TabBar
            activeTab={activeTab}
            onTabChange={setActiveTab}
            endpointCount={data?.total ?? 0}
            orphanCount={orphanTotal}
            orphanLoading={orphanLoading}
          />

          {/* ── Endpoints tab ────────────────────────────────────────────── */}
          {activeTab === 'endpoints' && (
            <>
              {/* Grouped-view controls — hidden in flat mode */}
              {!isFlat && !isLoading && paths.length > 0 && (
                <div className="flex items-center gap-1 px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-900/60">
                  <button
                    type="button"
                    title="Expand all groups"
                    onClick={expandAll}
                    className="flex items-center gap-1 text-xs text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors"
                  >
                    <ChevronDown className="w-3.5 h-3.5" aria-hidden />
                    Expand all
                  </button>
                  <button
                    type="button"
                    title="Collapse all groups"
                    onClick={collapseAll}
                    className="flex items-center gap-1 text-xs text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors"
                  >
                    <ChevronUp className="w-3.5 h-3.5" aria-hidden />
                    Collapse all
                  </button>
                  <span className="flex-1" />
                  {/* Compare 2 backends — stub UI (#1219) */}
                  {isMultiBackend && (
                    <button
                      type="button"
                      title="Compare 2 backends side-by-side"
                      onClick={() => setCompareOpen(true)}
                      className="flex items-center gap-1 text-xs text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors"
                      aria-label="Compare 2 backends"
                    >
                      <Columns2 className="w-3.5 h-3.5" aria-hidden />
                      Compare
                    </button>
                  )}
                  <button
                    type="button"
                    title="Switch to flat list"
                    onClick={toggleFlat}
                    className="flex items-center gap-1 text-xs text-slate-400 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors"
                    aria-pressed={false}
                  >
                    <List className="w-3.5 h-3.5" aria-hidden />
                    Flat list
                  </button>
                </div>
              )}

              {/* Flat-mode: show toggle to switch back to grouped */}
              {isFlat && !isLoading && paths.length > 0 && (
                <div className="flex items-center gap-1 px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-900/60">
                  <span className="flex-1" />
                  <button
                    type="button"
                    title="Switch to grouped view"
                    onClick={toggleFlat}
                    className="flex items-center gap-1 text-xs text-sky-400 hover:text-sky-300 px-1.5 py-0.5 rounded hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors"
                    aria-pressed={true}
                  >
                    <List className="w-3.5 h-3.5" aria-hidden />
                    Flat list
                  </button>
                </div>
              )}

              {/* Compare backends stub — shown when triggered (#1219) */}
              {compareOpen && isMultiBackend && (
                <div
                  className="mx-3 my-2 p-3 rounded border border-dashed border-slate-300 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/50 text-xs text-slate-500 dark:text-slate-400"
                  role="dialog"
                  aria-label="Compare backends"
                >
                  <div className="flex items-center justify-between mb-1">
                    <span className="font-medium text-slate-700 dark:text-slate-300">Compare backends</span>
                    <button
                      type="button"
                      onClick={() => setCompareOpen(false)}
                      className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-xs"
                      aria-label="Close compare panel"
                    >
                      ✕
                    </button>
                  </div>
                  <p className="text-slate-400 dark:text-slate-500">
                    Side-by-side backend diff — coming soon. Select two backends from the list to compare endpoint coverage.
                  </p>
                </div>
              )}

              {/* Path list */}
              <ErrorBoundary>
                {isLoading ? (
                  <PathListSkeleton count={12} />
                ) : paths.length === 0 ? (
                  <div className="flex-1 overflow-y-auto">
                    <EmptyState
                      icon={Globe}
                      title="No paths match"
                      message="Try adjusting the search."
                      action={
                        <button
                          type="button"
                          className="text-sm text-sky-400 hover:underline"
                          onClick={clearFilters}
                        >
                          Clear search
                        </button>
                      }
                    />
                  </div>
                ) : isFlat ? (
                  /* ── Flat list — virtualized with @tanstack/react-virtual ──── */
                  <VirtualFlatList
                    paths={paths}
                    group={group}
                    onSelect={(hash) => navigate(`/paths/${group}/${hash}`)}
                  />
                ) : (
                  /* ── Grouped list — virtualized (#1303) ─────────────────── */
                  <VirtualGroupedList
                    items={virtualItems}
                    group={group}
                    isMultiBackend={isMultiBackend}
                    expandedGroups={expandedGroups}
                    expandedBackends={expandedBackends}
                    onToggleGroup={toggleGroup}
                    onToggleBackend={toggleBackend}
                    onSelectPath={(hash) => navigate(`/paths/${group}/${hash}`)}
                    listRef={groupedListRef}
                  />
                )}
              </ErrorBoundary>
            </>
          )}

          {/* ── Orphan Callers tab ────────────────────────────────────────── */}
          {activeTab === 'orphan-callers' && (
            <ErrorBoundary>
              <OrphanCallersTab
                group={group}
                callers={orphanCallers}
                isLoading={orphanLoading}
                backendPending={orphanBackendPending}
              />
            </ErrorBoundary>
          )}
        </div>

        {/* Detail panel — rendered by nested route */}
        <div className="flex-1 overflow-hidden bg-white dark:bg-slate-950">
          <ErrorBoundary>
            <Outlet />
          </ErrorBoundary>
        </div>
      </div>
    </div>
  )
}
