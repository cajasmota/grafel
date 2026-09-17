import { keepPreviousData, useQuery } from "@tanstack/react-query";

import type { RepositoryTopologyEdgeFilters, RepositoryTopologyFilters } from "@/data/types";
import { api } from "@/lib/api";
import { serializeRepositoryTopologyFilters } from "@/lib/repository-topology-filters";

export function useRepositoryTopology(groupId: string, filters: RepositoryTopologyFilters) {
  const serialized = serializeRepositoryTopologyFilters(filters).toString();
  return useQuery({
    queryKey: ["repository-topology", groupId, serialized] as const,
    queryFn: () => api.getRepositoryTopology(groupId, filters),
    enabled: groupId.length > 0,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}

export function useRepositoryTopologyEdge(
  groupId: string,
  filters: RepositoryTopologyEdgeFilters | null,
) {
  const key = filters ? JSON.stringify({ ...filters, evidence: [...(filters.evidence ?? [])].sort() }) : "";
  return useQuery({
    queryKey: ["repository-topology-edge", groupId, key] as const,
    queryFn: () => api.getRepositoryTopologyEdge(groupId, filters!),
    enabled: groupId.length > 0 && filters !== null,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  });
}
