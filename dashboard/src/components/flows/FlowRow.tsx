import { Workflow, Cpu, Clock, Radio } from 'lucide-react'
import { RepoChip } from '@/components/shared/RepoChip'
import { CrossStackBadge } from './CrossStackBadge'
import type { Process } from '@/types/api'

interface FlowRowProps {
  process: Process
  isSelected?: boolean
  onSelect?: (processId: string) => void
}

const ENTITY_KIND_ICONS: Record<string, React.FC<{ className?: string }>> = {
  http: Workflow,
  kafka_consumer: Radio,
  scheduled: Clock,
  ws_handler: Cpu,
}

const ENTITY_KIND_LABELS: Record<string, string> = {
  http: 'HTTP',
  kafka_consumer: 'Kafka',
  scheduled: 'Scheduled',
  ws_handler: 'WebSocket',
}

export function FlowRow({ process, isSelected = false, onSelect }: FlowRowProps) {
  const KindIcon = ENTITY_KIND_ICONS[process.entity_kind ?? 'http'] ?? Workflow
  const kindLabel = ENTITY_KIND_LABELS[process.entity_kind ?? 'http'] ?? 'HTTP'

  const summary =
    process.chain_labels.length > 0
      ? process.chain_labels.join(' → ')
      : process.label

  const handleClick = () => onSelect?.(process.process_id)

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      handleClick()
    }
  }

  return (
    <div
      role="row"
      tabIndex={0}
      aria-selected={isSelected}
      className={[
        'group flex items-start gap-3 px-4 py-3 border-b border-slate-200 dark:border-slate-800',
        'cursor-pointer hover:bg-slate-200/60 dark:hover:bg-slate-800/60 focus:outline-none focus:bg-slate-800/80',
        'transition-colors duration-75',
        isSelected ? 'bg-slate-200/80 dark:bg-slate-800/80 border-l-2 border-l-sky-500' : '',
      ].filter(Boolean).join(' ')}
      onClick={handleClick}
      onKeyDown={handleKeyDown}
    >
      {/* Kind icon */}
      <span
        className="flex-shrink-0 mt-0.5 text-slate-400 dark:text-slate-500 group-hover:text-slate-600 dark:hover:text-slate-400"
        title={kindLabel}
        aria-label={kindLabel}
      >
        <KindIcon className="w-4 h-4" />
      </span>

      {/* Main content */}
      <div className="flex-1 min-w-0">
        {/* Entry name + repo */}
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-mono text-sm text-slate-800 dark:text-slate-200 truncate max-w-xs" title={process.entry_name}>
            {process.entry_name}
          </span>
          <RepoChip repo={process.repo} />
        </div>

        {/* Chain summary */}
        <p className="mt-1 text-xs text-slate-400 dark:text-slate-500 font-mono truncate" title={summary}>
          {summary}
        </p>
      </div>

      {/* Right-side badges */}
      <div className="flex items-center gap-2 flex-shrink-0">
        {process.cross_stack && (
          <CrossStackBadge />
        )}
        {/* Step count badge */}
        <span
          className="px-1.5 py-0.5 rounded text-xs font-mono bg-slate-200 dark:bg-slate-800 text-slate-400 dark:text-slate-400 border border-slate-300 dark:border-slate-700"
          title={`${process.step_count} steps`}
          aria-label={`${process.step_count} steps`}
        >
          {process.step_count}
        </span>
      </div>
    </div>
  )
}
