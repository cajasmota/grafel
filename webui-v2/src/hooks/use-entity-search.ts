/* ============================================================
   hooks/use-entity-search.ts — debounced entity search for
   the command palette. Wraps api.searchEntities via TanStack
   Query. Only fires when `q` is non-empty and `groupId` is set.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Entity } from "@/data/types";

export function useEntitySearch(groupId: string | undefined, q: string) {
  return useQuery<Entity[]>({
    queryKey: ["entity-search", groupId, q],
    queryFn: () => api.searchEntities(groupId!, q),
    enabled: !!groupId && q.trim().length > 0,
    staleTime: 10_000,
    placeholderData: [],
  });
}
