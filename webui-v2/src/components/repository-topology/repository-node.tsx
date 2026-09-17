import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Boxes, GitBranch } from "lucide-react";
import type { RepositoryNodeData } from "@/lib/repository-topology-layout";

function RepositoryNodeImpl({ data, selected }: NodeProps) {
  const repository = (data as RepositoryNodeData).repository;
  const evidenceIssues = repository.evidence.inferred
    + repository.evidence.dangling
    + repository.evidence.ambiguous
    + repository.evidence.external;

  return (
    <div
      role="button"
      aria-label={`Repository ${repository.label || repository.repository}`}
      aria-pressed={selected}
      tabIndex={0}
      className="flex h-full w-full flex-col rounded-lg border bg-surface-1 px-3 py-2.5 text-left shadow-sm transition-colors"
      style={{
        borderColor: selected ? "var(--accent)" : "var(--border)",
        boxShadow: selected ? "0 0 0 2px color-mix(in srgb, var(--accent) 30%, transparent)" : undefined,
      }}
      title={`${repository.repository} · ${repository.entity_count} entities · ${repository.connected_repositories} connected repositories`}
    >
      <Handle type="target" position={Position.Left} className="!h-2 !w-2 !border-0 !bg-text-4" />
      <Handle type="target" position={Position.Top} className="!h-2 !w-2 !border-0 !bg-text-4" />

      <div className="flex min-w-0 items-center justify-between gap-2">
        <span className="truncate text-sm font-semibold text-text-1">
          {repository.label || repository.repository}
        </span>
        {repository.primary_language && (
          <span className="shrink-0 rounded bg-surface-3 px-1.5 py-0.5 text-[10px] text-text-3">
            {repository.primary_language}
          </span>
        )}
      </div>

      <span className="mt-1 truncate font-mono text-[10px] text-text-4">{repository.repository}</span>

      <div className="mt-auto flex items-center gap-3 text-[10px] text-text-3">
        <span className="flex items-center gap-1" title="Modules">
          <Boxes size={11} aria-hidden="true" />
          {repository.module_count}
        </span>
        <span className="flex items-center gap-1" title="Connected repositories">
          <GitBranch size={11} aria-hidden="true" />
          {repository.connected_repositories}
        </span>
        <span className="ml-auto" title="Inbound and outbound relationships">
          {repository.inbound_relationships} in · {repository.outbound_relationships} out
        </span>
      </div>

      {evidenceIssues > 0 && (
        <span className="mt-1 text-[9px] text-warning" title="Relationships needing evidence review">
          {evidenceIssues} need review
        </span>
      )}

      <Handle type="source" position={Position.Right} className="!h-2 !w-2 !border-0 !bg-text-4" />
      <Handle type="source" position={Position.Bottom} className="!h-2 !w-2 !border-0 !bg-text-4" />
    </div>
  );
}

export const RepositoryNode = memo(RepositoryNodeImpl);
