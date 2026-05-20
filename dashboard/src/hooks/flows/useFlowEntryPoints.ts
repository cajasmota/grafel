import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchFlows } from '@/api/client'
import type { Process } from '@/types/api'

/**
 * Lists candidate entry points for the flow picker.
 * Debounced search filters by entry_name substring.
 * The recent list is derived from the last 5 selections stored in memory.
 */
export function useFlowEntryPoints(group: string, searchQuery: string = '') {
  const [debouncedQuery, setDebouncedQuery] = useState(searchQuery)
  const [recentIds, setRecentIds] = useState<string[]>([])

  // Debounce: 200 ms
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(searchQuery), 200)
    return () => clearTimeout(timer)
  }, [searchQuery])

  const { data, isLoading, error } = useQuery({
    queryKey: ['flow-entry-points', group, debouncedQuery],
    queryFn: () => fetchFlows(group, { limit: 100 }),
    staleTime: 60 * 1000,
    enabled: !!group,
  })

  const allProcesses = data?.processes ?? []

  // Unique entry points (one per entry_id)
  const seen = new Set<string>()
  const entryPoints: Process[] = []
  for (const p of allProcesses) {
    if (!seen.has(p.entry_id)) {
      seen.add(p.entry_id)
      entryPoints.push(p)
    }
  }

  // Filter by search query
  const filtered = debouncedQuery
    ? entryPoints.filter(
        (p) =>
          p.entry_name.toLowerCase().includes(debouncedQuery.toLowerCase()) ||
          p.label.toLowerCase().includes(debouncedQuery.toLowerCase()),
      )
    : entryPoints

  // Recent entries
  const recent = recentIds
    .map((id) => entryPoints.find((p) => p.entry_id === id))
    .filter((p): p is Process => p !== undefined)

  function recordRecent(entryId: string) {
    setRecentIds((prev) => {
      const next = [entryId, ...prev.filter((id) => id !== entryId)].slice(0, 5)
      return next
    })
  }

  return { entryPoints: filtered, recent, isLoading, error, recordRecent }
}
