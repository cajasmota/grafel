import { Suspense, lazy, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { GitBranch } from 'lucide-react'

const MermaidBlock = lazy(() =>
  import('./MermaidBlock').then((m) => ({ default: m.MermaidBlock })),
)

interface FlowDiagramProps {
  entryEntityId: string
}

interface MockStep {
  label: string
  repo: string
  kind: string
}

/** Minimal mock flow data keyed by entry entity ID */
const MOCK_FLOWS: Record<string, MockStep[]> = {
  'entity-order-viewset': [
    { label: 'OrderViewSet.create', repo: 'acme-web', kind: 'Operation' },
    { label: 'place_order', repo: 'acme-web', kind: 'Function' },
    { label: 'Order.save', repo: 'acme-web', kind: 'Function' },
    { label: 'orders.placed (Kafka)', repo: 'acme-workers', kind: 'Queue' },
    { label: 'FulfillmentWorker.handle', repo: 'acme-workers', kind: 'Function' },
  ],
}

function stepsToMermaid(steps: MockStep[]): string {
  const nodes = steps.map((s, i) => {
    const id = `s${i}`
    const shape = s.kind === 'Queue' ? `(("${s.label}"))` : `["${s.label}"]`
    return `  ${id}${shape}`
  })
  const edges = steps.slice(0, -1).map((_, i) => `  s${i} --> s${i + 1}`)
  return ['flowchart TD', ...nodes, ...edges].join('\n')
}

/**
 * Renders a {{flow:<entry-entity-id>}} block as a mini swim-lane Mermaid diagram.
 * Fetches process steps from /api/traces (or mocks) then renders via MermaidBlock.
 * MermaidBlock is lazy-loaded.
 */
export function FlowDiagram({ entryEntityId }: FlowDiagramProps) {
  const { data: steps, isLoading } = useQuery<MockStep[]>({
    queryKey: ['flow-diagram', entryEntityId],
    queryFn: async () => {
      if (import.meta.env.DEV) {
        await new Promise((r) => setTimeout(r, 80))
        return MOCK_FLOWS[entryEntityId] ?? []
      }
      const res = await fetch(
        `/api/traces?action=follow&entry_point_id=${encodeURIComponent(entryEntityId)}`,
      )
      if (!res.ok) throw new Error(`Traces fetch failed`)
      const json = await res.json()
      return json.steps ?? []
    },
    staleTime: 5 * 60 * 1000,
  })

  if (isLoading) {
    return (
      <div className="my-6 rounded-lg border border-slate-300 dark:border-slate-700 bg-slate-100 dark:bg-slate-900 p-4 animate-pulse">
        <div className="h-4 w-48 rounded bg-slate-200 dark:bg-slate-800 mb-2" />
        <div className="h-32 rounded bg-slate-200 dark:bg-slate-800" />
      </div>
    )
  }

  if (!steps || steps.length === 0) {
    return (
      <div className="my-6 rounded-lg border border-slate-300 dark:border-slate-700 bg-slate-100 dark:bg-slate-900 px-4 py-6 text-center text-sm text-slate-400 dark:text-slate-500">
        <GitBranch className="w-8 h-8 mx-auto mb-2 text-slate-700" aria-hidden />
        No flow data for this entry point.
      </div>
    )
  }

  const mermaidCode = stepsToMermaid(steps)

  return (
    <figure className="my-6 rounded-lg border border-slate-300 dark:border-slate-700 bg-slate-100 dark:bg-slate-900 overflow-hidden" aria-label="Process flow diagram">
      <figcaption className="px-4 py-2 border-b border-slate-200 dark:border-slate-800 flex items-center gap-2 text-xs text-slate-400 dark:text-slate-400">
        <GitBranch className="w-3.5 h-3.5" aria-hidden />
        Process Flow
      </figcaption>
      <div className="p-4">
        <Suspense fallback={<div className="h-40 animate-pulse rounded bg-slate-200 dark:bg-slate-800" />}>
          <MermaidBlock code={mermaidCode} />
        </Suspense>
      </div>
    </figure>
  )
}
