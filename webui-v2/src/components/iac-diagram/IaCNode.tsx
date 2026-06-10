/* ============================================================
   components/iac-diagram/IaCNode.tsx — one IaC resource node (#4526).

   A resource card colored + iconed by its resource_category. Label = the
   resource name (e.g. `aws_sqs_queue.main`), with the tool-native type as a
   subtitle. An unresolved-target chip appears when the resource has relations
   whose endpoint could not be joined to a rendered node (#4495). Clicking the
   node opens the source peek (file:line) when a source ref exists.
   ============================================================ */

import { memo } from "react";
import { useParams } from "react-router-dom";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { Unlink } from "lucide-react";
import { cn } from "@/lib/utils";
import { useSourcePeek } from "@/components/SourcePeek";
import { categoryStyle } from "./categoryStyle";
import type { IaCNodeData } from "./layout";

function IaCNodeImpl({ data, sourcePosition, targetPosition, selected }: NodeProps) {
  const { resource, unresolvedCount } = data as IaCNodeData;
  const style = categoryStyle(resource.category);
  const Icon = style.Icon;
  const { openSourcePeek } = useSourcePeek();
  const { groupId = "" } = useParams<{ groupId: string }>();

  const hasSource = !!resource.source_file;
  const onClick = () => {
    if (!hasSource) return;
    openSourcePeek({
      groupId,
      file: resource.source_file!,
      line: resource.start_line ?? 0,
      repo: resource.repo,
    });
  };

  return (
    <div
      onClick={onClick}
      className={cn(
        "group relative flex flex-col justify-center gap-0.5 rounded-md border bg-surface px-2.5 py-1.5 shadow-sm transition-shadow",
        hasSource && "cursor-pointer hover:shadow-md",
        selected && "ring-2 ring-offset-1 ring-offset-bg",
      )}
      style={{
        width: "100%",
        height: "100%",
        borderColor: style.color,
        borderLeftWidth: 3,
        // selection ring picks up the category color
        ...(selected ? ({ ["--tw-ring-color" as string]: style.color }) : {}),
      }}
      title={
        hasSource
          ? `${resource.name} — ${resource.source_file}:${resource.start_line ?? 0}`
          : resource.name
      }
    >
      <Handle type="target" position={targetPosition ?? Position.Left} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--text-4)]" />

      <div className="flex items-center gap-1.5 min-w-0">
        <span
          className="inline-flex size-4 shrink-0 items-center justify-center rounded"
          style={{ color: style.color, background: style.tint }}
        >
          <Icon size={11} />
        </span>
        <span className="truncate font-mono text-[11px] font-medium text-text" title={resource.name}>
          {resource.name}
        </span>
        {unresolvedCount > 0 && (
          <span
            className="ml-auto inline-flex shrink-0 items-center gap-0.5 rounded px-1 text-[9px] text-text-4"
            title={`${unresolvedCount} relation target(s) not resolved to a rendered resource (#4495)`}
          >
            <Unlink size={9} />
            {unresolvedCount}
          </span>
        )}
      </div>

      <div className="flex items-center gap-1 min-w-0 pl-[22px]">
        <span
          className="truncate font-mono text-[9px] uppercase tracking-wide"
          style={{ color: style.color }}
          title={style.label}
        >
          {style.key}
        </span>
        {resource.resource_type && (
          <span className="truncate font-mono text-[9px] text-text-4" title={resource.resource_type}>
            {resource.resource_type}
          </span>
        )}
      </div>

      <Handle type="source" position={sourcePosition ?? Position.Right} className="!h-1.5 !w-1.5 !border-0 !bg-[var(--text-4)]" />
    </div>
  );
}

export const IaCNode = memo(IaCNodeImpl);
