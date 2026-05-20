interface MultiplicityBadgeProps {
  count: number
  className?: string
}

/**
 * Shows the number of endpoints behind a path (e.g., ViewSet expansion).
 * Hidden when count === 1 (single-entity paths are unambiguous).
 */
export function MultiplicityBadge({ count, className = '' }: MultiplicityBadgeProps) {
  if (count <= 1) return null
  return (
    <span
      className={`inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-slate-200 dark:bg-slate-800 text-slate-400 dark:text-slate-400 border border-slate-300 dark:border-slate-700 ${className}`}
      title={`${count} endpoints behind this path`}
      aria-label={`${count} endpoints`}
    >
      ×{count}
    </span>
  )
}
