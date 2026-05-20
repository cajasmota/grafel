import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { useParams, Outlet, useNavigate } from 'react-router-dom'
import { ChevronUp, ChevronDown, List, Globe } from 'lucide-react'
import { PathRow } from '@/components/paths/PathRow'
import { PathsGroup } from '@/components/paths/PathsGroup'
import { PathFilterPanel } from '@/components/paths/PathFilterPanel'
import { PathSearchInput } from '@/components/paths/PathSearchInput'
import { Pagination } from '@/components/paths/Pagination'
import { EmptyState } from '@/components/shared/EmptyState'
import { PathListSkeleton } from '@/components/shared/LoadingState'
import { ErrorBoundary } from '@/components/shared/ErrorBoundary'
import { usePathList } from '@/hooks/paths/usePathList'
import { usePathFilters } from '@/hooks/paths/usePathFilters'
import { groupPaths } from '@/lib/groupPaths'

// ─── localStorage key for flat/grouped preference ────────────────────────────
const LS_FLAT_KEY = 'paths-view-flat'

function readFlatPref(): boolean {
  try {
    return localStorage.getItem(LS_FLAT_KEY) === 'true'
  } catch {
    return false
  }
}

/**
 * Surface 4 — API & Contracts Explorer.
 *
 * Layout:
 *   [PathTreeSidebar] | [PathList + detail panel via Outlet]
 *
 * Grouped view (default):
 *   - Endpoints grouped by controller/module/prefix
 *   - All groups collapsed by default
 *   - Filter input auto-expands matching groups
 *   - Expand all / Collapse all / Flat list controls
 *
 * Keyboard shortcuts:
 *   /  → focus search input
 *   ↑↓ → move selection in path list
 *   Enter → drill into selected path
 */
export function PathsRoute() {
  const { group = 'fixture-a' } = useParams<{ group: string }>()
  const { filters, setFilter, clearFilters } = usePathFilters()
  const { data, isLoading, isFetching } = usePathList(group, filters)
  const navigate = useNavigate()
  const searchRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  // ── View mode ─────────────────────────────────────────────────────────────
  const [isFlat, setIsFlat] = useState<boolean>(readFlatPref)

  const toggleFlat = useCallback(() => {
    setIsFlat((prev) => {
      const next = !prev
      try { localStorage.setItem(LS_FLAT_KEY, String(next)) } catch { /* noop */ }
      return next
    })
  }, [])

  // ── Group expand/collapse state ───────────────────────────────────────────
  // Map: groupName → expanded boolean. Default: all collapsed.
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})

  const toggleGroup = useCallback((name: string) => {
    setExpandedGroups((prev) => ({ ...prev, [name]: !prev[name] }))
  }, [])

  const paths = data?.paths ?? []
  const totalLabel = data ? `${data.total} paths` : ''

  // ── Group computation ─────────────────────────────────────────────────────
  const groups = useMemo(() => groupPaths(paths), [paths])

  // When the filter (q) changes, auto-expand groups that have matches.
  // When the filter is cleared, collapse all groups.
  const filterQ = filters.q?.toLowerCase() ?? ''
  useEffect(() => {
    if (isFlat) return
    if (!filterQ) {
      // Filter cleared — collapse everything
      setExpandedGroups({})
      return
    }
    const updates: Record<string, boolean> = {}
    for (const g of groups) {
      if (g.paths.some((p) => p.path.toLowerCase().includes(filterQ))) {
        updates[g.name] = true
      }
    }
    setExpandedGroups(updates)
  }, [filterQ, groups, isFlat])

  // Reset expand state when groups change substantially (e.g. navigating to a
  // different fixture group). Skip reset if a text filter is active, because the
  // auto-expand effect above handles that case.
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
    setExpandedGroups(all)
  }, [groups])

  const collapseAll = useCallback(() => setExpandedGroups({}), [])

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

  useEffect(() => {
    const list = listRef.current
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

  return (
    <div className="flex h-full overflow-hidden">
      {/* Main content — list + detail (full width, no prefix-tree sidebar) */}
      <div className="flex flex-1 overflow-hidden">
        {/* Path list panel */}
        <div className="flex flex-col w-[520px] flex-shrink-0 border-r border-slate-800 overflow-hidden">
          {/* Search + header */}
          <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-800 bg-slate-900/80">
            <div ref={searchRef} className="flex-1">
              <PathSearchInput
                value={filters.q ?? ''}
                onChange={(q) => setFilter('q', q || undefined)}
              />
            </div>
            {totalLabel && (
              <span className="text-xs text-slate-500 flex-shrink-0 tabular-nums">
                {isFetching && !isLoading ? '↻ ' : ''}{totalLabel}
              </span>
            )}
          </div>

          {/* Grouped-view controls — hidden in flat mode */}
          {!isFlat && !isLoading && paths.length > 0 && (
            <div className="flex items-center gap-1 px-3 py-1.5 border-b border-slate-800 bg-slate-900/60">
              <button
                type="button"
                title="Expand all groups"
                onClick={expandAll}
                className="flex items-center gap-1 text-xs text-slate-400 hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-800 transition-colors"
              >
                <ChevronDown className="w-3.5 h-3.5" aria-hidden />
                Expand all
              </button>
              <button
                type="button"
                title="Collapse all groups"
                onClick={collapseAll}
                className="flex items-center gap-1 text-xs text-slate-400 hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-800 transition-colors"
              >
                <ChevronUp className="w-3.5 h-3.5" aria-hidden />
                Collapse all
              </button>
              <span className="flex-1" />
              <button
                type="button"
                title="Switch to flat list"
                onClick={toggleFlat}
                className="flex items-center gap-1 text-xs text-slate-400 hover:text-slate-200 px-1.5 py-0.5 rounded hover:bg-slate-800 transition-colors"
                aria-pressed={false}
              >
                <List className="w-3.5 h-3.5" aria-hidden />
                Flat list
              </button>
            </div>
          )}

          {/* Flat-mode: show toggle to switch back to grouped */}
          {isFlat && !isLoading && paths.length > 0 && (
            <div className="flex items-center gap-1 px-3 py-1.5 border-b border-slate-800 bg-slate-900/60">
              <span className="flex-1" />
              <button
                type="button"
                title="Switch to grouped view"
                onClick={toggleFlat}
                className="flex items-center gap-1 text-xs text-sky-400 hover:text-sky-300 px-1.5 py-0.5 rounded hover:bg-slate-800 transition-colors"
                aria-pressed={true}
              >
                <List className="w-3.5 h-3.5" aria-hidden />
                Flat list
              </button>
            </div>
          )}

          {/* Filters — data-driven chips derived from current paths */}
          <ErrorBoundary>
            <PathFilterPanel
              filters={filters}
              setFilter={setFilter}
              clearFilters={clearFilters}
              paths={paths}
            />
          </ErrorBoundary>

          {/* Path list */}
          <ErrorBoundary>
          <div
            ref={listRef}
            className="flex-1 overflow-y-auto"
            role="grid"
            aria-label="API paths"
            aria-busy={isLoading}
          >
            {isLoading ? (
              <PathListSkeleton count={12} />
            ) : paths.length === 0 ? (
              <EmptyState
                icon={Globe}
                title="No paths match"
                message="Try adjusting the search or filters."
                action={
                  <button
                    type="button"
                    className="text-sm text-sky-400 hover:underline"
                    onClick={clearFilters}
                  >
                    Clear filters
                  </button>
                }
              />
            ) : isFlat ? (
              /* ── Flat list (legacy) ─────────────────────────────────────── */
              <div role="rowgroup">
                {paths.map((path) => (
                  <PathRow
                    key={path.path_hash}
                    path={path}
                    group={group}
                    onSelect={() => navigate(`/paths/${group}/${path.path_hash}`)}
                  />
                ))}
              </div>
            ) : (
              /* ── Grouped list ────────────────────────────────────────────── */
              <div>
                {groups.map((g) => (
                  <PathsGroup
                    key={g.name}
                    group={g}
                    isExpanded={!!expandedGroups[g.name]}
                    onToggle={() => toggleGroup(g.name)}
                  >
                    {g.paths.map((path) => (
                      <PathRow
                        key={path.path_hash}
                        path={path}
                        group={group}
                        onSelect={() => navigate(`/paths/${group}/${path.path_hash}`)}
                      />
                    ))}
                  </PathsGroup>
                ))}
              </div>
            )}
          </div>

          </ErrorBoundary>

          {/* Pagination */}
          {!isLoading && data && data.total > (data.page_size ?? 50) && (
            <Pagination
              page={data.page}
              pageSize={data.page_size}
              total={data.total}
              hasMore={data.has_more}
              onPageChange={(p) => setFilter('page', p)}
            />
          )}
        </div>

        {/* Detail panel — rendered by nested route */}
        <div className="flex-1 overflow-hidden bg-slate-950">
          <ErrorBoundary>
            <Outlet />
          </ErrorBoundary>
        </div>
      </div>
    </div>
  )
}
