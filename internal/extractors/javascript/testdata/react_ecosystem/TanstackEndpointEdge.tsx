// TanstackEndpointEdge.tsx — proving fixture for issue #5494: link each TanStack
// query/mutation to the HTTP endpoint its queryFn/mutationFn calls.
//
//   - inline `() => fetch('/api/users')`  → USES edge to http:GET:/api/users
//   - inline `() => axios.get('/api/orders')` → USES edge to http:GET:/api/orders
//   - inline POST fetch                   → USES edge to http:POST:/api/users
//   - named ref `mutationFn: createUser`  → CALLS edge to createUser
import {
  useQuery,
  useMutation,
} from '@tanstack/react-query';
import axios from 'axios';

import { createUser } from './api';

// Inline arrow fetcher → USES edge to the /api/users endpoint stub.
export function useUsers() {
  return useQuery({ queryKey: ['users'], queryFn: () => fetch('/api/users') });
}

// Inline axios.get fetcher → USES edge to the /api/orders endpoint stub.
export function useOrders() {
  return useQuery({ queryKey: ['orders'], queryFn: () => axios.get('/api/orders') });
}

// Inline POST fetch → USES edge to http:POST:/api/users.
export function useCreateUserInline() {
  return useMutation({
    mutationFn: () => fetch('/api/users', { method: 'POST' }),
  });
}

// Named ref → CALLS edge to createUser (transitive resolution to the endpoint).
export function useCreateUser() {
  return useMutation({ mutationFn: createUser });
}
