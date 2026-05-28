// Swr.tsx — proving fixture for issue #2894 PR2 React Ecosystem group:
// swr_extraction. useSWR / useSWRMutation / useSWRInfinite calls in custom
// hooks and components; the hook call surfaces as USES_HOOK via the generic
// pass and the enclosing function is decorated swr=true + the SWR key.
import useSWR from 'swr';
import useSWRMutation from 'swr/mutation';
import useSWRInfinite from 'swr/infinite';

const fetcher = (url: string) => fetch(url).then((r) => r.json());

// --- custom hook wrapping useSWR (swr_extraction) ---
export function useUser(id: string) {
  const { data, error, isLoading } = useSWR(`/api/users/${id}`, fetcher);
  return { user: data, error, isLoading };
}

// --- list hook with a literal key (swr_extraction) ---
export function useUsers() {
  return useSWR('/api/users', fetcher);
}

// --- mutation hook (swr_extraction) ---
export function useUpdateUser() {
  return useSWRMutation('/api/users', (url, { arg }: { arg: any }) =>
    fetch(url, { method: 'PATCH', body: JSON.stringify(arg) }),
  );
}

// --- infinite hook inside a component (swr_extraction) ---
export function Feed() {
  const { data } = useSWRInfinite(
    (index) => `/api/feed?page=${index}`,
    fetcher,
  );
  return null;
}
