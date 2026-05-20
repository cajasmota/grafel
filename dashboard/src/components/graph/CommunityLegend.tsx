import type { Community } from '@/types/api'

interface CommunityLegendProps {
  communities: Community[]
  colorMap: Map<number, string>
  /** If set, highlight only this community (dim others) */
  highlightId?: number | null
  onHover?: (id: number | null) => void
  className?: string
}

/**
 * Scrollable color legend for communities.
 * Shows auto_name (TF-IDF) or agent_name (LLM layer) with size count.
 */
export function CommunityLegend({
  communities,
  colorMap,
  highlightId,
  onHover,
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
      {sorted.map((c) => {
        const color = colorMap.get(c.id) ?? '#64748b'
        const name = c.agent_name ?? c.auto_name ?? `Community ${c.id}`
        const dimmed = highlightId !== null && highlightId !== undefined && highlightId !== c.id
        return (
          <button
            key={c.id}
            type="button"
            role="listitem"
            className={[
              'flex items-center gap-2 px-2 py-1 rounded text-left',
              'hover:bg-slate-200/60 dark:hover:bg-slate-800/60 transition-colors text-xs w-full',
              'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-sky-400',
              dimmed ? 'opacity-40' : '',
            ].join(' ')}
            onMouseEnter={() => onHover?.(c.id)}
            onMouseLeave={() => onHover?.(null)}
            onClick={() => onHover?.(highlightId === c.id ? null : c.id)}
            aria-pressed={highlightId === c.id}
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
