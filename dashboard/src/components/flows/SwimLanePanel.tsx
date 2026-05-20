/**
 * Horizontal swim-lane visualization.
 * Each column = one repo. Steps positioned in their repo's lane.
 * Phantom-edge crossings are drawn as dashed connectors with a different color.
 */
import { RepoChip } from '@/components/shared/RepoChip'
import { ChainStep } from './ChainStep'
import { repoColor } from '@/lib/colors'
import type { ProcessStep, SwimLaneEntry, EntityKind } from '@/types/api'

interface SwimLanePanelProps {
  lanes: SwimLaneEntry[]
  /** All steps in original chain order (needed to render cross-lane arrows) */
  allSteps: ProcessStep[]
  focusedStepIndex: number | null
  onStepClick: (step: ProcessStep) => void
  onStepFocus: (index: number) => void
}

/** Entity kind inferred from edge kind or position */
function inferEntityKind(step: ProcessStep, isFirst: boolean): EntityKind {
  if (isFirst) return 'Endpoint'
  if (step.edge_kind === 'ACCESSES_TABLE') return 'DataAccess'
  if (step.edge_kind === 'WS_SUBSCRIBES_TO' || step.edge_kind === 'WS_EMITS') return 'Event'
  if (step.edge_kind === 'PUBLISHES_TO' || step.edge_kind === 'SUBSCRIBES_TO') return 'MessageTopic'
  return 'Function'
}

export function SwimLanePanel({
  lanes,
  allSteps,
  focusedStepIndex,
  onStepClick,
  onStepFocus,
}: SwimLanePanelProps) {
  if (lanes.length === 0) return null

  return (
    <div
      className="overflow-x-auto"
      role="region"
      aria-label="Swim-lane process visualization"
    >
      <div
        className="flex gap-0 min-w-max"
        style={{ minWidth: `${lanes.length * 240}px` }}
      >
        {lanes.map((lane, laneIdx) => {
          const colors = repoColor(lane.repo)
          const isLastLane = laneIdx === lanes.length - 1

          return (
            <div
              key={lane.repo}
              className={[
                'flex flex-col min-w-[240px] border-r border-slate-800',
                isLastLane ? 'border-r-0' : '',
              ].join(' ')}
            >
              {/* Lane header */}
              <div
                className={[
                  'flex items-center gap-2 px-3 py-2',
                  'border-b border-slate-800 bg-slate-900/60',
                ].join(' ')}
              >
                <span className={`w-2 h-2 rounded-full ${colors.dot}`} aria-hidden />
                <RepoChip repo={lane.repo} />
                <span className="ml-auto text-[10px] text-slate-600 font-mono">
                  {lane.steps.length} step{lane.steps.length !== 1 ? 's' : ''}
                </span>
              </div>

              {/* Steps */}
              <div className="flex flex-col p-3 gap-0.5">
                {lane.steps.map((step) => {
                  const isFirst = step.step_index === 0
                  const isPhantom = step.edge_kind === 'FETCHES'
                  return (
                    <ChainStep
                      key={step.entity_id}
                      step={step}
                      entityKind={inferEntityKind(step, isFirst)}
                      isFocused={focusedStepIndex === step.step_index}
                      isPhantom={isPhantom}
                      onClick={onStepClick}
                      onFocus={() => onStepFocus(step.step_index)}
                    />
                  )
                })}
              </div>
            </div>
          )
        })}
      </div>

      {/* Cross-lane phantom edge indicator */}
      {allSteps.some((s) => s.edge_kind === 'FETCHES') && (
        <div className="px-3 py-2 border-t border-slate-800 bg-slate-900/40">
          <span className="text-[10px] text-violet-400 flex items-center gap-1.5">
            <span className="inline-block w-8 border-t border-dashed border-violet-500" aria-hidden />
            Phantom edge (cross-repo boundary)
          </span>
        </div>
      )}
    </div>
  )
}
