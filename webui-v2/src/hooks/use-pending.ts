/* ============================================================
   hooks/use-pending.ts — data hooks for the Pending screen (#1442).
   ============================================================ */

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** Returns the full candidates payload (both tabs) for a group. */
export function useCandidates(groupId: string) {
  return useQuery({
    queryKey: ["candidates", groupId],
    queryFn: () => api.listCandidates(groupId),
    // Candidates change on index runs; polling every 30 s keeps the count fresh
    // without hammering the daemon.
    refetchInterval: 30_000,
    staleTime: 10_000,
  });
}

/**
 * Mutation to persist a hint for one entity.
 * Pass the stable `entityId` from the candidate (NOT the ephemeral `id`)
 * so hints survive candidate-ID churn across re-index sweeps (#1518).
 * Invalidates the candidates query on success.
 */
export function useSaveHint(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ entityId, hint }: { entityId: string; hint: string }) =>
      api.saveHint(groupId, entityId, hint),
    onSuccess: () => {
      // Invalidate so a background refetch picks up the persisted hint value.
      qc.invalidateQueries({ queryKey: ["candidates", groupId] });
    },
  });
}
