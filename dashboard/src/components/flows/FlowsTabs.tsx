/**
 * FlowsTabs — four-tab structure for Flows v2 (#1149).
 *
 * Tabs:
 *   1. All Flows   — existing entry-picker + flow list + detail panel
 *   2. Cross-repo  — filtered to is_cross_repo=true, sorted by step count
 *   3. Dead-ends   — GET /api/flows/{group}/dead-ends (#1145)
 *   4. Truncated   — GET /api/flows/{group}/truncated (#1146)
 *
 * Tab selection is persisted in URL query param: ?tab=all|cross-repo|dead-ends|truncated
 */

import { useState, useMemo } from 'react'
import { AlertCircle, Workflow, ArrowDownUp, Globe } from 'lucide-react'
import { RepoChip } from '@/components/shared/RepoChip'
import { EmptyState } from '@/components/shared/EmptyState'
import { FlowListSkeleton } from '@/components/shared/LoadingState'
import { FlowRow } from '@/components/flows/FlowRow'
import type {
  Process,
  FlowDeadEnd,
  FlowTruncated,
  TruncatedSeverity,
  DeadEndReason,
  TruncatedReason,
} from '@/types/api'

// ─── Tab IDs ──────────────────────────────────────────────────────────────────

export type FlowTabId = 'all' | 'cross-repo' | 'dead-ends' | 'truncated'

// ─── Sort options ─────────────────────────────────────────────────────────────

type SortKey = 'step_count' | 'complexity_score' | 'entry_kind'

// ─── Severity colors ──────────────────────────────────────────────────────────

const SEVERITY_CLASSES: Record<TruncatedSeverity, string> = {
  info: 'bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300 border border-sky-200 dark:border-sky-700',
  warn: 'bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-700',
  error: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 border border-red-200 dark:border-red-700',
}

// ─── Entry kind labels ────────────────────────────────────────────────────────

const ENTRY_KIND_LABELS: Record<string, string> = {
  http: 'HTTP',
  kafka_consumer: 'Kafka',
  scheduled: 'Scheduled',
  ws_handler: 'WebSocket',
}

// ─── Reason labels ────────────────────────────────────────────────────────────

const DEAD_END_REASON_LABELS: Record<DeadEndReason, string> = {
  no_useful_sink: 'No useful sink',
  single_step: 'Single-step',
  unresolved_callee: 'Unresolved callee',
  phantom_terminal: 'Phantom terminal',
  dead_end: 'Dead end',
}

const TRUNCATED_REASON_LABELS: Record<TruncatedReason, string> = {
  unresolved_callee: 'Unresolved callee',
  cycle_detected: 'Cycle detected',
  depth_exceeded: 'Depth exceeded',
  external_boundary: 'External boundary',
  truncated: 'Truncated',
}

// ─── Tab bar ──────────────────────────────────────────────────────────────────

interface TabBarProps {
  activeTab: FlowTabId
  onTabChange: (tab: FlowTabId) => void
  allCount: number
  crossRepoCount: number
  deadEndsCount: number | null
  truncatedCount: number | null
  deadEndsLoading: boolean
  truncatedLoading: boolean
}

function TabBar({
  activeTab,
  onTabChange,
  allCount,
  crossRepoCount,
  deadEndsCount,
  truncatedCount,
  deadEndsLoading,
  truncatedLoading,
}: TabBarProps) {
  const tabs: {
    id: FlowTabId
    label: string
    count: number | null
    loading?: boolean
  }[] = [
    { id: 'all', label: 'All Flows', count: allCount },
    { id: 'cross-repo', label: 'Cross-repo', count: crossRepoCount },
    {
      id: 'dead-ends',
      label: 'Dead-ends',
      count: deadEndsLoading ? null : deadEndsCount,
      loading: deadEndsLoading,
    },
    {
      id: 'truncated',
      label: 'Truncated',
      count: truncatedLoading ? null : truncatedCount,
      loading: truncatedLoading,
    },
  ]

  return (
    <div
      role="tablist"
      aria-label="Flow views"
      className="flex items-center gap-0 border-b border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950 flex-shrink-0"
    >
      {tabs.map((tab) => {
        const isActive = activeTab === tab.id
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={isActive}
            aria-controls={`flows-tab-panel-${tab.id}`}
            id={`flows-tab-${tab.id}`}
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
                aria-label={`${tab.count} items`}
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

// ─── Entry-kind filter chips ──────────────────────────────────────────────────

interface EntryKindChipsProps {
  activeKind: string | null
  availableKinds: string[]
  onKindChange: (kind: string | null) => void
}

function EntryKindChips({ activeKind, availableKinds, onKindChange }: EntryKindChipsProps) {
  if (availableKinds.length <= 1) return null
  return (
    <div
      className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900/60 flex-wrap"
      aria-label="Filter by entry kind"
    >
      <span className="text-xs text-slate-400 dark:text-slate-500 mr-1">Filter:</span>
      <button
        type="button"
        onClick={() => onKindChange(null)}
        className={[
          'px-2.5 py-0.5 rounded-full text-xs transition-colors border',
          activeKind === null
            ? 'bg-sky-100 dark:bg-sky-900/50 text-sky-700 dark:text-sky-300 border-sky-200 dark:border-sky-700'
            : 'bg-white dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700 hover:border-slate-400',
        ].join(' ')}
        aria-pressed={activeKind === null}
      >
        All
      </button>
      {availableKinds.map((kind) => (
        <button
          key={kind}
          type="button"
          onClick={() => onKindChange(kind === activeKind ? null : kind)}
          className={[
            'px-2.5 py-0.5 rounded-full text-xs transition-colors border',
            activeKind === kind
              ? 'bg-sky-100 dark:bg-sky-900/50 text-sky-700 dark:text-sky-300 border-sky-200 dark:border-sky-700'
              : 'bg-white dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700 hover:border-slate-400',
          ].join(' ')}
          aria-pressed={activeKind === kind}
        >
          {ENTRY_KIND_LABELS[kind] ?? kind}
        </button>
      ))}
    </div>
  )
}

// ─── Sort toolbar ─────────────────────────────────────────────────────────────

interface SortToolbarProps {
  sortKey: SortKey
  onSortChange: (k: SortKey) => void
  options: { key: SortKey; label: string }[]
}

function SortToolbar({ sortKey, onSortChange, options }: SortToolbarProps) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900/60">
      <ArrowDownUp className="w-3.5 h-3.5 text-slate-400 dark:text-slate-500" aria-hidden />
      <span className="text-xs text-slate-400 dark:text-slate-500">Sort:</span>
      {options.map((opt) => (
        <button
          key={opt.key}
          type="button"
          onClick={() => onSortChange(opt.key)}
          className={[
            'text-xs px-2 py-0.5 rounded transition-colors',
            sortKey === opt.key
              ? 'bg-slate-200 dark:bg-slate-700 text-slate-700 dark:text-slate-200'
              : 'text-slate-400 dark:text-slate-500 hover:text-slate-600 dark:hover:text-slate-300',
          ].join(' ')}
          aria-pressed={sortKey === opt.key}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}

// ─── Cross-repo tab content ───────────────────────────────────────────────────

interface CrossRepoTabProps {
  group: string
  processes: Process[]
  isLoading: boolean
  error: Error | null
  onRefetch: () => void
  onSelectProcess: (id: string) => void
  selectedProcessId: string | null
}

export function CrossRepoTab({
  processes,
  isLoading,
  error,
  onRefetch,
  onSelectProcess,
  selectedProcessId,
}: CrossRepoTabProps) {
  const [sortKey, setSortKey] = useState<SortKey>('step_count')
  const [activeKind, setActiveKind] = useState<string | null>(null)

  const crossRepoProcesses = useMemo(
    () => processes.filter((p) => p.is_cross_repo || p.cross_stack),
    [processes],
  )

  const availableKinds = useMemo(
    () => [...new Set(crossRepoProcesses.map((p) => p.entity_kind).filter(Boolean) as string[])],
    [crossRepoProcesses],
  )

  const sorted = useMemo(() => {
    const filtered = activeKind
      ? crossRepoProcesses.filter((p) => p.entity_kind === activeKind)
      : crossRepoProcesses
    return [...filtered].sort((a, b) => {
      if (sortKey === 'step_count') return b.step_count - a.step_count
      if (sortKey === 'complexity_score') return (b.complexity_score ?? 0) - (a.complexity_score ?? 0)
      if (sortKey === 'entry_kind') return (a.entity_kind ?? '').localeCompare(b.entity_kind ?? '')
      return 0
    })
  }, [crossRepoProcesses, sortKey, activeKind])

  const sortOptions: { key: SortKey; label: string }[] = [
    { key: 'step_count', label: 'Steps' },
    { key: 'complexity_score', label: 'Complexity' },
    { key: 'entry_kind', label: 'Kind' },
  ]

  return (
    <div
      role="tabpanel"
      id="flows-tab-panel-cross-repo"
      aria-labelledby="flows-tab-cross-repo"
      className="flex flex-col flex-1 min-h-0 overflow-hidden"
    >
      <SortToolbar sortKey={sortKey} onSortChange={setSortKey} options={sortOptions} />
      <EntryKindChips
        activeKind={activeKind}
        availableKinds={availableKinds}
        onKindChange={setActiveKind}
      />

      {isLoading ? (
        <FlowListSkeleton count={6} />
      ) : error ? (
        <ErrorPanel message={error.message} onRetry={onRefetch} />
      ) : sorted.length === 0 ? (
        <EmptyState
          icon={Globe}
          title="No cross-repo flows"
          message="No flows span multiple repositories in this group."
        />
      ) : (
        <div
          role="rowgroup"
          aria-label="Cross-repo flows"
          className="flex-1 overflow-y-auto"
        >
          {sorted.map((p) => (
            <FlowRow
              key={p.process_id}
              process={p}
              isSelected={p.process_id === selectedProcessId}
              onSelect={onSelectProcess}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// ─── Dead-ends tab content ────────────────────────────────────────────────────

interface DeadEndsTabProps {
  group: string
  deadEnds: FlowDeadEnd[]
  isLoading: boolean
  error: Error | null
  onRefetch: () => void
  onSelectProcess: (id: string) => void
  selectedProcessId: string | null
  showSingleStep: boolean
  onToggleSingleStep: (v: boolean) => void
}

export function DeadEndsTab({
  deadEnds,
  isLoading,
  error,
  onRefetch,
  onSelectProcess,
  selectedProcessId,
  showSingleStep,
  onToggleSingleStep,
}: DeadEndsTabProps) {
  const [sortKey, setSortKey] = useState<SortKey>('step_count')
  const [activeKind, setActiveKind] = useState<string | null>(null)

  const availableKinds = useMemo(
    () => [...new Set(deadEnds.map((d) => d.entry_kind).filter(Boolean) as string[])],
    [deadEnds],
  )

  const sorted = useMemo(() => {
    const filtered = deadEnds.filter((d) => {
      if (!showSingleStep && d.reason === 'single_step') return false
      if (activeKind && d.entry_kind !== activeKind) return false
      return true
    })
    return [...filtered].sort((a, b) => {
      if (sortKey === 'step_count') return b.step_count - a.step_count
      if (sortKey === 'entry_kind') return (a.entry_kind ?? '').localeCompare(b.entry_kind ?? '')
      return 0
    })
  }, [deadEnds, sortKey, activeKind, showSingleStep])

  const sortOptions: { key: SortKey; label: string }[] = [
    { key: 'step_count', label: 'Steps' },
    { key: 'entry_kind', label: 'Kind' },
  ]

  return (
    <div
      role="tabpanel"
      id="flows-tab-panel-dead-ends"
      aria-labelledby="flows-tab-dead-ends"
      className="flex flex-col flex-1 min-h-0 overflow-hidden"
    >
      <SortToolbar sortKey={sortKey} onSortChange={setSortKey} options={sortOptions} />
      <EntryKindChips
        activeKind={activeKind}
        availableKinds={availableKinds}
        onKindChange={setActiveKind}
      />
      {/* Single-step toggle */}
      <div className="flex items-center gap-2 px-4 py-1.5 border-b border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900/60">
        <label className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={showSingleStep}
            onChange={(e) => onToggleSingleStep(e.target.checked)}
            className="rounded border-slate-300 dark:border-slate-600 text-sky-500 focus:ring-sky-500"
            aria-label="Show single-step flows"
          />
          Show single-step flows
        </label>
      </div>

      {isLoading ? (
        <FlowListSkeleton count={6} />
      ) : error ? (
        <ErrorPanel message={error.message} onRetry={onRefetch} />
      ) : sorted.length === 0 ? (
        <EmptyState
          icon={Workflow}
          title="No dead-end flows"
          message="All flows resolve to a useful sink — no dead ends detected."
        />
      ) : (
        <div
          role="rowgroup"
          aria-label="Dead-end flows"
          className="flex-1 overflow-y-auto"
        >
          {sorted.map((deadEnd) => (
            <DeadEndRow
              key={deadEnd.process_id}
              deadEnd={deadEnd}
              isSelected={deadEnd.process_id === selectedProcessId}
              onSelect={onSelectProcess}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function DeadEndRow({
  deadEnd,
  isSelected,
  onSelect,
}: {
  deadEnd: FlowDeadEnd
  isSelected: boolean
  onSelect: (id: string) => void
}) {
  return (
    <div
      role="row"
      tabIndex={0}
      aria-selected={isSelected}
      className={[
        'group flex items-start gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800',
        'cursor-pointer hover:bg-slate-200/60 dark:hover:bg-slate-800/60 focus:outline-none focus:bg-slate-200/80 dark:focus:bg-slate-800/80',
        'transition-colors duration-75',
        isSelected ? 'bg-slate-200/80 dark:bg-slate-800/80 border-l-2 border-l-sky-500' : '',
      ].filter(Boolean).join(' ')}
      onClick={() => onSelect(deadEnd.process_id)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect(deadEnd.process_id) }
      }}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-mono text-sm text-slate-800 dark:text-slate-200 truncate max-w-xs" title={deadEnd.entry_name}>
            {deadEnd.entry_name}
          </span>
          <RepoChip repo={deadEnd.repo} />
          {deadEnd.entry_kind && (
            <span className="text-xs px-1.5 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border border-slate-300 dark:border-slate-700">
              {ENTRY_KIND_LABELS[deadEnd.entry_kind] ?? deadEnd.entry_kind}
            </span>
          )}
        </div>
        <div className="mt-1 flex items-center gap-2 flex-wrap">
          {/* Reason badge */}
          <span className="text-xs px-2 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 border border-amber-200 dark:border-amber-700">
            {DEAD_END_REASON_LABELS[deadEnd.reason] ?? deadEnd.reason}
          </span>
          <span className="text-xs text-slate-400 dark:text-slate-500 font-mono">
            {deadEnd.step_count} {deadEnd.step_count === 1 ? 'step' : 'steps'}
          </span>
        </div>
      </div>
    </div>
  )
}

// ─── Truncated tab content ────────────────────────────────────────────────────

interface TruncatedTabProps {
  group: string
  truncated: FlowTruncated[]
  isLoading: boolean
  error: Error | null
  onRefetch: () => void
  onSelectProcess: (id: string) => void
  selectedProcessId: string | null
}

export function TruncatedTab({
  truncated,
  isLoading,
  error,
  onRefetch,
  onSelectProcess,
  selectedProcessId,
}: TruncatedTabProps) {
  const [sortKey, setSortKey] = useState<SortKey>('step_count')
  const [activeKind, setActiveKind] = useState<string | null>(null)

  const availableKinds = useMemo(
    () => [...new Set(truncated.map((t) => t.entry_kind).filter(Boolean) as string[])],
    [truncated],
  )

  const sorted = useMemo(() => {
    const filtered = activeKind
      ? truncated.filter((t) => t.entry_kind === activeKind)
      : truncated
    return [...filtered].sort((a, b) => {
      if (sortKey === 'step_count') return b.step_count - a.step_count
      if (sortKey === 'entry_kind') return (a.entry_kind ?? '').localeCompare(b.entry_kind ?? '')
      return 0
    })
  }, [truncated, sortKey, activeKind])

  const sortOptions: { key: SortKey; label: string }[] = [
    { key: 'step_count', label: 'Steps' },
    { key: 'entry_kind', label: 'Kind' },
  ]

  return (
    <div
      role="tabpanel"
      id="flows-tab-panel-truncated"
      aria-labelledby="flows-tab-truncated"
      className="flex flex-col flex-1 min-h-0 overflow-hidden"
    >
      <SortToolbar sortKey={sortKey} onSortChange={setSortKey} options={sortOptions} />
      <EntryKindChips
        activeKind={activeKind}
        availableKinds={availableKinds}
        onKindChange={setActiveKind}
      />

      {isLoading ? (
        <FlowListSkeleton count={6} />
      ) : error ? (
        <ErrorPanel message={error.message} onRetry={onRefetch} />
      ) : sorted.length === 0 ? (
        <EmptyState
          icon={Workflow}
          title="No truncated flows"
          message="Everything resolves cleanly — no truncated flows detected."
        />
      ) : (
        <div
          role="rowgroup"
          aria-label="Truncated flows"
          className="flex-1 overflow-y-auto"
        >
          {sorted.map((t) => (
            <TruncatedRow
              key={t.process_id}
              truncated={t}
              isSelected={t.process_id === selectedProcessId}
              onSelect={onSelectProcess}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function TruncatedRow({
  truncated,
  isSelected,
  onSelect,
}: {
  truncated: FlowTruncated
  isSelected: boolean
  onSelect: (id: string) => void
}) {
  return (
    <div
      role="row"
      tabIndex={0}
      aria-selected={isSelected}
      className={[
        'group flex items-start gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800',
        'cursor-pointer hover:bg-slate-200/60 dark:hover:bg-slate-800/60 focus:outline-none focus:bg-slate-200/80 dark:focus:bg-slate-800/80',
        'transition-colors duration-75',
        isSelected ? 'bg-slate-200/80 dark:bg-slate-800/80 border-l-2 border-l-sky-500' : '',
      ].filter(Boolean).join(' ')}
      onClick={() => onSelect(truncated.process_id)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect(truncated.process_id) }
      }}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-mono text-sm text-slate-800 dark:text-slate-200 truncate max-w-xs" title={truncated.entry_name}>
            {truncated.entry_name}
          </span>
          <RepoChip repo={truncated.repo} />
          {truncated.entry_kind && (
            <span className="text-xs px-1.5 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border border-slate-300 dark:border-slate-700">
              {ENTRY_KIND_LABELS[truncated.entry_kind] ?? truncated.entry_kind}
            </span>
          )}
        </div>
        <div className="mt-1 flex items-center gap-2 flex-wrap">
          {/* Severity + reason badge */}
          <span className={`text-xs px-2 py-0.5 rounded-full ${SEVERITY_CLASSES[truncated.severity]}`}>
            {TRUNCATED_REASON_LABELS[truncated.reason] ?? truncated.reason}
          </span>
          <span className="text-xs text-slate-400 dark:text-slate-500 font-mono">
            {truncated.step_count} steps · truncated at step {truncated.truncation_step}
          </span>
        </div>
      </div>
    </div>
  )
}

// ─── Error panel ──────────────────────────────────────────────────────────────

function ErrorPanel({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 p-8 text-center">
      <AlertCircle className="w-8 h-8 text-red-400" aria-hidden />
      <p className="text-sm text-slate-400 dark:text-slate-400">{message}</p>
      <button
        type="button"
        onClick={onRetry}
        className="px-3 py-1.5 rounded text-sm bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-700 transition-colors"
      >
        Retry
      </button>
    </div>
  )
}

// ─── Main FlowsTabs export ────────────────────────────────────────────────────

export { TabBar }
export type { TabBarProps }
