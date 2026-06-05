/* ============================================================
   hooks/use-paths.ts — Paths screen data hooks.

   Three queries, all v2-enveloped:
     usePaths(groupId)         → backend-grouped route list + totals
     usePathDetail(groupId, hash) → full route detail for detail pane
     useOrphans(groupId)       → orphan caller list for Orphans tab
   ============================================================ */

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useAuthCoverage } from "@/hooks/use-security";
import type { AuthEndpointFinding } from "@/data/types";

export const pathsQueryKey = (groupId: string) =>
  ["paths", groupId] as const;

export const pathDetailQueryKey = (groupId: string, hash: string) =>
  ["paths", groupId, "detail", hash] as const;

export const orphansQueryKey = (groupId: string) =>
  ["paths", groupId, "orphans"] as const;

/** Grouped backend route list + totals — drives the left rail. */
export function usePaths(groupId: string) {
  return useQuery({
    queryKey: pathsQueryKey(groupId),
    queryFn: () => api.listPaths(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}

/** Full detail for a selected path (right pane). Fetched lazily when a route is clicked. */
export function usePathDetail(groupId: string, pathHash: string | null) {
  return useQuery({
    queryKey: pathDetailQueryKey(groupId, pathHash ?? ""),
    queryFn: () => api.getPathDetail(groupId, pathHash!),
    enabled: !!groupId && !!pathHash,
    staleTime: 60_000,
  });
}

export const pathPostureQueryKey = (groupId: string, hash: string) =>
  ["paths", groupId, "posture", hash] as const;

/**
 * Lazy posture + effective-contract sections for the open path (#4254).
 * Fetched only once a path is selected so the main detail payload stays lean.
 */
export function usePathPosture(groupId: string, pathHash: string | null) {
  return useQuery({
    queryKey: pathPostureQueryKey(groupId, pathHash ?? ""),
    queryFn: () => api.getPathPosture(groupId, pathHash!),
    enabled: !!groupId && !!pathHash,
    staleTime: 60_000,
  });
}

/** Orphan callers list — fetched when the Orphans tab is active. */
export function useOrphans(groupId: string, enabled: boolean) {
  return useQuery({
    queryKey: orphansQueryKey(groupId),
    queryFn: () => api.listOrphans(groupId),
    enabled: !!groupId && enabled,
    staleTime: 30_000,
  });
}

/**
 * Refs #1935 Phase 1 — fetch the ShapeTree subtree for a class entity.
 * Used by the ShapeTree component to lazy-load fields when the user
 * expands a parameter or response row. `typeEntityId` is the prefixed
 * entity id returned by the path-detail payload; `type` is a bare-name
 * fallback when no entity id is available.
 */
export const shapeQueryKey = (groupId: string, key: string) =>
  ["paths", groupId, "shape", key] as const;

export function useShape(
  groupId: string,
  args: { typeEntityId?: string; type?: string },
  enabled: boolean,
) {
  const key = args.typeEntityId ?? args.type ?? "";
  return useQuery({
    queryKey: shapeQueryKey(groupId, key),
    queryFn: () => api.getShape(groupId, args),
    enabled: !!groupId && enabled && !!key,
    staleTime: 5 * 60_000,
  });
}

/* ============================================================
   Auth-coverage overlay for the Paths screen (#4253, epic #4249)

   The Paths list rows / detail header have no entity_id, but the
   auth-coverage findings (AuthEndpointFinding) carry method + path.
   So we index findings by a normalised "<METHOD> <path>" key and let
   Paths look up a finding per (verb, route.path). This reuses the
   Security screen's cached query (authCoverageQueryKey) — no refetch.
   ============================================================ */

/** Normalise a path for join-keying: strip a trailing slash, lowercase nothing. */
function normPath(p: string | undefined): string {
  const s = (p ?? "").trim();
  if (s.length > 1 && s.endsWith("/")) return s.slice(0, -1);
  return s;
}

/** Build the lookup key for a (method, path) pair. Method is upper-cased. */
function authKey(method: string | undefined, path: string | undefined): string {
  return `${(method ?? "").toUpperCase()} ${normPath(path)}`;
}

/** A method+path → finding lookup plus convenience getters. */
export interface AuthCoverageIndex {
  /** True while the underlying auth-coverage query is loading. */
  isLoading: boolean;
  /** Look up the worst finding for a (verb, path). */
  lookup: (verb: string | undefined, path: string | undefined) => AuthEndpointFinding | undefined;
  /** Look up the worst finding across several verbs for one path (detail header). */
  lookupAny: (verbs: string[] | undefined, path: string | undefined) => AuthEndpointFinding | undefined;
}

/** Severity rank — higher is worse, used to pick the worst finding per key. */
const SEV_RANK: Record<string, number> = { error: 3, warn: 2, info: 1 };

function worse(a: AuthEndpointFinding, b: AuthEndpointFinding): AuthEndpointFinding {
  // Prefer a NO-AUTH finding, then higher severity, then sensitive/IDOR.
  const sa = (a.has_auth ? 0 : 1) * 10 + (SEV_RANK[a.severity] ?? 0);
  const sb = (b.has_auth ? 0 : 1) * 10 + (SEV_RANK[b.severity] ?? 0);
  return sb > sa ? b : a;
}

/**
 * useAuthCoverageIndex — reuses the cached auth-coverage query and indexes
 * findings by "<METHOD> <path>" so the Paths screen can badge each row and
 * the detail header. Falls back to a path-only index for findings that lack
 * a method, so a row can still match when only the path is known.
 */
export function useAuthCoverageIndex(groupId: string): AuthCoverageIndex {
  const { data, isLoading } = useAuthCoverage(groupId);

  const { byMethodPath, byPath } = useMemo(() => {
    const byMethodPath = new Map<string, AuthEndpointFinding>();
    const byPath = new Map<string, AuthEndpointFinding>();
    for (const f of data?.findings ?? []) {
      if (!f.path) continue;
      const mpKey = authKey(f.method, f.path);
      const existingMp = byMethodPath.get(mpKey);
      byMethodPath.set(mpKey, existingMp ? worse(existingMp, f) : f);

      const pKey = normPath(f.path);
      const existingP = byPath.get(pKey);
      byPath.set(pKey, existingP ? worse(existingP, f) : f);
    }
    return { byMethodPath, byPath };
  }, [data]);

  return useMemo<AuthCoverageIndex>(() => {
    const lookup = (verb: string | undefined, path: string | undefined) =>
      byMethodPath.get(authKey(verb, path)) ?? byPath.get(normPath(path));

    const lookupAny = (verbs: string[] | undefined, path: string | undefined) => {
      let best: AuthEndpointFinding | undefined;
      for (const v of verbs ?? []) {
        const f = byMethodPath.get(authKey(v, path));
        if (f) best = best ? worse(best, f) : f;
      }
      return best ?? byPath.get(normPath(path));
    };

    return { isLoading, lookup, lookupAny };
  }, [byMethodPath, byPath, isLoading]);
}
