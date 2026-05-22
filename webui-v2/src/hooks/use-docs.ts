/* ============================================================
   use-docs.ts — TanStack Query hooks for the Docs screen.

   Docs are the GENERATED MARKDOWN documents produced by the
   `generate-docs` skill (run by the user's coding agent), served by
   the v2 endpoints in handlers_v2_docs.go:

     useDocsTree — tree of generated documents (per-repo → category → doc)
     useDocPage  — raw markdown of one document, by its tree path key
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { DocNode, DocPage } from "@/data/types";

export function useDocsTree(groupId: string) {
  return useQuery<DocNode[], ApiError>({
    queryKey: ["docs-tree", groupId],
    queryFn: () => api.getDocsTree(groupId),
    staleTime: 30_000,
  });
}

export function useDocPage(groupId: string, path: string | null) {
  return useQuery<DocPage, ApiError>({
    queryKey: ["docs-page", groupId, path],
    queryFn: () => api.getDocPage(groupId, path!),
    enabled: path !== null,
    staleTime: 30_000,
    retry: (count, err) => {
      if (err instanceof ApiError && err.status === 404) return false;
      return count < 2;
    },
  });
}
