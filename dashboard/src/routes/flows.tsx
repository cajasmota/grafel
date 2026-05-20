import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { AlertCircle } from 'lucide-react'
import { useFlowList } from '@/hooks/flows/useFlowList'
import { useFlowEntryPoints } from '@/hooks/flows/useFlowEntryPoints'
import { useFlowFilters } from '@/hooks/flows/useFlowFilters'
import { FlowEntryPicker } from '@/components/flows/FlowEntryPicker'
import { FlowList } from '@/components/flows/FlowList'
import { FlowDetailPanel } from '@/components/flows/FlowDetailPanel'
import { EmptyFlowState } from '@/components/flows/EmptyFlowState'
import { FlowListSkeleton } from '@/components/shared/LoadingState'
import type { Process } from '@/types/api'

export function FlowsRoute() {
  const { group } = useParams<{ group: string }>()
  const [searchQuery, setSearchQuery] = useState('')

  const {
    filters,
    selectedProcessId,
    setEntry,
    setCrossStackOnly,
    setSelectedProcessId,
    clearFilters,
  } = useFlowFilters()

  // Entry point picker — debounced search
  const {
    entryPoints,
    recent,
    isLoading: entryLoading,
    recordRecent,
  } = useFlowEntryPoints(group ?? '', searchQuery)

  // Flow list — filtered by current entry selection
  const {
    data: flowData,
    isLoading: listLoading,
    error: listError,
    refetch,
  } = useFlowList(group ?? '', {
    entry: filters.entry,
    cross_stack_only: filters.cross_stack_only,
    repo: filters.repo,
    limit: 50,
  })

  const processes = flowData?.processes ?? []
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
      {/* Left panel: entry picker + flow list */}
      <div
        className={[
          'flex flex-col border-r border-slate-800 bg-slate-950',
          'transition-[width] duration-200',
          hasSelection ? 'w-[360px] min-w-[280px]' : 'flex-1',
        ].join(' ')}
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
          <div className="flex items-center gap-2 px-4 py-2 border-b border-slate-800 bg-slate-900/60 text-xs text-slate-400">
            <span className="flex-1 truncate font-mono">
              Entry: <span className="text-sky-400">{filters.entry}</span>
            </span>
            <button
              type="button"
              onClick={clearFilters}
              className="text-slate-600 hover:text-slate-300 transition-colors px-1"
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
            <p className="text-sm text-slate-400">{listError.message}</p>
            <button
              type="button"
              onClick={() => refetch()}
              className="px-3 py-1.5 rounded text-sm bg-slate-800 text-slate-300 hover:bg-slate-700 transition-colors"
            >
              Retry
            </button>
          </div>
        ) : processes.length === 0 && filters.entry ? (
          <div className="flex-1 flex items-center justify-center">
            <EmptyFlowState />
          </div>
        ) : processes.length === 0 ? (
          <div className="flex-1 flex items-center justify-center">
            <EmptyFlowState />
          </div>
        ) : (
          <FlowList
            processes={processes}
            selectedProcessId={selectedProcessId}
            onSelectProcess={handleSelectProcess}
          />
        )}
      </div>

      {/* Right panel: flow detail */}
      {hasSelection && (
        <div className="flex-1 overflow-hidden">
          <FlowDetailPanel
            group={group}
            processId={selectedProcessId!}
            onClose={() => setSelectedProcessId(null)}
          />
        </div>
      )}

      {/* Empty right side hint */}
      {!hasSelection && processes.length > 0 && (
        <div className="hidden xl:flex flex-1 items-center justify-center text-slate-600 text-sm">
          Select a flow to see its chain
        </div>
      )}
    </div>
  )
}
