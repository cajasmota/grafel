import type { Community } from '@/types/api'

interface CommunityLegendProps {
  communities: Community[]
  colorMap: Map<number, string>
  /** If set, highlight only this community (dim others) */
  highlightId?: number | null
  onHover?: (id: number | null) => void
  /**
   * Called when a community row is clicked — triggers drill-in (#1000).
   * Receives the community id and display name.
   */
  onSelect?: (id: number, name: string) => void
  /** Currently drilled-in community (shows as active/selected state) */
  selectedId?: number | null
  className?: string
}

/**
 * Scrollable color legend for communities.
 * Shows auto_name (TF-IDF) or agent_name (LLM layer) with size count.
 *
 * #1000: clicking a community now triggers onSelect(id, name) for drill-in.
 */
export function CommunityLegend({
  communities,
  colorMap,
  highlightId,
  onHover,
  onSelect,
  selectedId,
  className = '',
}: CommunityLegendProps) {
  const sorted = [...communities].sort((a, b) => b.size - a.size)

  return (
    <div
      className={[
        'flex flex-col gap-0.5 overflow-y-auto max-h-[280px] scrollbar-thin',
        className,
      ].join(' ')}
      role="list"
      aria-label="Community legend"
    >
      {sorted.map((c, idx) => {
        const color = colorMap.get(c.id) ?? '#64748b'
        const name = c.agent_name ?? c.auto_name ?? `Community ${c.id}`
        const dimmed = highlightId !== null && highlightId !== undefined && highlightId !== c.id
        const isSelected = selectedId === c.id
        // Use composite key — community IDs are per-repo integers and can collide
        // across repos in a multi-repo group. Append the display name to guarantee uniqueness.
        const itemKey = `${c.id}-${name}-${idx}`
        return (
          <button
            key={itemKey}
            type="button"
            role="listitem"
            className={[
              'flex items-center gap-2 px-2 py-1 rounded text-left',
              'hover:bg-slate-200/60 dark:hover:bg-slate-800/60 transition-colors text-xs w-full',
              'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              dimmed && !isSelected ? 'opacity-40' : '',
              isSelected ? 'bg-sky-900/30 ring-1 ring-sky-700' : '',
            ].join(' ')}
            onMouseEnter={() => onHover?.(c.id)}
            onMouseLeave={() => onHover?.(null)}
            onClick={() => {
              if (onSelect) {
                // Community drill-in (#1000)
                onSelect(c.id, name)
              } else {
                // Legacy: hover-highlight toggle
                onHover?.(highlightId === c.id ? null : c.id)
              }
            }}
            aria-pressed={isSelected}
            title={onSelect ? `Drill in: ${name}` : name}
          >
            <span
              className="w-2.5 h-2.5 rounded-full shrink-0"
              style={{ background: color }}
              aria-hidden
            />
            <span className="flex-1 truncate text-slate-700 dark:text-slate-300">{name}</span>
            <span className="text-slate-500 dark:text-slate-600 tabular-nums">{c.size.toLocaleString()}</span>
          </button>
        )
      })}
    </div>
  )
}
