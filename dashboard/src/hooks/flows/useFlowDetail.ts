import { useQuery } from '@tanstack/react-query'
import { fetchFlowDetail } from '@/api/client'
import type { FlowDetailResponse } from '@/types/api'

/**
 * Fetches full process detail including chain steps and source snippets.
 * Only fires when processId is non-null.
 */
export function useFlowDetail(group: string, processId: string | null) {
  return useQuery<FlowDetailResponse, Error>({
    queryKey: ['flow-detail', group, processId],
    queryFn: () => fetchFlowDetail(group, processId!),
    enabled: !!processId && !!group,
    staleTime: 5 * 60 * 1000,
  })
}
