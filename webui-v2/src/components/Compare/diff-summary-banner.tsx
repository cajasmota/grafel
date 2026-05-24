/* ============================================================
   Compare/diff-summary-banner.tsx — "PR review framing" banner
   displayed above the change list (PH5 / #2093).

   Shows: "Showing what changes if you merge <refB> into <refA>"
   and a stat row: +N entities  -N entities  ~N entities
                   +N relationships  -N relationships
   ============================================================ */

import { GitMerge } from "lucide-react";
import { cn } from "@/lib/utils";
import type { DiffSummary } from "@/data/types";

export interface DiffSummaryBannerProps {
  refA: string;
  refB: string;
  summary: DiffSummary;
  className?: string;
}

export function DiffSummaryBanner({ refA, refB, summary, className }: DiffSummaryBannerProps) {
  const isEmpty =
    summary.entities_added === 0 &&
    summary.entities_removed === 0 &&
    summary.entities_modified === 0 &&
    summary.relationships_added === 0 &&
    summary.relationships_removed === 0;

  return (
    <div
      className={cn(
        "px-4 py-3 bg-surface-2 border-b border-border",
        className,
      )}
    >
      {/* Title row */}
      <div className="flex items-center gap-2 mb-2">
        <GitMerge className="size-4 text-text-3 shrink-0" />
        {isEmpty ? (
          <span className="text-sm text-text-2">
            No differences between{" "}
            <code className="font-mono text-text-1">{refA}</code> and{" "}
            <code className="font-mono text-text-1">{refB}</code>
          </span>
        ) : (
          <span className="text-sm text-text-2">
            Showing what changes if you merge{" "}
            <code className="font-mono text-accent">{refB}</code> into{" "}
            <code className="font-mono text-text-1">{refA}</code>
          </span>
        )}
      </div>

      {/* Stats row */}
      {!isEmpty && (
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs font-medium">
          {summary.entities_added > 0 && (
            <span className="text-success">+{summary.entities_added} entities</span>
          )}
          {summary.entities_removed > 0 && (
            <span className="text-danger">−{summary.entities_removed} entities</span>
          )}
          {summary.entities_modified > 0 && (
            <span className="text-warning">~{summary.entities_modified} entities</span>
          )}
          {summary.relationships_added > 0 && (
            <span className="text-success">+{summary.relationships_added} relationships</span>
          )}
          {summary.relationships_removed > 0 && (
            <span className="text-danger">−{summary.relationships_removed} relationships</span>
          )}
          {summary.files_changed > 0 && (
            <span className="text-text-3">{summary.files_changed} files</span>
          )}
        </div>
      )}
    </div>
  );
}
