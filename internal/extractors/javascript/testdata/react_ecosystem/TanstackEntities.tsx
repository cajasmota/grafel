// TanstackEntities.tsx — proving fixture for issue #5492: TanStack/React Query
// entity extraction. Each use* hook call site becomes a decorated
// SCOPE.Operation (subtype tanstack_query | tanstack_mutation) carrying the
// queryKey/queryFn/mutationFn as attributes. The queryKey->endpoint USES edge
// is the follow-up #5494; here we only assert the entities + attributes.
import {
  useQuery,
  useSuspenseQuery,
  useInfiniteQuery,
  useMutation,
} from '@tanstack/react-query';

import { getUsers, getUser, getFeed, createUser } from './api';

// Object-arg form, queryFn as a bare ref → query_key="users", query_fn="getUsers".
export function useUsers() {
  return useQuery({ queryKey: ['users'], queryFn: getUsers });
}

// Object-arg form with a composite key → query_key="user,id".
export function useUserDetail(id: string) {
  return useSuspenseQuery({ queryKey: ['user', id], queryFn: getUser });
}

// Infinite query, object-arg form → query_kind=infinite_query.
export function useFeed() {
  return useInfiniteQuery({ queryKey: ['feed'], queryFn: getFeed });
}

// Mutation, object-arg form → subtype tanstack_mutation, mutation_fn="createUser".
export function useCreateUser() {
  return useMutation({ mutationFn: createUser });
}

// Older positional form: useQuery(key, fn) → query_key="todos", query_fn="getTodos".
export function useTodos() {
  return useQuery(['todos'], getTodos);
}

function getTodos() {
  return fetch('/api/todos').then((r) => r.json());
}
