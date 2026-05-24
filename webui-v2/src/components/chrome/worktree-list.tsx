/* ============================================================
   chrome/worktree-list.tsx — worktree subtree in the nav-rail (PH4 / #2092).

   Renders after the main screen-nav entries and before the Pending
   divider. Shows repos that have at least one worktree ref, indented
   under the repo slug. Each worktree child row has:
     - 🌿 (Sprout) icon
     - ref name (truncated)
     - tier badge (HOT/WARM/COLD)
     - click → switches ?ref= in URL (via setRef)

   The list is collapsed by default when the rail is narrow (56px) and
   expands on rail-hover, matching the rest of the rail's pattern.

   Only shown when refs endpoint is available (graceful degradation).
   ============================================================ */

import { Sprout } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import { useRefs } from "@/hooks/use-refs";
import { useRefState } from "@/lib/use-ref-state";
import type { RefEntry, RefTier } from "@/data/types";

const TIER_TONE: Record<RefTier, "success" | "warning" | "neutral" | "danger"> = {
  HOT: "success",
  WARM: "warning",
  COLD: "neutral",
  EXPIRED: "danger",
  UNKNOWN: "neutral",
};

const TIER_LABEL: Record<RefTier, string> = {
  HOT: "H",
  WARM: "W",
  COLD: "C",
  EXPIRED: "E",
  UNKNOWN: "?",
};

interface WorktreeRowProps {
  entry: RefEntry;
  isSelected: boolean;
  onSelect: (name: string) => void;
}

function WorktreeRow({ entry, isSelected, onSelect }: WorktreeRowProps) {
  return (
    <button
      onClick={() => onSelect(entry.name)}
      title={`${entry.name} · ${entry.shortSha}`}
      className={cn(
        "group/wt relative flex items-center h-9 rounded-md px-2.5 mx-2 gap-2",
        "text-text-2 transition-colors duration-[120ms] w-[calc(100%-16px)]",
        isSelected ? "bg-surface text-text shadow-[var(--shadow-1)]" : "hover:bg-surface-2",
      )}
      data-testid={`worktree-row-${entry.name}`}
    >
      {/* Indent visual */}
      <span className="w-3 shrink-0 opacity-0 group-hover/rail:opacity-100" aria-hidden>
        └
      </span>

      <Sprout size={15} className="shrink-0 text-text-3" aria-label="worktree" />

      <span className="flex-1 truncate font-mono text-xs opacity-0 group-hover/rail:opacity-100 transition-opacity whitespace-nowrap">
        {entry.name}
      </span>

      <Badge
        tone={TIER_TONE[entry.tier]}
        className="shrink-0 text-[9px] opacity-0 group-hover/rail:opacity-100 transition-opacity"
        title={entry.tier}
      >
        {TIER_LABEL[entry.tier]}
      </Badge>
    </button>
  );
}

export interface WorktreeListProps {
  /** Resolved group ID (from URL param). */
  groupId: string;
}

/**
 * Renders the worktree subtree section in the nav-rail.
 * Returns null when there are no worktree refs (graceful degradation).
 */
export function WorktreeList({ groupId }: WorktreeListProps) {
  const { currentRef, setRef } = useRefState();
  const { byRepo, isLoading, isError } = useRefs(groupId);

  if (isLoading || isError) return null;

  // Build a list of repos that have at least one worktree ref.
  const reposWithWorktrees = Object.entries(byRepo)
    .map(([slug, refs]) => ({
      slug,
      worktrees: refs.filter((r) => r.source === "worktree"),
    }))
    .filter((r) => r.worktrees.length > 0);

  if (reposWithWorktrees.length === 0) return null;

  function handleSelect(name: string) {
    setRef(name === currentRef ? null : name);
  }

  return (
    <div
      className="flex flex-col gap-0 mt-1"
      aria-label="Worktree branches"
      data-testid="worktree-list"
    >
      <div className="my-1.5 mx-3 border-t border-border" />

      {/* Section header — only visible when rail is expanded */}
      <p className="px-3 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-text-4 select-none opacity-0 group-hover/rail:opacity-100 transition-opacity whitespace-nowrap">
        Worktrees
      </p>

      {reposWithWorktrees.map(({ slug, worktrees }) => (
        <div key={slug} className="flex flex-col gap-0">
          {/* Repo slug label — only visible expanded */}
          <p className="px-4 py-0.5 text-[10px] text-text-4 select-none opacity-0 group-hover/rail:opacity-100 transition-opacity font-mono whitespace-nowrap truncate">
            {slug}
          </p>

          {worktrees.map((entry) => (
            <WorktreeRow
              key={entry.name}
              entry={entry}
              isSelected={entry.name === currentRef}
              onSelect={handleSelect}
            />
          ))}
        </div>
      ))}
    </div>
  );
}
