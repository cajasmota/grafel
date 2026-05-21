import { useQuery } from '@tanstack/react-query'
import { fetchOrphanPublishers } from '@/api/client'
import type { OrphanPublishersResponse } from '@/types/api'

/**
 * Fetches orphan publishers for a given group.
 * Producers that have no matching consumer — from GET /api/topology/{group}/orphan-publishers (#1136).
 * Gracefully returns empty when the endpoint is not yet deployed.
 */
export function useOrphanPublishers(group: string) {
  return useQuery<OrphanPublishersResponse, Error>({
    queryKey: ['topology-orphan-publishers', group],
    queryFn: () => fetchOrphanPublishers(group),
    staleTime: 60 * 1000,
    enabled: !!group,
  })
}
