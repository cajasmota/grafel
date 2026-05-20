import { useQuery } from '@tanstack/react-query'
import { Lightbulb } from 'lucide-react'

interface PatternCalloutProps {
  patternId: string
}

interface MockPattern {
  id: string
  title: string
  description: string
  exemplars: string[]
}

const MOCK_PATTERNS: Record<string, MockPattern> = {
  'drf-viewset-crud': {
    id: 'drf-viewset-crud',
    title: 'DRF ViewSet CRUD',
    description:
      'Standard CRUD pattern using Django REST Framework ModelViewSet. All five HTTP verbs (list, create, retrieve, update, destroy) are wired automatically by a router.',
    exemplars: ['OrderViewSet', 'UserViewSet', 'ProductViewSet'],
  },
  'kafka-producer': {
    id: 'kafka-producer',
    title: 'Kafka Producer',
    description:
      'Publishes domain events to a Kafka topic immediately after a successful database write. Keeps HTTP response latency low by not waiting for downstream consumers.',
    exemplars: ['place_order', 'complete_payment', 'ship_order'],
  },
}

/**
 * Renders a {{pattern:<id>}} block as a styled callout with exemplar list.
 * Fetches pattern metadata from /api/patterns/{id} in production;
 * uses MOCK_PATTERNS in dev.
 */
export function PatternCallout({ patternId }: PatternCalloutProps) {
  const { data: pattern, isLoading } = useQuery<MockPattern>({
    queryKey: ['pattern', patternId],
    queryFn: async () => {
      if (import.meta.env.DEV) {
        await new Promise((r) => setTimeout(r, 50))
        return MOCK_PATTERNS[patternId] ?? {
          id: patternId,
          title: patternId,
          description: 'Pattern not found in mock data.',
          exemplars: [],
        }
      }
      const res = await fetch(`/api/patterns/${encodeURIComponent(patternId)}`)
      if (!res.ok) throw new Error(`Pattern ${patternId} not found`)
      return res.json()
    },
    staleTime: 5 * 60 * 1000,
  })

  if (isLoading) {
    return (
      <div className="my-4 rounded-lg border border-amber-800/50 bg-amber-950/30 px-4 py-3 animate-pulse">
        <div className="h-4 w-40 rounded bg-amber-900/50" />
      </div>
    )
  }

  if (!pattern) return null

  return (
    <aside
      role="note"
      aria-label={`Pattern: ${pattern.title}`}
      className="my-6 rounded-lg border border-amber-700/50 bg-amber-950/20 px-4 py-4"
    >
      <div className="flex items-start gap-3">
        <Lightbulb
          className="w-5 h-5 text-amber-400 flex-shrink-0 mt-0.5"
          aria-hidden
        />
        <div className="min-w-0 space-y-2">
          <div className="flex items-center gap-2">
            <span className="text-xs font-semibold uppercase tracking-wider text-amber-400">
              Pattern
            </span>
            <span className="text-sm font-semibold text-slate-800 dark:text-slate-200">{pattern.title}</span>
          </div>
          <p className="text-sm text-slate-400 dark:text-slate-400 leading-relaxed">{pattern.description}</p>
          {pattern.exemplars.length > 0 && (
            <div className="flex flex-wrap gap-2 pt-1">
              <span className="text-xs text-slate-400 dark:text-slate-500">Exemplars:</span>
              {pattern.exemplars.map((name) => (
                <code
                  key={name}
                  className="px-1.5 py-0.5 rounded bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 font-mono text-xs border border-slate-300 dark:border-slate-700"
                >
                  {name}
                </code>
              ))}
            </div>
          )}
        </div>
      </div>
    </aside>
  )
}
