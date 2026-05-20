import { X } from 'lucide-react'
import type { PathFilters, PathRow } from '@/types/api'

interface PathFilterPanelProps {
  filters: PathFilters
  setFilter: <K extends keyof PathFilters>(key: K, value: PathFilters[K]) => void
  clearFilters: () => void
  /** All paths in the current group — used to derive unique repo/framework chips */
  paths?: PathRow[]
}

export function PathFilterPanel({ filters, setFilter, clearFilters, paths = [] }: PathFilterPanelProps) {
  // Derive unique repos and frameworks from actual data; sort for stable order.
  const uniqueRepos = Array.from(new Set(paths.flatMap((p) => p.repos ?? []))).sort()
  const uniqueFrameworks = Array.from(new Set(paths.flatMap((p) => p.frameworks ?? []))).sort()

  const hasActiveFilters = !!(
    filters.repo || filters.framework || filters.status_code || filters.is_webhook !== undefined
  )

  // Nothing to show if the data hasn't loaded yet
  if (uniqueRepos.length === 0 && uniqueFrameworks.length === 0 && !hasActiveFilters) {
    return null
  }

  return (
    <div className="flex flex-col gap-1.5 px-4 py-2 border-b border-slate-800 text-xs">
      {/* Row 1 — Repos */}
      {uniqueRepos.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-slate-500 shrink-0">Repos:</span>
          {uniqueRepos.map((repo) => (
            <FilterChip
              key={repo}
              label={repo}
              active={filters.repo === repo}
              onClick={() => setFilter('repo', filters.repo === repo ? undefined : repo)}
            />
          ))}
        </div>
      )}

      {/* Row 2 — Frameworks */}
      {uniqueFrameworks.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="text-slate-500 shrink-0">Frameworks:</span>
          {uniqueFrameworks.map((fw) => (
            <FilterChip
              key={fw}
              label={fw}
              active={filters.framework === fw}
              onClick={() => setFilter('framework', filters.framework === fw ? undefined : fw)}
            />
          ))}
        </div>
      )}

      {/* Row 3 — Misc + clear */}
      <div className="flex flex-wrap items-center gap-1.5">
        {/* Webhook toggle */}
        <FilterChip
          label="Webhooks only"
          active={filters.is_webhook === true}
          onClick={() =>
            setFilter('is_webhook', filters.is_webhook === true ? undefined : true)
          }
        />

        {/* Clear all */}
        {hasActiveFilters && (
          <button
            type="button"
            className="ml-auto inline-flex items-center gap-1 px-2 py-0.5 rounded text-slate-400 hover:text-slate-200 hover:bg-slate-800 transition-colors"
            onClick={clearFilters}
            aria-label="Clear all filters"
          >
            <X className="w-3 h-3" />
            Clear
          </button>
        )}
      </div>
    </div>
  )
}

interface FilterChipProps {
  label: string
  active: boolean
  onClick: () => void
}

function FilterChip({ label, active, onClick }: FilterChipProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      className={[
        'px-2 py-0.5 rounded border transition-colors font-mono',
        active
          ? 'bg-sky-900/50 border-sky-500 text-sky-300'
          : 'bg-slate-900 border-slate-700 text-slate-400 hover:border-slate-500 hover:text-slate-300',
      ].join(' ')}
      onClick={onClick}
    >
      {label}
    </button>
  )
}
