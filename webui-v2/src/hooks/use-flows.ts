/* ============================================================
   hooks/use-flows.ts — data hooks for the Flows screen.

   Three hook families:
     useFlows(groupId, tab, search)   — list rail (all + crossrepo tabs)
     useFlowDeadEnds(groupId)         — dead-ends tab
     useFlowTruncated(groupId)        — truncated tab (expected to be empty)
     useFlowDetail(groupId, pid)      — detail panel (enabled when pid != null)
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export function useFlows(groupId: string, tab: string, search: string) {
  return useQuery({
    queryKey: ["flows", groupId, tab, search],
    queryFn: () => api.listFlows(groupId, { tab, search, limit: 200 }),
    staleTime: 30_000,
    enabled: tab !== "deadends" && tab !== "truncated",
  });
}

export function useFlowDeadEnds(groupId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["flows-deadends", groupId],
    queryFn: () => api.listFlowDeadEnds(groupId),
    staleTime: 30_000,
    enabled,
  });
}

export function useFlowTruncated(groupId: string, enabled: boolean) {
  return useQuery({
    queryKey: ["flows-truncated", groupId],
    queryFn: () => api.listFlowTruncated(groupId),
    staleTime: 30_000,
    enabled,
  });
}

export function useFlowDetail(groupId: string, processId: string | null) {
  return useQuery({
    queryKey: ["flow-detail", groupId, processId],
    queryFn: () => api.getFlowDetail(groupId, processId!),
    staleTime: 60_000,
    enabled: processId != null,
  });
}
