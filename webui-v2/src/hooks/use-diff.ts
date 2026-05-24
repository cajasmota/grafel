/* ============================================================
   hooks/use-diff.ts — data hook for PH5 graph diff (#2093).

   Fetches the structural diff between two indexed git refs for a single
   repo via GET /api/v2/groups/:g/repos/:r/diff?refA=...&refB=...

   The hook skips the query when refA or refB are empty strings so callers
   can render the compare screen's empty state while the user hasn't
   selected both refs yet.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export const diffQueryKey = (
  groupId: string,
  repo: string,
  refA: string,
  refB: string,
) => ["diff", groupId, repo, refA, refB] as const;

/**
 * Fetch the graph diff between refA and refB for a single repo.
 *
 * Returns the standard TanStack Query result object plus the typed DiffResult
 * data when the query succeeds.
 */
export function useDiff(
  groupId: string,
  repo: string,
  refA: string | null,
  refB: string | null,
) {
  const enabled = Boolean(groupId && repo && refA && refB);

  return useQuery({
    queryKey: diffQueryKey(groupId, repo, refA ?? "", refB ?? ""),
    queryFn: () => api.getDiff(groupId, repo, refA!, refB!),
    enabled,
    // Diff results are stable until a re-index; 60 s stale is fine.
    staleTime: 60_000,
    retry: 1,
  });
}
