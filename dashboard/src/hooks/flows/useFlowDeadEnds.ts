import { useQuery } from '@tanstack/react-query'
import { fetchFlowDeadEnds } from '@/api/client'
import type { FlowDeadEndsResponse } from '@/types/api'

/**
 * Fetches flows that terminate without reaching a useful sink.
 * Endpoint: GET /api/flows/{group}/dead-ends (#1145)
 */
export function useFlowDeadEnds(group: string) {
  return useQuery<FlowDeadEndsResponse, Error>({
    queryKey: ['flows-dead-ends', group],
    queryFn: () => fetchFlowDeadEnds(group),
    placeholderData: (prev: FlowDeadEndsResponse | undefined) => prev,
    staleTime: 60 * 1000,
    enabled: !!group,
  })
}
