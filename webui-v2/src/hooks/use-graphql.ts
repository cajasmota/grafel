/* ============================================================
   hooks/use-graphql.ts — GraphQL resolver-effects data hook (#4255).

   Wraps api.getGraphQL in TanStack Query. Backed by the route
   GET /api/graphql/{group} (handlers_graphql.go → handleGraphQL),
   which returns a raw-JSON GraphQLReport: resolvers grouped by their
   parent SDL type, each with operation verb, effects (db/http/mutation
   with confidence), auth (when modeled), framework, and a source ref —
   plus an SDL schema-type roll-up. Pure static graph read.
   ============================================================ */

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export const graphqlQueryKey = (groupId: string) =>
  ["graphql", groupId] as const;

/** GraphQL resolver-effects report for the group. */
export function useGraphQL(groupId: string) {
  return useQuery({
    queryKey: graphqlQueryKey(groupId),
    queryFn: () => api.getGraphQL(groupId),
    enabled: !!groupId,
    staleTime: 30_000,
  });
}
