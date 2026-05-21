/**
 * FlowList — renders either a flat list or grouped-by-entry-kind list (#1151).
 *
 * Grouped mode activates when `entry_kind_groups` is present in the response
 * and contains more than one kind. Each group is collapsible with state
 * persisted to localStorage (`archigraph:flows-group-{kind}`).
 *
 * Extras (go-beyond scope of #1151):
 *  - Quick filter input: instant filter across all groups
 *  - Sort-by-complexity-descending toggle
 *  - Cross-repo chip on rows (rendered inside FlowRow via the cross_stack flag)
 *  - TODO(#1157): hover row → highlight entry node in graph (Jarvis hook TBD)
 */

import { useState, useMemo, useCallback } from 'react'
import { Search, ArrowDownNarrowWide } from 'lucide-react'
import { FlowRow } from './FlowRow'
import { FlowsGroup } from './FlowsGroup'
import { EmptyFlowState } from './EmptyFlowState'
import { groupFlows, isDefaultOpen } from '@/lib/groupFlows'
import type { Process, FlowEntryKind, FlowEntryKindGroup } from '@/types/api'

// ── localStorage helpers ──────────────────────────────────────────────────────

const LS_PREFIX = 'archigraph:flows-group-'

function readCollapsed(kind: FlowEntryKind): boolean {
  try {
    const raw = localStorage.getItem(LS_PREFIX + kind)
    if (raw === null) return !isDefaultOpen(kind)
    return raw === 'collapsed'
  } catch {
    return !isDefaultOpen(kind)
  }
}

function writeCollapsed(kind: FlowEntryKind, collapsed: boolean): void {
  try {
    localStorage.setItem(LS_PREFIX + kind, collapsed ? 'collapsed' : 'expanded')
  } catch {
    // ignore quota / private-browsing errors
  }
}

// ── Component ─────────────────────────────────────────────────────────────────

interface FlowListProps {
  processes: Process[]
  selectedProcessId?: string | null
  onSelectProcess: (processId: string) => void
  /** From FlowListResponse.entry_kind_groups (#1148 backend) */
  entryKindGroups?: FlowEntryKindGroup[]
}

export function FlowList({
  processes,
  selectedProcessId,
  onSelectProcess,
  entryKindGroups,
}: FlowListProps) {
  // Quick filter
  const [filterQuery, setFilterQuery] = useState('')

  // Sort by complexity toggle
  const [sortByComplexity, setSortByComplexity] = useState(false)

  // Collapsed-state per kind, lazily initialised from localStorage
  const [collapsed, setCollapsed] = useState<Partial<Record<FlowEntryKind, boolean>>>({})

  const toggleGroup = useCallback((kind: FlowEntryKind) => {
    setCollapsed((prev) => {
      const current = kind in prev ? prev[kind]! : !isDefaultOpen(kind)
      const next = !current
      writeCollapsed(kind, next)
      return { ...prev, [kind]: next }
    })
  }, [])

  const isCollapsed = useCallback((kind: FlowEntryKind): boolean => {
    if (kind in collapsed) return collapsed[kind]!
    return readCollapsed(kind)
  }, [collapsed])

  // Decide whether to use grouped mode
  const useGrouped = Boolean(
    entryKindGroups && entryKindGroups.length > 1,
  )

  // Filter processes
  const lowerQuery = filterQuery.trim().toLowerCase()
  const filteredProcesses = useMemo(() => {
    if (!lowerQuery) return processes
    return processes.filter((p) =>
      p.label.toLowerCase().includes(lowerQuery) ||
      p.entry_name.toLowerCase().includes(lowerQuery) ||
      (p.entry_module ?? '').toLowerCase().includes(lowerQuery) ||
      p.repo.toLowerCase().includes(lowerQuery),
    )
  }, [processes, lowerQuery])

  // For flat mode: optionally sort by complexity desc
  const flatProcesses = useMemo(() => {
    if (!sortByComplexity) return filteredProcesses
    return [...filteredProcesses].sort(
      (a, b) => (b.complexity_score ?? 0) - (a.complexity_score ?? 0),
    )
  }, [filteredProcesses, sortByComplexity])

  // Build groups (memoised)
  const groups = useMemo(
    () => groupFlows(filteredProcesses, entryKindGroups),
    [filteredProcesses, entryKindGroups],
  )

  if (processes.length === 0) {
    return (
      <div className="flex-1 overflow-y-auto">
        <EmptyFlowState />
      </div>
    )
  }

  return (
    <div className="flex flex-col flex-1 min-h-0">
      {/* ── Toolbar ─────────────────────────────────────────────────────── */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950 flex-shrink-0">
        {/* Quick filter */}
        <div className="relative flex-1 min-w-0">
          <Search
            className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-slate-400 pointer-events-none"
            aria-hidden
          />
          <input
            type="search"
            value={filterQuery}
            onChange={(e) => setFilterQuery(e.target.value)}
            placeholder="Filter flows…"
            aria-label="Filter flows"
            className={[
              'w-full pl-7 pr-2 py-1 text-xs rounded',
              'bg-slate-100 dark:bg-slate-800 text-slate-800 dark:text-slate-200',
              'border border-slate-200 dark:border-slate-700',
              'placeholder:text-slate-400 dark:placeholder:text-slate-600',
              'focus:outline-none focus:ring-1 focus:ring-sky-500',
            ].join(' ')}
            data-testid="flow-filter-input"
          />
        </div>

        {/* Sort by complexity (flat mode only; visible in both but only affects flat) */}
        {!useGrouped && (
          <button
            type="button"
            onClick={() => setSortByComplexity((v) => !v)}
            className={[
              'flex-shrink-0 flex items-center gap-1 px-2 py-1 rounded text-xs border transition-colors',
              sortByComplexity
                ? 'bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border-sky-300 dark:border-sky-700'
                : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700 hover:border-slate-400 dark:hover:border-slate-600',
            ].join(' ')}
            title="Sort by complexity descending"
            aria-pressed={sortByComplexity}
            data-testid="sort-by-complexity"
          >
            <ArrowDownNarrowWide className="w-3.5 h-3.5" aria-hidden />
            Complexity
          </button>
        )}
      </div>

      {/* ── List body ───────────────────────────────────────────────────── */}
      <div
        role="rowgroup"
        aria-label="Process flows"
        className="flex-1 overflow-y-auto"
        data-testid="flow-list-body"
      >
        {filteredProcesses.length === 0 ? (
          <div className="flex items-center justify-center h-24 text-sm text-slate-400 dark:text-slate-600">
            No flows match &ldquo;{filterQuery}&rdquo;
          </div>
        ) : useGrouped ? (
          /* ── Grouped render ─────────────────────────────────────────── */
          groups.map((group) => (
            <FlowsGroup
              key={group.kind}
              kind={group.kind}
              label={group.label}
              priority={group.priority}
              count={group.count}
              isExpanded={!isCollapsed(group.kind)}
              onToggle={() => toggleGroup(group.kind)}
            >
              {group.processes.map((process) => (
                <FlowRow
                  key={process.process_id}
                  process={process}
                  isSelected={process.process_id === selectedProcessId}
                  onSelect={onSelectProcess}
                />
              ))}
            </FlowsGroup>
          ))
        ) : (
          /* ── Flat render ────────────────────────────────────────────── */
          flatProcesses.map((process) => (
            <FlowRow
              key={process.process_id}
              process={process}
              isSelected={process.process_id === selectedProcessId}
              onSelect={onSelectProcess}
            />
          ))
        )}
      </div>
    </div>
  )
}
