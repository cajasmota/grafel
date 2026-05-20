import { FlowRow } from './FlowRow'
import { EmptyFlowState } from './EmptyFlowState'
import type { Process } from '@/types/api'

interface FlowListProps {
  processes: Process[]
  selectedProcessId?: string | null
  onSelectProcess: (processId: string) => void
}

export function FlowList({ processes, selectedProcessId, onSelectProcess }: FlowListProps) {
  if (processes.length === 0) {
    return (
      <div className="flex-1 overflow-y-auto">
        <EmptyFlowState />
      </div>
    )
  }

  return (
    <div
      role="rowgroup"
      aria-label="Process flows"
      className="flex-1 overflow-y-auto"
    >
      {processes.map((process) => (
        <FlowRow
          key={process.process_id}
          process={process}
          isSelected={process.process_id === selectedProcessId}
          onSelect={onSelectProcess}
        />
      ))}
    </div>
  )
}
