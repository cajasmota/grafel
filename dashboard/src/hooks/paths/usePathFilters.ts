import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { PathFilters } from '@/types/api'

/**
 * Reads / writes Surface 4 filter state from the URL search params.
 * Changes to any filter reset the page to 1.
 */
export function usePathFilters(): {
  filters: PathFilters
  setFilter: <K extends keyof PathFilters>(key: K, value: PathFilters[K]) => void
  clearFilters: () => void
} {
  const [params, setParams] = useSearchParams()

  const filters: PathFilters = {
    prefix: params.get('prefix') ?? undefined,
    q: params.get('q') ?? undefined,
    repo: params.get('repo') ?? undefined,
    framework: params.get('framework') ?? undefined,
    status_code: params.get('status_code')
      ? (parseInt(params.get('status_code')!, 10) || undefined)
      : undefined,
    is_webhook: params.get('is_webhook') === 'true'
      ? true
      : params.get('is_webhook') === 'false'
        ? false
        : undefined,
    page: Math.max(1, parseInt(params.get('page') ?? '1', 10) || 1),
    page_size: 50,
  }

  const setFilter = useCallback(
    <K extends keyof PathFilters>(key: K, value: PathFilters[K]) => {
      setParams((prev) => {
        const next = new URLSearchParams(prev)
        if (value === undefined || value === null || value === '') {
          next.delete(key)
        } else {
          next.set(key, String(value))
        }
        // Reset page on filter change (not on page change itself)
        if (key !== 'page') {
          next.set('page', '1')
        }
        return next
      })
    },
    [setParams],
  )

  const clearFilters = useCallback(() => {
    setParams(new URLSearchParams())
  }, [setParams])

  return { filters, setFilter, clearFilters }
}
