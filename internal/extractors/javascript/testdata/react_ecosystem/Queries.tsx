// Queries.tsx — proving fixture for issue #2894 PR1 React Ecosystem group:
// tanstack_query_extraction + rtk_query_extraction. Mixes TanStack/React Query
// hooks (useQuery/useMutation/useInfiniteQuery + invalidateQueries) and an RTK
// Query api (createApi/injectEndpoints).
import {
  useQuery,
  useMutation,
  useInfiniteQuery,
  useQueryClient,
  QueryClient,
} from '@tanstack/react-query';
import { createApi, fetchBaseQuery } from '@reduxjs/toolkit/query/react';

// --- TanStack Query QueryClient ---
export const queryClient = new QueryClient();

// --- TanStack Query hooks (tanstack_query_extraction) ---
export function useUsers() {
  return useQuery({
    queryKey: ['users'],
    queryFn: () => fetch('/api/users').then((r) => r.json()),
  });
}

export function useUser(id: string) {
  return useQuery({
    queryKey: ['user', id],
    queryFn: () => fetch(`/api/users/${id}`).then((r) => r.json()),
  });
}

export function useUserFeed() {
  return useInfiniteQuery({
    queryKey: ['feed'],
    queryFn: ({ pageParam = 0 }) => fetch(`/api/feed?p=${pageParam}`).then((r) => r.json()),
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: any) =>
      fetch('/api/users', { method: 'POST', body: JSON.stringify(body) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['users'] });
    },
  });
}

// --- RTK Query api (rtk_query_extraction) ---
export const usersApi = createApi({
  reducerPath: 'usersApi',
  baseQuery: fetchBaseQuery({ baseUrl: '/api' }),
  endpoints: (builder) => ({
    getUsers: builder.query({
      query: () => '/users',
    }),
    getUserById: builder.query({
      query: (id: string) => `/users/${id}`,
    }),
    addUser: builder.mutation({
      query: (body: any) => ({ url: '/users', method: 'POST', body }),
    }),
  }),
});

export const { useGetUsersQuery, useGetUserByIdQuery, useAddUserMutation } = usersApi;

// --- RTK Query injectEndpoints (rtk_query_extraction) ---
export const extendedApi = usersApi.injectEndpoints({
  endpoints: (builder) => ({
    deleteUser: builder.mutation({
      query: (id: string) => ({ url: `/users/${id}`, method: 'DELETE' }),
    }),
  }),
});
