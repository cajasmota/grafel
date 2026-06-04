/* ============================================================
   hooks/use-security.ts — Security / Auth-Coverage screen data hooks.

   Wraps the typed api client in TanStack Query. Three queries, all backed
   by the v1 /api/security/* routes (handlers_security.go), which serve
   capability data the rest of the UI did not previously surface (#4250):

     useAuthCoverage(groupId)  → GET /api/security/auth-coverage/{group}
     useSecrets(groupId)       → GET /api/security/secrets/{group}
     useSecurityCycles(groupId)→ GET /api/security/cycles/{group}

   The detector runs server-side over the same cached graph the dashboard
   already loads, so these are pure static reads — NO runtime metrics.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

// ---------------------------------------------------------------------------
// Query keys
// ---------------------------------------------------------------------------

export const authCoverageQueryKey = (groupId: string) =>
  ["security", groupId, "auth-coverage"] as const;

export const secretsQueryKey = (groupId: string) =>
  ["security", groupId, "secrets"] as const;

export const securityCyclesQueryKey = (groupId: string) =>
  ["security", groupId, "cycles"] as const;

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

/** Auth-coverage summary + ranked endpoint findings for the group. */
export function useAuthCoverage(groupId: string) {
  return useQuery({
    queryKey: authCoverageQueryKey(groupId),
    queryFn: () => api.getAuthCoverage(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** Hardcoded-secret + secrets-management findings for the group. */
export function useSecrets(groupId: string) {
  return useQuery({
    queryKey: secretsQueryKey(groupId),
    queryFn: () => api.getSecrets(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** Import-cycle findings for the group. */
export function useSecurityCycles(groupId: string) {
  return useQuery({
    queryKey: securityCyclesQueryKey(groupId),
    queryFn: () => api.getSecurityCycles(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}
