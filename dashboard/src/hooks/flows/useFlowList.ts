import { useQuery } from '@tanstack/react-query'
import { fetchFlows } from '@/api/client'
import type { FlowListResponse, FlowFilters } from '@/types/api'

/**
 * Fetches the list of processes for a given group.
 * Supports filtering by entry point, cross_stack_only, and repo.
 */
export function useFlowList(group: string, filters: FlowFilters = {}) {
  return useQuery<FlowListResponse, Error>({
    queryKey: ['flows', group, filters],
    queryFn: () => fetchFlows(group, filters),
    placeholderData: (prev) => prev,
    staleTime: 60 * 1000,
    enabled: !!group,
  })
}
