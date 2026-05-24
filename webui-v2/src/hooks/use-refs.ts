/* ============================================================
   hooks/use-refs.ts — data hook for PH4 ref-selector (#2092).

   Fetches all indexed refs for a group from GET /api/v2/groups/:g/refs
   and exposes a flat sorted array of RefEntry per repo. The sort order
   mirrors the tier priority: HOT → WARM → COLD → EXPIRED → UNKNOWN,
   then alphabetically within each tier.

   The hook is intentionally kept stateless — current-ref selection
   lives in the URL (?ref=) via use-ref-state.ts and is NOT stored
   here so that deep links work without hydration ordering issues.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { RefEntry, RefTier } from "@/data/types";

const TIER_ORDER: Record<RefTier, number> = {
  HOT: 0,
  WARM: 1,
  COLD: 2,
  EXPIRED: 3,
  UNKNOWN: 4,
};

/** Sort RefEntry[] by tier priority then by name alpha. */
export function sortRefs(refs: RefEntry[]): RefEntry[] {
  return [...refs].sort((a, b) => {
    const td = TIER_ORDER[a.tier] - TIER_ORDER[b.tier];
    if (td !== 0) return td;
    return a.name.localeCompare(b.name);
  });
}

/** Query key factory so callers can invalidate precisely. */
export const refsQueryKey = (groupId: string) => ["refs", groupId] as const;

/**
 * Fetch + expose all refs for a group.  Returns the raw record keyed by
 * repo slug as `byRepo` plus a convenience flat sorted list `allRefs`.
 */
export function useRefs(groupId: string) {
  const query = useQuery({
    queryKey: refsQueryKey(groupId),
    queryFn: () => api.getRefs(groupId),
    // Refs change only after a re-index; 30 s stale is fine.
    staleTime: 30_000,
    // Don't hammer the daemon if it returns 404 (endpoint not yet live).
    retry: false,
  });

  const byRepo = query.data?.refs ?? {};
  const allRefs: RefEntry[] = sortRefs(
    Object.values(byRepo).flat(),
  );

  return { ...query, byRepo, allRefs };
}

/**
 * Fetch refs for a specific repo within a group. Returns a sorted list
 * of RefEntry for that repo (empty while loading or on error).
 */
export function useRepoRefs(groupId: string, repoSlug: string) {
  const { byRepo, isLoading, isError, error } = useRefs(groupId);
  const refs = sortRefs(byRepo[repoSlug] ?? []);
  return { refs, isLoading, isError, error };
}
