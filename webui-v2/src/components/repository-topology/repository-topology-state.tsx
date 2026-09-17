import { Button } from "@/components/ui/button";
import type { RepositoryTopologyResponse } from "@/data/types";

export function RepositoryTopologyState({ loading, error, data, onRetry }: { loading: boolean; error: Error | null; data?: RepositoryTopologyResponse; onRetry: () => void }) {
  if (loading && !data) return <div className="absolute inset-0 z-10 grid place-items-center bg-surface-0/70 text-sm text-text-3">Loading repository relationships…</div>;
  if (error && !data) return <div className="absolute inset-0 z-10 grid place-items-center bg-surface-0/80"><div className="max-w-md rounded border border-danger bg-surface p-4 text-sm"><p className="font-medium text-danger">Repository topology failed to load</p><p className="mt-1 text-text-3">{error.message}</p><Button className="mt-3" size="sm" onClick={onRetry}>Retry</Button></div></div>;
  if (data && data.nodes.length === 0) return <div className="absolute inset-0 z-10 grid place-items-center pointer-events-none"><div className="rounded border border-border bg-surface p-4 text-sm text-text-3">No connected repositories match these filters. Controls remain available.</div></div>;
  return null;
}

export function RepositoryTopologyTruncation({ data }: { data: RepositoryTopologyResponse }) {
  if (!data.truncated) return null;
  return <div className="border-b border-warning/40 bg-warning/10 px-4 py-2 text-xs text-warning">Showing {data.summary.repository_count.toLocaleString()} of {data.summary.pre_limit_nodes.toLocaleString()} repositories and {data.summary.edge_count.toLocaleString()} of {data.summary.pre_limit_edges.toLocaleString()} edges. Focus a repository, reduce depth, or raise minimum relationship count.</div>;
}
