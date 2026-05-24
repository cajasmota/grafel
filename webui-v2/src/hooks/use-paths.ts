/* ============================================================
   hooks/use-paths.ts — Paths screen data hooks.

   Three queries, all v2-enveloped:
     usePaths(groupId)         → backend-grouped route list + totals
     usePathDetail(groupId, hash) → full route detail for detail pane
     useOrphans(groupId)       → orphan caller list for Orphans tab
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

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
