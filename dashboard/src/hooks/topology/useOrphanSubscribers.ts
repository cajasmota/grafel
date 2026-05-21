import { useQuery } from '@tanstack/react-query'
import { fetchOrphanSubscribers } from '@/api/client'
import type { OrphanSubscribersResponse } from '@/types/api'

/**
 * Fetches orphan subscribers for a given group.
 * Consumers that have no matching producer — from GET /api/topology/{group}/orphan-subscribers (#1137).
 * Gracefully returns empty when the endpoint is not yet deployed.
 */
export function useOrphanSubscribers(group: string) {
  return useQuery<OrphanSubscribersResponse, Error>({
    queryKey: ['topology-orphan-subscribers', group],
    queryFn: () => fetchOrphanSubscribers(group),
    staleTime: 60 * 1000,
    enabled: !!group,
  })
}
