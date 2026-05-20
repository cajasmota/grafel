import { useQuery } from '@tanstack/react-query'
import { fetchTopology } from '@/api/client'
import type { TopologyResponse, TopologyFilters } from '@/types/api'

/**
 * Fetches broker topology for a given group.
 * Returns topics, queues, channels, graphql_subscriptions, nats_subjects,
 * transforms, and producer/consumer entity stubs.
 */
export function useTopologyData(group: string, filters: TopologyFilters = {}) {
  return useQuery<TopologyResponse, Error>({
    queryKey: ['topology', group, filters],
    queryFn: () => fetchTopology(group, filters),
    placeholderData: (prev) => prev,
    staleTime: 60 * 1000,
    enabled: !!group,
  })
}
