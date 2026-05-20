import { useQuery } from '@tanstack/react-query'
import { fetchOrphanCallers } from '@/api/client'
import type { OrphanCallersResponse } from '@/types/api'

export function useOrphanCallers(group: string) {
  return useQuery<OrphanCallersResponse, Error>({
    queryKey: ['orphan-callers', group],
    queryFn: () => fetchOrphanCallers(group),
    staleTime: 60 * 1000,
  })
}
