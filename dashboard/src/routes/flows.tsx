/**
 * FlowsRoute — four-tab Flows v2 page (#1149).
 *
 * Tabs:
 *   all        → existing entry-picker + flow list + detail panel (unchanged)
 *   cross-repo → flows filtered to is_cross_repo/cross_stack
 *   dead-ends  → GET /api/flows/{group}/dead-ends (#1145)
 *   truncated  → GET /api/flows/{group}/truncated (#1146)
 *
 * Tab selection is persisted in URL via ?tab=<id> and survives hard refresh.
 * Dead-ends single-step toggle is persisted via ?show_single_step=true.
 */

import { useState, useCallback } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { AlertCircle } from 'lucide-react'
import { useFlowList } from '@/hooks/flows/useFlowList'
import { useFlowEntryPoints } from '@/hooks/flows/useFlowEntryPoints'
import { useFlowFilters } from '@/hooks/flows/useFlowFilters'
import { useFlowDeadEnds } from '@/hooks/flows/useFlowDeadEnds'
import { useFlowTruncated } from '@/hooks/flows/useFlowTruncated'
import { FlowEntryPicker } from '@/components/flows/FlowEntryPicker'
import { FlowList } from '@/components/flows/FlowList'
import { FlowDetailPanel } from '@/components/flows/FlowDetailPanel'
import { EmptyFlowState } from '@/components/flows/EmptyFlowState'
import { FlowListSkeleton } from '@/components/shared/LoadingState'
import {
  TabBar,
  CrossRepoTab,
  DeadEndsTab,
  TruncatedTab,
} from '@/components/flows/FlowsTabs'
import type { FlowTabId } from '@/components/flows/FlowsTabs'
import type { Process } from '@/types/api'

// ─── Valid tab IDs ────────────────────────────────────────────────────────────

const VALID_TABS: FlowTabId[] = ['all', 'cross-repo', 'dead-ends', 'truncated']

function parseTab(raw: string | null): FlowTabId {
  if (raw && VALID_TABS.includes(raw as FlowTabId)) return raw as FlowTabId
  return 'all'
}

// ─── Route component ──────────────────────────────────────────────────────────

export function FlowsRoute() {
  const { group } = useParams<{ group: string }>()
  const [searchParams, setSearchParams] = useSearchParams()

  // ── Tab state (URL-persisted) ───────────────────────────────────────────────
  const activeTab = parseTab(searchParams.get('tab'))
  const showSingleStep = searchParams.get('show_single_step') === 'true'

  const setTab = useCallback(
    (tab: FlowTabId) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev)
        next.set('tab', tab)
        return next
      })
    },
    [setSearchParams],
  )

  const setShowSingleStep = useCallback(
    (v: boolean) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev)
        if (v) next.set('show_single_step', 'true')
        else next.delete('show_single_step')
        return next
      })
    },
    [setSearchParams],
  )

  // ── All Flows tab state ────────────────────────────────────────────────────
  const [searchQuery, setSearchQuery] = useState('')

  const {
    filters,
    selectedProcessId,
    setEntry,
    setCrossStackOnly,
    setSelectedProcessId,
    clearFilters,
  } = useFlowFilters()

  const {
    entryPoints,
    recent,
    isLoading: entryLoading,
    recordRecent,
  } = useFlowEntryPoints(group ?? '', searchQuery)

  const {
    data: flowData,
    isLoading: listLoading,
    error: listError,
    refetch: refetchFlows,
  } = useFlowList(group ?? '', {
    entry: filters.entry,
    cross_stack_only: filters.cross_stack_only,
    repo: filters.repo,
    limit: 50,
  })

  // ── Dead-ends tab data ─────────────────────────────────────────────────────
  const {
    data: deadEndsData,
    isLoading: deadEndsLoading,
    error: deadEndsError,
    refetch: refetchDeadEnds,
  } = useFlowDeadEnds(group ?? '')

  // ── Truncated tab data ─────────────────────────────────────────────────────
  const {
    data: truncatedData,
    isLoading: truncatedLoading,
    error: truncatedError,
    refetch: refetchTruncated,
  } = useFlowTruncated(group ?? '')

  const processes = flowData?.processes ?? []
  const allFlowsTotal = flowData?.total ?? 0
  const crossRepoTotal = processes.filter((p: Process) => p.is_cross_repo || p.cross_stack).length
  const deadEndsTotal = deadEndsData?.total ?? 0
  const truncatedTotal = truncatedData?.total ?? 0

  const hasSelection = !!selectedProcessId

  function handleSelectEntry(process: Process) {
    setEntry(process.entry_id)
    recordRecent(process.entry_id)
    setSelectedProcessId(null)
  }

  function handleSelectProcess(processId: string) {
    setSelectedProcessId(processId)
  }

  if (!group) {
    return (
      <div className="h-full flex flex-col items-center justify-center">
        <EmptyFlowState hasGroup={false} />
      </div>
    )
  }

  return (
    <div className="flex h-full overflow-hidden">
      {/* Left panel: tabs + content */}
      <div
        className={[
          'flex flex-col border-r border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-950',
          'transition-[width] duration-200',
          hasSelection ? 'w-[400px] min-w-[320px]' : 'flex-1',
        ].join(' ')}
      >
        {/* Tab bar */}
        <TabBar
          activeTab={activeTab}
          onTabChange={setTab}
          allCount={allFlowsTotal}
          crossRepoCount={crossRepoTotal}
          deadEndsCount={deadEndsLoading ? null : deadEndsTotal}
          truncatedCount={truncatedLoading ? null : truncatedTotal}
          deadEndsLoading={deadEndsLoading}
          truncatedLoading={truncatedLoading}
        />

        {/* ── All Flows tab ───────────────────────────────────────────────── */}
        {activeTab === 'all' && (
          <div
            role="tabpanel"
            id="flows-tab-panel-all"
            aria-labelledby="flows-tab-all"
            className="flex flex-col flex-1 min-h-0 overflow-hidden"
          >
            {/* Entry picker */}
            <FlowEntryPicker
              searchQuery={searchQuery}
              onSearchChange={setSearchQuery}
              entryPoints={entryPoints}
              recent={recent}
              isLoading={entryLoading}
              crossStackOnly={filters.cross_stack_only ?? false}
              onCrossStackChange={setCrossStackOnly}
              onSelectEntry={handleSelectEntry}
              selectedEntryId={filters.entry}
            />

            {/* Active filter banner */}
            {filters.entry && (
              <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-900/60 text-xs text-slate-400 dark:text-slate-400">
                <span className="flex-1 truncate font-mono">
                  Entry: <span className="text-sky-400">{filters.entry}</span>
                </span>
                <button
                  type="button"
                  onClick={clearFilters}
                  className="text-slate-500 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300 transition-colors px-1"
                  aria-label="Clear entry filter"
                >
                  Clear
                </button>
              </div>
            )}

            {/* Flow list */}
            {listLoading ? (
              <FlowListSkeleton count={6} />
            ) : listError ? (
              <div className="flex flex-col items-center justify-center gap-3 p-8 text-center">
                <AlertCircle className="w-8 h-8 text-red-400" aria-hidden />
                <p className="text-sm text-slate-400 dark:text-slate-400">{listError.message}</p>
                <button
                  type="button"
                  onClick={() => refetchFlows()}
                  className="px-3 py-1.5 rounded text-sm bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-700 transition-colors"
                >
                  Retry
                </button>
              </div>
            ) : (
              <FlowList
                processes={processes}
                selectedProcessId={selectedProcessId}
                onSelectProcess={handleSelectProcess}
              />
            )}
          </div>
        )}

        {/* ── Cross-repo tab ──────────────────────────────────────────────── */}
        {activeTab === 'cross-repo' && (
          <CrossRepoTab
            group={group}
            processes={processes}
            isLoading={listLoading}
            error={listError}
            onRefetch={refetchFlows}
            onSelectProcess={handleSelectProcess}
            selectedProcessId={selectedProcessId}
          />
        )}

        {/* ── Dead-ends tab ───────────────────────────────────────────────── */}
        {activeTab === 'dead-ends' && (
          <DeadEndsTab
            group={group}
            deadEnds={deadEndsData?.dead_ends ?? []}
            isLoading={deadEndsLoading}
            error={deadEndsError}
            onRefetch={refetchDeadEnds}
            onSelectProcess={handleSelectProcess}
            selectedProcessId={selectedProcessId}
            showSingleStep={showSingleStep}
            onToggleSingleStep={setShowSingleStep}
          />
        )}

        {/* ── Truncated tab ───────────────────────────────────────────────── */}
        {activeTab === 'truncated' && (
          <TruncatedTab
            group={group}
            truncated={truncatedData?.truncated ?? []}
            isLoading={truncatedLoading}
            error={truncatedError}
            onRefetch={refetchTruncated}
            onSelectProcess={handleSelectProcess}
            selectedProcessId={selectedProcessId}
          />
        )}
      </div>

      {/* Right panel: flow detail (stub — lazy-loads when #1150 React Flow lands) */}
      {hasSelection && (
        <div className="flex-1 overflow-hidden">
          <FlowDetailPanel
            group={group}
            processId={selectedProcessId!}
            onClose={() => setSelectedProcessId(null)}
          />
        </div>
      )}

      {/* Empty right side hint — only show on All tab */}
      {!hasSelection && activeTab === 'all' && processes.length > 0 && (
        <div className="hidden xl:flex flex-1 items-center justify-center text-slate-500 dark:text-slate-600 text-sm">
          Select a flow to see its chain
        </div>
      )}
    </div>
  )
}
