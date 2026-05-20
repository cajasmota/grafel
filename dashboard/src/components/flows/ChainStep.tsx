import { KindIcon } from '@/components/shared/KindIcon'
import { RepoChip } from '@/components/shared/RepoChip'
import type { ProcessStep, EntityKind, RelationshipKind } from '@/types/api'

interface ChainStepProps {
  step: ProcessStep
  /** Inferred entity kind for the icon — defaults to Function */
  entityKind?: EntityKind
  isFocused?: boolean
  isPhantom?: boolean
  onClick?: (step: ProcessStep) => void
  onFocus?: () => void
}

const EDGE_KIND_LABELS: Partial<Record<RelationshipKind, string>> = {
  CALLS: 'calls',
  FETCHES: 'fetches',
  ACCESSES_TABLE: 'queries',
  WS_SUBSCRIBES_TO: 'subscribes',
  SUBSCRIBES_TO: 'subscribes',
  PUBLISHES_TO: 'publishes',
  TRANSFORMS: 'transforms',
  ENTRY_POINT_OF: 'entry',
  STEP_IN_PROCESS: 'step',
}

export function ChainStep({
  step,
  entityKind = 'Function',
  isFocused = false,
  isPhantom = false,
  onClick,
  onFocus,
}: ChainStepProps) {
  const edgeLabel = EDGE_KIND_LABELS[step.edge_kind] ?? step.edge_kind.toLowerCase()
  const isEntry = step.edge_kind === 'ENTRY_POINT_OF'

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onClick?.(step)
    }
  }

  return (
    <div className="flex flex-col gap-0.5">
      {/* Edge connector label (skip for entry) */}
      {!isEntry && (
        <div className="flex items-center gap-1 pl-3 py-0.5">
          <span className="w-px h-3 bg-slate-300 dark:bg-slate-700" aria-hidden />
          <span className="text-[10px] text-slate-500 dark:text-slate-600 font-mono">{edgeLabel}</span>
        </div>
      )}

      {/* Step row */}
      <div
        role="button"
        tabIndex={0}
        aria-pressed={isFocused}
        aria-label={`${step.label} in ${step.repo}${isPhantom ? ' (phantom)' : ''}`}
        className={[
          'group flex items-center gap-2 px-2 py-1.5 rounded-md cursor-pointer',
          'transition-colors duration-75',
          'focus:outline-none focus-visible:ring-1 focus-visible:ring-sky-500',
          isFocused
            ? 'bg-sky-900/40 ring-1 ring-sky-700'
            : 'hover:bg-slate-200/60 dark:hover:bg-slate-800/60',
          isPhantom
            ? 'opacity-75 border border-dashed border-violet-700/50'
            : '',
        ].filter(Boolean).join(' ')}
        onClick={() => onClick?.(step)}
        onKeyDown={handleKeyDown}
        onFocus={onFocus}
      >
        <span
          className={[
            'flex-shrink-0',
            isPhantom ? 'text-violet-400' : 'text-slate-400 dark:text-slate-500',
          ].join(' ')}
          aria-hidden
        >
          <KindIcon kind={entityKind} className="w-3.5 h-3.5" />
        </span>

        <span
          className={[
            'flex-1 min-w-0 font-mono text-xs truncate',
            isPhantom ? 'text-violet-300 italic' : 'text-slate-700 dark:text-slate-300',
          ].join(' ')}
          title={step.label}
        >
          {step.label}
        </span>

        <RepoChip repo={step.repo} className="flex-shrink-0 hidden group-hover:inline-flex" />
      </div>
    </div>
  )
}
