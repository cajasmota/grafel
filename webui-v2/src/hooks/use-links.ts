/* ============================================================
   hooks/use-links.ts — Cross-repo links map data hook (#4253).

   Wraps api.getGroupLinks in TanStack Query. Backed by the v1 route
   GET /api/groups/{group}/links (handlers_graph.go → handleGroupLinks),
   which returns raw JSON { links: CrossRepoLink[] } — the resolved
   cross-repo call map (frontend fetch → backend endpoint, publisher →
   topic, etc.). Pure static graph read, no runtime metrics.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export const groupLinksQueryKey = (groupId: string) =>
  ["links", groupId] as const;

/** Resolved cross-repo link records for the group. */
export function useGroupLinks(groupId: string) {
  return useQuery({
    queryKey: groupLinksQueryKey(groupId),
    queryFn: () => api.getGroupLinks(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}
