/* ============================================================
   use-docs.ts — TanStack Query hooks for the Docs screen.
   useDocsTree: fetches the entity tree for the left pane.
   useDocsEntity: fetches a single entity's full detail.
   Both use v2 envelope endpoints added in handlers_v2_docs.go.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { DocsTreeNode, DocsEntityDetail } from "@/data/types";

export function useDocsTree(groupId: string) {
  return useQuery<DocsTreeNode[], ApiError>({
    queryKey: ["docs-tree", groupId],
    queryFn: () => api.getDocsTree(groupId),
    staleTime: 30_000,
  });
}

export function useDocsEntity(groupId: string, entityId: string | null) {
  return useQuery<DocsEntityDetail, ApiError>({
    queryKey: ["docs-entity", groupId, entityId],
    queryFn: () => api.getDocsEntity(groupId, entityId!),
    enabled: entityId !== null,
    staleTime: 30_000,
    retry: (count, err) => {
      // Don't retry on 404 — renders EntityStub instead.
      if (err instanceof ApiError && err.status === 404) return false;
      return count < 2;
    },
  });
}
