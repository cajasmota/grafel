import type { RelationshipKind } from '@/types/api'
import { EdgeBadge } from './EdgeBadge'

interface EdgeKindFiltersProps {
  kinds: string[]
  activeKinds: Set<RelationshipKind>
  onToggle: (kind: RelationshipKind) => void
  onClear: () => void
  className?: string
}

/**
 * Multi-select chip strip — one chip per relationship kind present in graph.
 * Keyboard-navigable: Tab between chips, Space/Enter to toggle.
 */
export function EdgeKindFilters({
  kinds,
  activeKinds,
  onToggle,
  onClear,
  className = '',
}: EdgeKindFiltersProps) {
  const hasActive = activeKinds.size > 0

  return (
    <div
      className={['flex flex-wrap items-center gap-1', className].join(' ')}
      role="group"
      aria-label="Edge kind filters"
    >
      {kinds.map((kind) => {
        const k = kind as RelationshipKind
        const active = !hasActive || activeKinds.has(k)
        return (
          <button
            key={kind}
            type="button"
            onClick={() => onToggle(k)}
            aria-pressed={activeKinds.has(k)}
            className={[
              'rounded transition-opacity focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              !active ? 'opacity-35 hover:opacity-60' : 'opacity-100',
            ].join(' ')}
            title={activeKinds.has(k) ? `Hide ${kind} edges` : `Show only ${kind} edges`}
          >
            <EdgeBadge kind={k} />
          </button>
        )
      })}

      {hasActive && (
        <button
          type="button"
          onClick={onClear}
          className="text-[10px] text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 px-1 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400 rounded transition-colors"
          aria-label="Clear all edge filters"
        >
          clear
        </button>
      )}
    </div>
  )
}
