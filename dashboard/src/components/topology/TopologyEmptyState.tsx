import { Radio } from 'lucide-react'

interface TopologyEmptyStateProps {
  hasGroup?: boolean
  hasFilters?: boolean
  onClearFilters?: () => void
}

export function TopologyEmptyState({
  hasGroup = true,
  hasFilters = false,
  onClearFilters,
}: TopologyEmptyStateProps) {
  if (!hasGroup) {
    return (
      <div
        role="status"
        className="flex flex-col items-center justify-center gap-3 py-16 text-center"
      >
        <div className="rounded-full bg-slate-800 p-4">
          <Radio className="w-8 h-8 text-slate-500" aria-hidden />
        </div>
        <h3 className="text-base font-semibold text-slate-300">No group selected</h3>
        <p className="max-w-sm text-sm text-slate-500">
          Select a group from the navigation to view its broker topology.
        </p>
      </div>
    )
  }

  if (hasFilters) {
    return (
      <div
        role="status"
        className="flex flex-col items-center justify-center gap-3 py-16 text-center"
      >
        <div className="rounded-full bg-slate-800 p-4">
          <Radio className="w-8 h-8 text-slate-500" aria-hidden />
        </div>
        <h3 className="text-base font-semibold text-slate-300">No topics match filters</h3>
        <p className="max-w-sm text-sm text-slate-500">
          Try selecting a different protocol or clearing the filter.
        </p>
        {onClearFilters && (
          <button
            type="button"
            onClick={onClearFilters}
            className="mt-1 px-3 py-1.5 rounded text-sm bg-slate-800 text-slate-300 hover:bg-slate-700 transition-colors"
          >
            Clear filters
          </button>
        )}
      </div>
    )
  }

  return (
    <div
      role="status"
      className="flex flex-col items-center justify-center gap-3 py-16 text-center"
    >
      <div className="rounded-full bg-slate-800 p-4">
        <Radio className="w-8 h-8 text-slate-500" aria-hidden />
      </div>
      <h3 className="text-base font-semibold text-slate-300">No broker topology found</h3>
      <p className="max-w-sm text-sm text-slate-500">
        No message topics, queues, or channels were indexed for this group.
        Make sure the indexer has run against repos that use Kafka, RabbitMQ, SQS, or similar.
      </p>
    </div>
  )
}
