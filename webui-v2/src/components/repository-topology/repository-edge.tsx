import { memo } from "react";
import { BaseEdge, EdgeLabelRenderer, type EdgeProps } from "@xyflow/react";
import type { RepositoryEdgeData } from "@/lib/repository-topology-layout";
import { repositoryProtocolColor } from "@/lib/repository-topology-layout";

function curvedPath(
  sourceX: number,
  sourceY: number,
  targetX: number,
  targetY: number,
  parallelIndex: number,
  parallelCount: number,
): { path: string; labelX: number; labelY: number } {
  const deltaX = targetX - sourceX;
  const deltaY = targetY - sourceY;
  const length = Math.max(1, Math.hypot(deltaX, deltaY));
  const centeredIndex = parallelIndex - (parallelCount - 1) / 2;
  const offset = centeredIndex * 30;
  const normalX = (-deltaY / length) * offset;
  const normalY = (deltaX / length) * offset;
  const controlOneX = sourceX + deltaX / 3 + normalX;
  const controlOneY = sourceY + deltaY / 3 + normalY;
  const controlTwoX = sourceX + (deltaX * 2) / 3 + normalX;
  const controlTwoY = sourceY + (deltaY * 2) / 3 + normalY;

  return {
    path: `M ${sourceX},${sourceY} C ${controlOneX},${controlOneY} ${controlTwoX},${controlTwoY} ${targetX},${targetY}`,
    labelX: sourceX + deltaX / 2 + normalX * 0.75,
    labelY: sourceY + deltaY / 2 + normalY * 0.75,
  };
}

function RepositoryEdgeImpl({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  data,
  markerEnd,
  style,
  selected,
}: EdgeProps) {
  const edgeData = data as RepositoryEdgeData;
  const relationship = edgeData.relationship;
  const { path, labelX, labelY } = curvedPath(
    sourceX,
    sourceY,
    targetX,
    targetY,
    edgeData.parallelIndex,
    edgeData.parallelCount,
  );
  const color = repositoryProtocolColor(relationship.channel);

  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          ...style,
          filter: selected ? `drop-shadow(0 0 3px ${color})` : undefined,
          opacity: selected ? 1 : 0.82,
        }}
      />
      <EdgeLabelRenderer>
        <div
          role="note"
          aria-label={`${relationship.channel}, ${relationship.relationship_count} relationships`}
          className="nodrag nopan pointer-events-none absolute rounded border bg-surface-1 px-1.5 py-0.5 text-[9px] font-medium uppercase shadow-sm"
          style={{
            color,
            borderColor: color,
            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
          }}
        >
          {relationship.channel} · {relationship.relationship_count}
        </div>
      </EdgeLabelRenderer>
    </>
  );
}

export const RepositoryEdge = memo(RepositoryEdgeImpl);
