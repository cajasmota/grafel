import { useState } from 'react'
import * as Tooltip from '@radix-ui/react-tooltip'
import { useEntityHovercard } from '@/hooks/docs/useEntityHovercard'
import { useEntityDeepLink } from '@/hooks/docs/useEntityDeepLink'
import { KindBadge } from '@/components/shared/KindBadge'
import type { EntityCard } from '@/types/docs'
import type { EntityKind } from '@/types/api'

interface EntityHovercardProps {
  entityId: string | null
  prefetchedCard?: EntityCard
  children: React.ReactNode
}

/**
 * Wraps a child element with a Radix Tooltip that shows mini entity metadata.
 * If prefetchedCard is provided (from hovercards map), no extra fetch is needed.
 * Otherwise, a lazy fetch fires on hover open.
 *
 * A11y:
 * - Esc key closes the hovercard (onEscapeKeyDown handler)
 * - Tooltip.Trigger preserves keyboard focus on the wrapped element
 */
export function EntityHovercard({ entityId, prefetchedCard, children }: EntityHovercardProps) {
  const [open, setOpen] = useState(false)
  const { data: fetched } = useEntityHovercard(entityId, open && !prefetchedCard)
  const buildLink = useEntityDeepLink()
  const card = prefetchedCard ?? fetched

  return (
    <Tooltip.Provider delayDuration={300}>
      <Tooltip.Root open={open} onOpenChange={setOpen}>
        <Tooltip.Trigger asChild>
          <span>{children}</span>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            side="top"
            align="start"
            sideOffset={6}
            className="z-50 rounded-lg bg-slate-100 dark:bg-slate-900 border border-slate-300 dark:border-slate-700 shadow-xl p-3 max-w-xs text-sm"
            onEscapeKeyDown={() => setOpen(false)}
          >
            {!card ? (
              <div className="text-slate-400 dark:text-slate-500 text-xs">Loading…</div>
            ) : (
              <div className="space-y-2">
                <div className="flex items-center gap-2 justify-between">
                  <span className="font-mono font-medium text-sky-300 text-xs">{card.label}</span>
                  <KindBadge kind={card.kind as EntityKind} />
                </div>
                <div className="text-xs text-slate-400 dark:text-slate-500 truncate">
                  {card.source_file}:{card.start_line}
                </div>
                {card.outbound_edges.length > 0 && (
                  <ul className="space-y-1 text-xs">
                    {card.outbound_edges.slice(0, 3).map((e, i) => (
                      <li key={i} className="flex items-center gap-1.5 text-slate-400 dark:text-slate-400">
                        <span className="px-1 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-400 dark:text-slate-500 font-mono text-[10px]">
                          {e.kind}
                        </span>
                        <span className="truncate">{e.target_label}</span>
                      </li>
                    ))}
                  </ul>
                )}
                {entityId && (
                  <a
                    href={buildLink(entityId)}
                    className="block mt-1 text-xs text-sky-400 hover:underline"
                  >
                    Open in Graph →
                  </a>
                )}
              </div>
            )}
            <Tooltip.Arrow className="fill-slate-700" />
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  )
}
