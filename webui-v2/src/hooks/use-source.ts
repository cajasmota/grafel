/* ============================================================
   hooks/use-source.ts — source-peek data hook (#4499).

   Fetches a window of source for a file:line ref from the indexed repo
   working tree (GET /api/v2/groups/:id/source). Drives the shared
   <SourcePeek> modal — the get_source equivalent for the UI.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export const sourceQueryKey = (
  groupId: string,
  file: string,
  line: number,
  repo: string,
) => ["source", groupId, repo, file, line] as const;

/**
 * Fetch the source window for a file:line ref. Lazy: only runs when enabled
 * and a file is present. Results are cached per (group, repo, file, line) so
 * reopening the same ref is instant.
 */
export function useSource(
  groupId: string,
  file: string | null,
  line: number,
  repo?: string,
  enabled = true,
) {
  return useQuery({
    queryKey: sourceQueryKey(groupId, file ?? "", line, repo ?? ""),
    queryFn: () =>
      api.getSource(groupId, { file: file!, line: line || undefined, repo }),
    enabled: enabled && !!groupId && !!file,
    staleTime: 60_000,
  });
}
