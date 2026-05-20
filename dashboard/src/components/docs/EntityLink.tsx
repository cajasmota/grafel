import { useEntityDeepLink } from '@/hooks/docs/useEntityDeepLink'
import { EntityHovercard } from './EntityHovercard'
import type { EntityCard } from '@/types/docs'

interface EntityLinkProps {
  symbol: string
  entityId?: string
  prefetchedCard?: EntityCard
}

/**
 * Renders a backticked code symbol as a clickable deep-link to Surface 1 (Graph Viewer).
 * Wraps in <EntityHovercard> for the hover mini-card.
 *
 * The entityId is resolved from the prefetchedCard if available.
 * If no match is found, renders as plain inline code.
 */
export function EntityLink({ symbol, entityId, prefetchedCard }: EntityLinkProps) {
  const buildLink = useEntityDeepLink()
  const resolvedId = entityId ?? prefetchedCard?.id ?? null

  if (!resolvedId && !prefetchedCard) {
    // No entity match — just render styled code
    return (
      <code className="px-1 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 font-mono text-[0.875em] border border-slate-300 dark:border-slate-700">
        {symbol}
      </code>
    )
  }

  return (
    <EntityHovercard entityId={resolvedId} prefetchedCard={prefetchedCard}>
      <a
        href={resolvedId ? buildLink(resolvedId) : undefined}
        className="inline-flex items-center gap-0.5 px-1 py-0.5 rounded bg-sky-950/60 text-sky-300 font-mono text-[0.875em] border border-sky-800/50 hover:bg-sky-900/60 hover:border-sky-600/60 transition-colors cursor-pointer"
        aria-label={`View ${symbol} in graph`}
        title={`${symbol} — click to open in Graph Viewer`}
      >
        {symbol}
      </a>
    </EntityHovercard>
  )
}
