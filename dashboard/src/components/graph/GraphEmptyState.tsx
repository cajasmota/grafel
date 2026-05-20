import { Network, Filter, AlertCircle } from 'lucide-react'
import { EmptyState } from '@/components/shared/EmptyState'

interface GraphEmptyStateProps {
  reason?: 'no-group' | 'blocked' | 'filtered'
}

/**
 * Empty state variants for the graph canvas.
 */
export function GraphEmptyState({ reason = 'no-group' }: GraphEmptyStateProps) {
  if (reason === 'blocked') {
    return (
      <EmptyState
        icon={Filter}
        title="Graph too large to render at full resolution"
        message="This group has more than 20 000 entities. Use the edge-kind filters above to narrow the view, or zoom out to explore community clusters."
      />
    )
  }
  if (reason === 'filtered') {
    return (
      <EmptyState
        icon={Filter}
        title="No nodes match current filters"
        message="Try adjusting the edge-kind filters to show more of the graph."
      />
    )
  }
  return (
    <EmptyState
      icon={Network}
      title="No group selected"
      message="Select a group from the top nav to explore its dependency graph."
    />
  )
}

interface GraphLoadingStateProps {
  label?: string
}

/**
 * Gradient skeleton placeholder for the graph canvas.
 */
export function GraphLoadingState({ label = 'Loading graph…' }: GraphLoadingStateProps) {
  return (
    <div className="relative w-full h-full flex items-center justify-center bg-white dark:bg-slate-950">
      {/* Animated gradient circles mimicking a force graph */}
      <div className="absolute inset-0 overflow-hidden" aria-hidden>
        {Array.from({ length: 12 }, (_, i) => (
          <div
            key={i}
            className="absolute rounded-full animate-pulse"
            style={{
              width: `${20 + (i % 4) * 30}px`,
              height: `${20 + (i % 4) * 30}px`,
              top: `${10 + ((i * 7) % 80)}%`,
              left: `${5 + ((i * 13) % 90)}%`,
              background: `hsl(${(i * 37) % 360}deg 40% 20% / 0.6)`,
              animationDelay: `${i * 100}ms`,
            }}
          />
        ))}
      </div>
      <div role="status" aria-live="polite" className="relative z-10 text-center">
        <p className="text-sm text-slate-400 dark:text-slate-400 animate-pulse">{label}</p>
        <span className="sr-only">{label}</span>
      </div>
    </div>
  )
}

interface GraphErrorStateProps {
  message: string
  onRetry?: () => void
}

export function GraphErrorState({ message, onRetry }: GraphErrorStateProps) {
  return (
    <EmptyState
      icon={AlertCircle}
      title="Failed to load graph"
      message={message}
      action={
        onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="px-3 py-1.5 rounded text-sm bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 dark:hover:bg-slate-700 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400"
          >
            Retry
          </button>
        )
      }
    />
  )
}
