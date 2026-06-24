// TanstackNegative.tsx — a component that calls a locally-defined function named
// useQuery WITHOUT importing @tanstack/react-query. The TanStack entity pass
// must NOT fire here (gate on the import). Proves issue #5492 is import-gated.
function useQuery(opts: { queryKey: string[] }) {
  return opts;
}

export function NotTanstack() {
  return useQuery({ queryKey: ['nope'] });
}
