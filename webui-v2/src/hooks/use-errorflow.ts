/* ============================================================
   hooks/use-errorflow.ts — Error-flow data hook (#4267).

   Wraps api.getErrorFlow in TanStack Query. Backed by the route
   GET /api/errorflow/{group} (handlers_errorflow.go → handleErrorFlow),
   which rolls up SCOPE.ExceptionType nodes across the group: each
   exception type lists the callables that THROW it (throwers) and
   the handlers that CATCH it (catchers), with file refs and an
   honest uncaught flag (thrown-but-no-typed-catcher-in-graph).

   Pure static graph read. Honest-empty when the group has no
   THROWS/CATCHES edges (no exception modelling indexed).
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export const errorFlowQueryKey = (groupId: string) =>
  ["errorflow", groupId] as const;

/** Error-flow report for the group. */
export function useErrorFlow(groupId: string) {
  return useQuery({
    queryKey: errorFlowQueryKey(groupId),
    queryFn: () => api.getErrorFlow(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}
