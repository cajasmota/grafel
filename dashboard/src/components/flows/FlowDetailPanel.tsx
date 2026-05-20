/**
 * Full-chain detail panel, opened when a flow row is clicked.
 * Shows swim-lane visualization, focused step source, and process metadata.
 */
import { X, ArrowLeftRight } from 'lucide-react'
import * as Tabs from '@radix-ui/react-tabs'
import { RepoChip } from '@/components/shared/RepoChip'
import { CrossStackBadge } from './CrossStackBadge'
import { SwimLanePanel } from './SwimLanePanel'
import { ChainStepDetail } from './ChainStepDetail'
import { EmptyState } from '@/components/shared/EmptyState'
import { CardSkeleton } from '@/components/shared/LoadingState'
import { useFlowDetail } from '@/hooks/flows/useFlowDetail'
import { useSwimLaneLayout } from '@/hooks/flows/useSwimLaneLayout'
import { useChainNavigation } from '@/hooks/flows/useChainNavigation'
import type { ProcessStep } from '@/types/api'
import { useState } from 'react'

interface FlowDetailPanelProps {
  group: string
  processId: string
  onClose?: () => void
}

export function FlowDetailPanel({ group, processId, onClose }: FlowDetailPanelProps) {
  const { data, isLoading, error, refetch } = useFlowDetail(group, processId)
  const [selectedStep, setSelectedStep] = useState<ProcessStep | null>(null)

  const lanes = useSwimLaneLayout(data?.process.steps)

  const { focusedIndex, handleKeyDown, focusStep } = useChainNavigation(
    data?.process.steps,
    (step) => setSelectedStep(step),
  )

  if (isLoading) {
    return (
      <div className="h-full flex flex-col p-4 gap-4">
        <CardSkeleton />
        <CardSkeleton />
      </div>
    )
  }

  if (error || !data) {
    return (
      <EmptyState
        title="Could not load flow"
        message={error?.message ?? 'Flow detail unavailable.'}
        action={
          <button
            type="button"
            onClick={() => refetch()}
            className="px-3 py-1.5 rounded text-sm bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-700 transition-colors"
          >
            Retry
          </button>
        }
      />
    )
  }

  const { process } = data
  const steps = process.steps ?? []

  return (
    <div
      className="flex flex-col h-full overflow-hidden"
      onKeyDown={handleKeyDown}
      tabIndex={-1}
      aria-label={`Flow detail: ${process.label}`}
    >
      {/* Header */}
      <div className="flex items-start gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800 bg-slate-100/80 dark:bg-slate-900/80 flex-shrink-0">
        <div className="flex-1 min-w-0">
          <h2 className="font-mono text-sm font-semibold text-slate-800 dark:text-slate-200 truncate">
            {process.label}
          </h2>
          <div className="flex items-center gap-2 mt-1 flex-wrap">
            <RepoChip repo={process.repo} />
            {process.cross_stack && <CrossStackBadge />}
            <span className="text-xs text-slate-400 dark:text-slate-500 font-mono">
              {process.step_count} steps
            </span>
          </div>
        </div>
        {onClose && (
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 rounded text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 hover:bg-slate-200 dark:hover:bg-slate-800 transition-colors flex-shrink-0"
            aria-label="Close flow detail"
          >
            <X className="w-4 h-4" />
          </button>
        )}
      </div>

      {/* Keyboard nav hint */}
      {steps.length > 0 && (
        <div className="px-4 py-1.5 border-b border-slate-200 dark:border-slate-800 bg-slate-100/60 dark:bg-slate-950/60 flex-shrink-0">
          <p className="text-[10px] text-slate-500 dark:text-slate-600">
            ↑ ↓ navigate steps · Enter expand source
          </p>
        </div>
      )}

      {/* Tabs: Swim lane + Chain list */}
      <Tabs.Root defaultValue="lanes" className="flex flex-col flex-1 overflow-hidden">
        <Tabs.List
          className="flex border-b border-slate-200 dark:border-slate-800 px-4 bg-slate-100/60 dark:bg-slate-900/60 flex-shrink-0"
          aria-label="Flow detail views"
        >
          <FlowTabTrigger value="lanes">
            <ArrowLeftRight className="w-3.5 h-3.5" aria-hidden />
            Swim Lanes
          </FlowTabTrigger>
          <FlowTabTrigger value="chain">
            Chain ({steps.length})
          </FlowTabTrigger>
        </Tabs.List>

        <div className="flex-1 overflow-y-auto">
          {/* Swim lane view */}
          <Tabs.Content value="lanes" className="p-0">
            {lanes.length === 0 ? (
              <EmptyState
                title="No chain steps"
                message="Detailed steps are loaded on demand. Switch to Chain view or select a flow with steps populated."
              />
            ) : (
              <SwimLanePanel
                lanes={lanes}
                allSteps={steps}
                focusedStepIndex={focusedIndex}
                onStepClick={(step) => {
                  setSelectedStep(step)
                  focusStep(step.step_index)
                }}
                onStepFocus={(idx) => focusStep(idx)}
              />
            )}
          </Tabs.Content>

          {/* Linear chain view */}
          <Tabs.Content value="chain" className="p-4 space-y-2">
            {steps.length === 0 ? (
              <EmptyState title="No steps" message="This process has no expanded chain steps." />
            ) : (
              steps.map((step, idx) => (
                <div
                  key={step.entity_id}
                  className={[
                    'rounded-lg border transition-colors cursor-pointer',
                    focusedIndex === idx
                      ? 'border-sky-700 bg-sky-900/20'
                      : 'border-slate-200 dark:border-slate-800 hover:border-slate-300 dark:hover:border-slate-700 hover:bg-slate-200/60 dark:hover:bg-slate-900/60',
                  ].join(' ')}
                  onClick={() => { setSelectedStep(step); focusStep(idx) }}
                  role="button"
                  tabIndex={0}
                  aria-label={`Step ${idx + 1}: ${step.label}`}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      setSelectedStep(step)
                      focusStep(idx)
                    }
                  }}
                >
                  <div className="px-3 py-2">
                    <span className="text-[10px] text-slate-500 dark:text-slate-600 font-mono mr-2">{idx + 1}.</span>
                    <span className="font-mono text-xs text-slate-700 dark:text-slate-300">{step.label}</span>
                    <RepoChip repo={step.repo} className="ml-2" />
                  </div>
                </div>
              ))
            )}
          </Tabs.Content>
        </div>
      </Tabs.Root>

      {/* Selected step detail slide-in */}
      {selectedStep && (
        <div className="border-t border-slate-200 dark:border-slate-800 p-4 bg-slate-100/60 dark:bg-slate-900/60 flex-shrink-0 max-h-64 overflow-y-auto">
          <ChainStepDetail
            step={selectedStep}
            sourceCode={data.source_snippets?.[selectedStep.entity_id]}
            onClose={() => setSelectedStep(null)}
          />
        </div>
      )}
    </div>
  )
}

function FlowTabTrigger({ value, children }: { value: string; children: React.ReactNode }) {
  return (
    <Tabs.Trigger
      value={value}
      className={[
        'flex items-center gap-1.5 px-4 py-2.5 text-sm border-b-2 border-transparent transition-colors',
        'text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300',
        'data-[state=active]:border-sky-500 data-[state=active]:text-slate-800 dark:data-[state=active]:text-slate-200',
        'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
      ].join(' ')}
    >
      {children}
    </Tabs.Trigger>
  )
}
