/* ============================================================
   hooks/use-group-monorepos.ts — data hook for M3 monorepo module
   map (#2180).

   Reads the `monorepos` field from the GET /api/v2/groups response
   for the current group. The groups list is already fetched by
   useGroups / Landing; this hook derives the monorepos slice from
   that cached query so no extra network request is needed.

   Returns:
     monorepos  — Record<string, string[]> or undefined when absent
     isLoading  — true while the groups query is in flight
     isError    — true on network error
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Group } from "@/data/types";

/** Query key for the groups list (matches use-groups.ts). */
const groupsQueryKey = ["groups"] as const;

/**
 * Derive the monorepos map for a specific group from the cached /api/v2/groups
 * response. Re-uses the same query so no additional network call is made when
 * the Landing screen has already fetched the list.
 */
export function useGroupMonorepos(groupId: string) {
  const query = useQuery({
    queryKey: groupsQueryKey,
    queryFn: () => api.listGroups(),
    staleTime: 30_000,
    retry: false,
  });

  const group: Group | undefined = query.data?.find((g) => g.id === groupId);
  const monorepos = group?.monorepos;

  return {
    monorepos,
    isLoading: query.isLoading,
    isError: query.isError,
  };
}
