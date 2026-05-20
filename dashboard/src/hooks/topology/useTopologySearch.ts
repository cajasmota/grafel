import { useMemo } from 'react'
import type { TopologyResponse } from '@/types/api'

export interface TopologySearchResult {
  id: string
  label: string
  protocol: string
  repo: string
  kind: 'topic' | 'queue' | 'channel' | 'nats' | 'graphql'
}

/**
 * Typeahead search over topic/queue/channel/subject names.
 * Pure derivation from already-fetched data — no extra fetch.
 * Returns up to 10 matches sorted by score (prefix match scores higher).
 */
export function useTopologySearch(
  query: string,
  data: TopologyResponse | undefined,
): TopologySearchResult[] {
  return useMemo(() => {
    if (!data || query.trim().length < 1) return []

    const q = query.toLowerCase().trim()
    const results: Array<TopologySearchResult & { score: number }> = []

    function score(label: string): number {
      const l = label.toLowerCase()
      if (l === q) return 100
      if (l.startsWith(q)) return 80
      if (l.includes(q)) return 50
      return 0
    }

    for (const t of data.topics) {
      const s = score(t.label)
      if (s > 0) results.push({ id: t.id, label: t.label, protocol: 'kafka', repo: t.repo, kind: 'topic', score: s })
    }
    for (const q_ of data.queues) {
      const s = score(q_.label)
      if (s > 0) results.push({ id: q_.id, label: q_.label, protocol: q_.broker, repo: q_.repo, kind: 'queue', score: s })
    }
    for (const n of data.nats_subjects) {
      const s = score(n.label)
      if (s > 0) results.push({ id: n.id, label: n.label, protocol: 'nats', repo: n.repo, kind: 'nats', score: s })
    }
    for (const c of data.channels) {
      const s = score(c.label)
      if (s > 0) results.push({ id: c.id, label: c.label, protocol: c.channel_type, repo: c.repo, kind: 'channel', score: s })
    }
    for (const g of data.graphql_subscriptions) {
      const s = score(g.label)
      if (s > 0) results.push({ id: g.id, label: g.label, protocol: 'graphql_subscription', repo: g.repo, kind: 'graphql', score: s })
    }

    return results
      .sort((a, b) => b.score - a.score || a.label.localeCompare(b.label))
      .slice(0, 10)
      .map(({ score: _score, ...rest }) => rest)
  }, [query, data])
}
