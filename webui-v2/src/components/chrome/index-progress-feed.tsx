/* ============================================================
   IndexProgressFeed — per-repo / per-MODULE live indexing rows (#1527).

   Renders ONE row per repo (single repos) or per MODULE (monorepo package
   roots) from the SSE progress stream. Each row shows its phase, a
   files-done/total bar, entity count, and the file currently being processed.

   This replaces the "single coarse bar" feel: a monorepo with ~20 packages
   now shows ~20 rows that fill independently, matching landing.md's
   "per-repo rows below" indexing spec and settings.md's per-module layout.
   ============================================================ */

import { CheckCircle2, AlertTriangle, Loader2 } from "lucide-react";

import { Badge } from "@/components/ui";
import { cn } from "@/lib/utils";
import type { ProgressPhase, ProgressRow } from "@/data/types";

const PHASE_LABEL: Record<ProgressPhase, string> = {
  scanning: "Scanning files",
  extracting_ast: "Extracting AST",
  resolving_refs: "Resolving refs",
  running_algorithms: "Running algorithms",
  materializing: "Materializing",
  done: "Indexed",
  error: "Failed",
};

function pct(row: ProgressRow): number {
  if (row.phase === "done") return 100;
  if (row.filesTotal <= 0) return row.phase === "scanning" ? 5 : 10;
  return Math.min(99, Math.round((row.filesDone / row.filesTotal) * 100));
}

function PhaseIcon({ phase }: { phase: ProgressPhase }) {
  if (phase === "done") return <CheckCircle2 size={13} className="text-success" />;
  if (phase === "error") return <AlertTriangle size={13} className="text-danger" />;
  return <Loader2 size={13} className="animate-spin text-accent-strong" />;
}

export interface IndexProgressFeedProps {
  rows: ProgressRow[];
  /** Shown while connected but no events have arrived yet. */
  loading?: boolean;
  className?: string;
}

export function IndexProgressFeed({ rows, loading, className }: IndexProgressFeedProps) {
  if (rows.length === 0) {
    return (
      <p className={cn("text-xs text-text-4", className)} data-testid="progress-feed-empty">
        {loading ? "Waiting for the first files…" : "No per-repo progress yet."}
      </p>
    );
  }

  return (
    <ul className={cn("space-y-2", className)} data-testid="progress-feed">
      {rows.map((row) => {
        const p = pct(row);
        const failed = row.phase === "error";
        const done = row.phase === "done";
        return (
          <li
            key={row.key}
            className="rounded-lg border border-border bg-surface p-2.5"
            data-testid="progress-row"
            data-module={row.module ?? ""}
            data-repo={row.repoSlug}
          >
            <div className="flex items-center justify-between gap-2">
              <div className="flex min-w-0 items-center gap-2">
                <PhaseIcon phase={row.phase} />
                <span className="truncate font-mono text-xs text-text-2" title={row.module ?? row.repoSlug}>
                  {row.module ?? row.repoSlug}
                </span>
                {row.module && (
                  <Badge tone="info" className="shrink-0">
                    module
                  </Badge>
                )}
              </div>
              <span className="shrink-0 text-[11px] text-text-4">{PHASE_LABEL[row.phase]}</span>
            </div>

            <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded-full bg-surface-2">
              <div
                className={cn(
                  "h-full rounded-full transition-all duration-300",
                  failed ? "bg-danger" : done ? "bg-success" : "bg-accent",
                )}
                style={{ width: `${p}%` }}
              />
            </div>

            <div className="mt-1 flex items-center justify-between gap-2 text-[11px] text-text-4">
              <span className="truncate font-mono" title={row.currentFile}>
                {failed
                  ? row.error || "error"
                  : row.currentFile || (row.filesTotal > 0 ? `${row.filesDone}/${row.filesTotal} files` : "")}
              </span>
              <span className="shrink-0 tabular-nums">
                {row.filesTotal > 0 && `${row.filesDone}/${row.filesTotal}`}
                {row.entitiesSoFar > 0 && ` · ${row.entitiesSoFar} entities`}
              </span>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
