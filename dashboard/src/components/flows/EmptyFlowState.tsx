import { Workflow } from 'lucide-react'
import { EmptyState } from '@/components/shared/EmptyState'

interface EmptyFlowStateProps {
  hasGroup?: boolean
}

export function EmptyFlowState({ hasGroup = true }: EmptyFlowStateProps) {
  if (!hasGroup) {
    return (
      <EmptyState
        icon={Workflow}
        title="No group selected"
        message="Select a group from the navigation to explore process flows."
      />
    )
  }

  return (
    <EmptyState
      icon={Workflow}
      title="Select an entry point"
      message="Search for a function, endpoint, or consumer above to trace its process flow through the codebase."
    />
  )
}
