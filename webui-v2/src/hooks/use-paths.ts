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
import type { AuthEndpointFinding, ControlFlowDetail } from "@/data/types";

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

/* ============================================================
   Downstream-DAG for the endpoint-flow modal (#4350, backend #4349)

   The DAG is parameterised by (mode, depth, semantic, verb): changing the
   spine/full toggle or the depth control refetches with a new query key, and
   TanStack caches each (mode, depth) combination so toggling back is instant.
   ============================================================ */

export interface DownstreamDAGParams {
  mode: "spine" | "full";
  depth: number;
  semantic: boolean;
  verb?: string;
}

export const downstreamDAGQueryKey = (
  groupId: string,
  hash: string,
  p: DownstreamDAGParams,
) => ["paths", groupId, "downstream-dag", hash, p.mode, p.depth, p.semantic, p.verb ?? ""] as const;

/**
 * Fetch the endpoint downstream-DAG for the <FlowDag> modal. Fetched lazily
 * (enabled only when the modal is open) so the main paths payload stays lean.
 */
export function useDownstreamDAG(
  groupId: string,
  pathHash: string | null,
  params: DownstreamDAGParams,
  enabled: boolean,
) {
  return useQuery({
    queryKey: downstreamDAGQueryKey(groupId, pathHash ?? "", params),
    queryFn: () =>
      api.getPathDownstreamDAG(groupId, pathHash!, {
        mode: params.mode,
        depth: params.depth,
        semantic: params.semantic,
        verb: params.verb,
      }),
    enabled: !!groupId && !!pathHash && enabled,
    staleTime: 60_000,
  });
}

/* ============================================================
   Control-flow / flowchart for the Downstream-flow modal (#4819, backend #4820)

   The Flowchart VIEW of the modal. Parameterised by (detail, verb): moving the
   Detail slider (outline→decisions→data→full) refetches with a new query key;
   TanStack caches each detail level so sliding back is instant. Fetched lazily
   (only when the modal is open AND the Flowchart view is selected).
   ============================================================ */

export const controlFlowQueryKey = (
  groupId: string,
  hash: string,
  detail: ControlFlowDetail,
  depth: number,
  verb?: string,
) => ["paths", groupId, "control-flow", hash, detail, depth, verb ?? ""] as const;

/**
 * Fetch the endpoint handler's control-flow graph for the Flowchart view.
 * Enabled only when the modal is open and the Flowchart view is selected, so the
 * Tree view never pays for a CFG it isn't showing. The #4883 `depth` param
 * controls interprocedural inlining (1 = handler-only); TanStack caches each
 * (detail, depth) pair so sliding either control back is instant.
 */
export function useControlFlow(
  groupId: string,
  pathHash: string | null,
  detail: ControlFlowDetail,
  depth: number,
  verb: string | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: controlFlowQueryKey(groupId, pathHash ?? "", detail, depth, verb),
    queryFn: () =>
      api.getPathControlFlow(groupId, pathHash!, { detail, depth, verb }),
    enabled: !!groupId && !!pathHash && enabled,
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
