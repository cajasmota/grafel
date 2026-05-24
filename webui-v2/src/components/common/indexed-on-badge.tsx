/* ============================================================
   common/indexed-on-badge.tsx — "indexed @ <ref> · <sha> · <ago>"
   metadata badge (PH4 / #2092).

   Renders small muted inline text showing when the currently viewed
   ref was last indexed. Hovering reveals a tooltip with the full SHA,
   full timestamp, and indexer version. When the tier is COLD or EXPIRED,
   the badge color shifts to gray and the tooltip warns about warm-load.

   Usage:
     <IndexedOnBadge groupId="my-group" repoSlug="core" refName="main" />

   If refName is null (HEAD), the hook picks the first ref for the repo
   (typically the default branch).
   ============================================================ */

import { cn } from "@/lib/utils";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { useRepoRefs } from "@/hooks/use-refs";

function relativeTime(ms: number | null): string {
  if (ms === null) return "never";
  const delta = Date.now() - ms;
  if (delta < 60_000) return "just now";
  if (delta < 3_600_000) return `${Math.round(delta / 60_000)}m ago`;
  if (delta < 86_400_000) return `${Math.round(delta / 3_600_000)}h ago`;
  return `${Math.round(delta / 86_400_000)}d ago`;
}

export interface IndexedOnBadgeProps {
  groupId: string;
  repoSlug: string;
  /** Currently selected ref name; null means HEAD / default. */
  refName: string | null;
  className?: string;
}

export function IndexedOnBadge({
  groupId,
  repoSlug,
  refName,
  className,
}: IndexedOnBadgeProps) {
  const { refs, isLoading } = useRepoRefs(groupId, repoSlug);

  if (isLoading || refs.length === 0) return null;

  // Find the matching entry or fall back to the first (HEAD-equivalent).
  const entry = refName
    ? (refs.find((r) => r.name === refName) ?? refs[0])
    : refs[0];

  if (!entry) return null;

  const isCold = entry.tier === "COLD" || entry.tier === "EXPIRED";
  const ago = relativeTime(entry.indexedAt);

  const tooltipLines = [
    `Ref: ${entry.name}`,
    `SHA: ${entry.sha}`,
    entry.indexedAt
      ? `Indexed: ${new Date(entry.indexedAt).toLocaleString()}`
      : "Never indexed",
    entry.indexerVersion ? `Indexer: ${entry.indexerVersion}` : null,
    isCold
      ? "This ref is cold — first query will warm-load (~50 ms)"
      : null,
  ]
    .filter(Boolean)
    .join("\n");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          className={cn(
            "inline-flex items-center gap-1 font-mono text-xs select-none cursor-default",
            isCold ? "text-text-4" : "text-text-3",
            className,
          )}
          data-testid="indexed-on-badge"
        >
          <span>indexed @</span>
          <span className={cn(isCold && "opacity-60")}>{entry.name}</span>
          <span className="text-text-5">·</span>
          <span className={cn(isCold && "opacity-60")}>{entry.shortSha}</span>
          <span className="text-text-5">·</span>
          <span>{ago}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent className="whitespace-pre-line">{tooltipLines}</TooltipContent>
    </Tooltip>
  );
}
