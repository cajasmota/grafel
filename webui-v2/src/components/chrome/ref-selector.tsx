/* ============================================================
   chrome/ref-selector.tsx — topbar ref picker (PH4 / #2092).

   Renders next to the group-switcher button in the TopBar. Shows the
   current ref name + short SHA. Clicking opens a Popover listing all
   indexed refs for the current group, grouped by repo slug.

   Each row shows: name · short SHA · tier badge (HOT/WARM/COLD/EXPIRED)
   · last-indexed timestamp · source icon (branch vs worktree 🌿).

   Sort order: HOT → WARM → COLD → EXPIRED → UNKNOWN, then alpha within
   tier (mirrors server-side sort for resilience).

   Switching a ref updates ?ref= in the URL (via setRef) which triggers
   data refreshes on every view without additional wiring.
   ============================================================ */

import { useState } from "react";
import { GitBranch, Sprout, ChevronsUpDown } from "lucide-react";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useRefs } from "@/hooks/use-refs";
import type { RefEntry, RefTier } from "@/data/types";

// ---------------------------------------------------------------------------
// Tier helpers
// ---------------------------------------------------------------------------

const TIER_TONE: Record<RefTier, "success" | "warning" | "neutral" | "danger"> = {
  HOT: "success",
  WARM: "warning",
  COLD: "neutral",
  EXPIRED: "danger",
  UNKNOWN: "neutral",
};

const TIER_LABEL: Record<RefTier, string> = {
  HOT: "HOT",
  WARM: "WARM",
  COLD: "COLD",
  EXPIRED: "EXP",
  UNKNOWN: "?",
};

/** Format an ms-epoch timestamp as a relative "N ago" string. */
function relativeTime(ms: number | null): string {
  if (ms === null) return "never";
  const delta = Date.now() - ms;
  if (delta < 60_000) return "just now";
  if (delta < 3_600_000) return `${Math.round(delta / 60_000)}m ago`;
  if (delta < 86_400_000) return `${Math.round(delta / 3_600_000)}h ago`;
  return `${Math.round(delta / 86_400_000)}d ago`;
}

// ---------------------------------------------------------------------------
// Single ref row in the dropdown
// ---------------------------------------------------------------------------

interface RefRowProps {
  entry: RefEntry;
  isSelected: boolean;
  onSelect: (name: string) => void;
}

function RefRow({ entry, isSelected, onSelect }: RefRowProps) {
  const isCold = entry.tier === "COLD" || entry.tier === "EXPIRED";
  const tooltipBody = [
    `SHA: ${entry.sha}`,
    entry.indexedAt ? `Indexed: ${new Date(entry.indexedAt).toLocaleString()}` : "Never indexed",
    entry.indexerVersion ? `Indexer: ${entry.indexerVersion}` : null,
    isCold ? "This ref is cold — first query will warm-load (~50 ms)" : null,
  ]
    .filter(Boolean)
    .join("\n");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          role="option"
          aria-selected={isSelected}
          onClick={() => onSelect(entry.name)}
          className={cn(
            "w-full flex items-center gap-2 px-3 py-1.5 text-left rounded-md text-md",
            "hover:bg-surface-2 transition-colors",
            isSelected && "bg-accent-soft text-accent-strong",
          )}
        >
          {/* Source icon: branch vs worktree */}
          {entry.source === "worktree" ? (
            <Sprout size={13} className="shrink-0 text-text-3" aria-label="worktree" />
          ) : (
            <GitBranch size={13} className="shrink-0 text-text-3" aria-label="branch" />
          )}

          {/* Ref name */}
          <span className={cn("flex-1 truncate font-mono text-xs", isCold && "text-text-3")}>
            {entry.name}
          </span>

          {/* Short SHA */}
          <span className="font-mono text-xs text-text-4 shrink-0">{entry.shortSha}</span>

          {/* Tier badge */}
          <Badge tone={TIER_TONE[entry.tier]} className="shrink-0 text-[10px]">
            {TIER_LABEL[entry.tier]}
          </Badge>

          {/* Last indexed */}
          <span className="text-xs text-text-4 shrink-0 min-w-[48px] text-right">
            {relativeTime(entry.indexedAt)}
          </span>
        </button>
      </TooltipTrigger>
      <TooltipContent className="whitespace-pre-line">{tooltipBody}</TooltipContent>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// Main RefSelector component
// ---------------------------------------------------------------------------

export interface RefSelectorProps {
  groupId: string;
  /** Currently selected ref name (null = HEAD). */
  currentRef: string | null;
  /** Callback when the user picks a different ref. */
  onRefChange: (ref: string | null) => void;
}

export function RefSelector({ groupId, currentRef, onRefChange }: RefSelectorProps) {
  const [open, setOpen] = useState(false);
  const { allRefs, byRepo, isLoading } = useRefs(groupId);

  // Find the entry matching the current ref to display its short SHA
  const currentEntry = currentRef
    ? allRefs.find((r) => r.name === currentRef)
    : null;

  const displayLabel = currentRef ?? "HEAD";
  const displaySha = currentEntry?.shortSha ?? null;

  function handleSelect(name: string) {
    onRefChange(name === currentRef ? null : name);
    setOpen(false);
  }

  const repoSlugs = Object.keys(byRepo);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          aria-label="Switch ref"
          aria-haspopup="listbox"
          aria-expanded={open}
          className="inline-flex items-center gap-1.5 h-8 pl-2.5 pr-2 rounded-md border border-border bg-surface text-text-2 text-md hover:bg-surface-2 transition-colors max-w-[220px]"
          data-testid="ref-selector-trigger"
        >
          <GitBranch size={13} className="shrink-0 text-text-3" />
          <span className="font-mono text-xs truncate">{displayLabel}</span>
          {displaySha && (
            <>
              <span className="text-text-4">·</span>
              <span className="font-mono text-xs text-text-4 shrink-0">{displaySha}</span>
            </>
          )}
          <ChevronsUpDown size={13} className="text-text-4 shrink-0 ml-0.5" />
        </button>
      </PopoverTrigger>

      <PopoverContent
        align="end"
        className="w-[360px] p-1 max-h-[480px] overflow-y-auto"
        role="listbox"
        aria-label="Select ref"
        data-testid="ref-selector-popover"
      >
        {isLoading && (
          <p className="px-3 py-2 text-sm text-text-3">Loading refs…</p>
        )}

        {!isLoading && allRefs.length === 0 && (
          <p className="px-3 py-2 text-sm text-text-3">No indexed refs found.</p>
        )}

        {/* HEAD / default option */}
        {!isLoading && allRefs.length > 0 && (
          <button
            role="option"
            aria-selected={currentRef === null}
            onClick={() => { onRefChange(null); setOpen(false); }}
            className={cn(
              "w-full flex items-center gap-2 px-3 py-1.5 text-left rounded-md text-md",
              "hover:bg-surface-2 transition-colors",
              currentRef === null && "bg-accent-soft text-accent-strong",
            )}
          >
            <GitBranch size={13} className="shrink-0 text-text-3" />
            <span className="flex-1 font-mono text-xs">HEAD (default)</span>
          </button>
        )}

        {/* Refs grouped by repo */}
        {!isLoading && repoSlugs.map((slug) => {
          const refs = byRepo[slug];
          if (!refs || refs.length === 0) return null;

          return (
            <div key={slug} className="mt-1">
              <p className="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-text-4 select-none">
                {slug}
              </p>
              {refs.map((entry) => (
                <RefRow
                  key={`${slug}:${entry.name}`}
                  entry={entry}
                  isSelected={entry.name === currentRef}
                  onSelect={handleSelect}
                />
              ))}
            </div>
          );
        })}
      </PopoverContent>
    </Popover>
  );
}
