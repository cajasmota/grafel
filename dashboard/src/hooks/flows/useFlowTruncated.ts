import { useQuery } from '@tanstack/react-query'
import { fetchFlowTruncated } from '@/api/client'
import type { FlowTruncatedResponse } from '@/types/api'

/**
 * Fetches flows that were cut short during chain resolution.
 * Endpoint: GET /api/flows/{group}/truncated (#1146)
 */
export function useFlowTruncated(group: string) {
  return useQuery<FlowTruncatedResponse, Error>({
    queryKey: ['flows-truncated', group],
    queryFn: () => fetchFlowTruncated(group),
    placeholderData: (prev: FlowTruncatedResponse | undefined) => prev,
    staleTime: 60 * 1000,
    enabled: !!group,
  })
}
