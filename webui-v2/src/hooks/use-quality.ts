/* ============================================================
   hooks/use-quality.ts — Coverage / Quality screen data hooks.

   Wraps the typed api client in TanStack Query. Five queries, all backed
   by capability routes the backend already serves but no screen previously
   surfaced (#4251, epic #4249). All are raw-JSON v1 routes:

     useQualityCoverage(groupId) → GET /api/quality/coverage/{group}
     useDependencies(groupId)    → GET /api/dependencies/{group}
     useAntiPatterns(groupId)    → GET /api/quality/anti-patterns/{group}
     useGodNodes(groupId)        → GET /api/groups/{group}/god-nodes
     useQualityTrends(groupId)   → GET /api/quality/trends/{group}

   Each runs server-side over the same cached graph the dashboard already
   loads (trends reads health-history.jsonl) — pure static reads.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

// ---------------------------------------------------------------------------
// Query keys
// ---------------------------------------------------------------------------

export const qualityCoverageQueryKey = (groupId: string) =>
  ["quality", groupId, "coverage"] as const;

export const dependenciesQueryKey = (groupId: string) =>
  ["quality", groupId, "dependencies"] as const;

export const antiPatternsQueryKey = (groupId: string) =>
  ["quality", groupId, "anti-patterns"] as const;

export const godNodesQueryKey = (groupId: string) =>
  ["quality", groupId, "god-nodes"] as const;

export const qualityTrendsQueryKey = (groupId: string) =>
  ["quality", groupId, "trends"] as const;

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

/** Test-coverage report + uncovered-entity drill-down for the group. */
export function useQualityCoverage(groupId: string) {
  return useQuery({
    queryKey: qualityCoverageQueryKey(groupId),
    queryFn: () => api.getQualityCoverage(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** Declared / used / unused / phantom dependency breakdown per repo. */
export function useDependencies(groupId: string) {
  return useQuery({
    queryKey: dependenciesQueryKey(groupId),
    queryFn: () => api.getDependencies(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** N+1 query anti-pattern findings for the group. */
export function useAntiPatterns(groupId: string) {
  return useQuery({
    queryKey: antiPatternsQueryKey(groupId),
    queryFn: () => api.getAntiPatterns(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** High-degree structural hotspots (god-nodes) for the group. */
export function useGodNodes(groupId: string) {
  return useQuery({
    queryKey: godNodesQueryKey(groupId),
    queryFn: () => api.getGodNodes(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** Per-metric quality time series (over history) for the group. */
export function useQualityTrends(groupId: string) {
  return useQuery({
    queryKey: qualityTrendsQueryKey(groupId),
    queryFn: () => api.getQualityTrends(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}
