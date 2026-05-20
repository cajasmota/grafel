import { useSearchParams } from 'react-router-dom'
import type { FlowFilters } from '@/types/api'

/**
 * Reads and writes flow filter state from URL search params.
 * Keeps the URL as the single source of truth for shareability.
 */
export function useFlowFilters(): {
  filters: FlowFilters
  selectedProcessId: string | null
  setEntry: (entryId: string | undefined) => void
  setCrossStackOnly: (v: boolean) => void
  setRepo: (repo: string | undefined) => void
  setSelectedProcessId: (id: string | null) => void
  clearFilters: () => void
} {
  const [params, setParams] = useSearchParams()

  const filters: FlowFilters = {
    entry: params.get('entry') ?? undefined,
    cross_stack_only: params.get('cross_stack_only') === 'true',
    repo: params.get('repo') ?? undefined,
  }

  const selectedProcessId = params.get('process') ?? null

  function update(patch: Record<string, string | null>) {
    setParams((prev) => {
      const next = new URLSearchParams(prev)
      for (const [k, v] of Object.entries(patch)) {
        if (v === null || v === '' || v === 'false') {
          next.delete(k)
        } else {
          next.set(k, v)
        }
      }
      return next
    })
  }

  return {
    filters,
    selectedProcessId,
    setEntry: (entryId) => update({ entry: entryId ?? null }),
    setCrossStackOnly: (v) => update({ cross_stack_only: v ? 'true' : null }),
    setRepo: (repo) => update({ repo: repo ?? null }),
    setSelectedProcessId: (id) => update({ process: id }),
    clearFilters: () => update({ entry: null, cross_stack_only: null, repo: null, process: null }),
  }
}
