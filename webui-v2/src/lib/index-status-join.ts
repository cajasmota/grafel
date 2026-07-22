/* ============================================================
   index-status-join.ts — join the status-plane poll (GET
   /api/v2/groups/{group}/index-status) onto the SSE progress rows (#47 phase 2).

   The SSE stream drives per-repo phase/file progress, but it stops once the
   graph is queryable — it has no visibility into the `enhancing` (background
   relationship enrichment) state or engine CPU/RSS. The status-plane poll fills
   that gap. Both surfaces key each repo by the SAME `repo_slug`, so these pure
   helpers merge the two by slug: the web wizard uses them to drive a secondary
   "enhancing" bar, CPU/RAM badges, and to know when to keep polling — mirroring
   the TUI (see internal/cli/wiztui/indexview.go bgProgressBlock + metricSuffix).

   Extracted as pure functions so they are unit-testable under `vitest run
   src/lib` without a DOM or a live daemon.
   ============================================================ */

import type { IndexStatusEngine, IndexStatusReply, ProgressRow } from "@/data/types";

/**
 * Merge the status-plane repo rows onto the progress rows, joining by
 * `repo_slug` === `repoSlug`. Adds `indexing` / `enhancing` / `relationships`
 * and backfills `entitiesSoFar` when the status count is AHEAD of the SSE count
 * (never regressing a higher SSE value — the SSE stream is the live source
 * during extraction). Rows with no matching status repo are returned untouched.
 * A no-op (same array reference) when `status` is undefined.
 */
export function joinIndexStatus(
  rows: ProgressRow[],
  status: IndexStatusReply | undefined,
): ProgressRow[] {
  if (!status) return rows;
  const bySlug = new Map(status.repos.map((r) => [r.repo_slug, r]));
  let changed = false;
  const next = rows.map((row) => {
    const s = bySlug.get(row.repoSlug);
    if (!s) return row;
    changed = true;
    return {
      ...row,
      indexing: s.indexing,
      enhancing: s.enhancing,
      relationships: s.relationships,
      // Never regress a higher live SSE entity count with a lagging status count.
      entitiesSoFar: Math.max(row.entitiesSoFar, s.entities),
      reindexRequired: s.reindex_required,
      reindexReason: s.reindex_reason,
    };
  });
  return changed ? next : rows;
}

/** True when at least one repo is still enhancing in the background. */
export function groupEnhancing(status: IndexStatusReply | undefined): boolean {
  return !!status && status.repos.some((r) => r.enhancing);
}

/**
 * True when at least one repo needs a reindex after a format upgrade (#5907)
 * — the engine has already loop-guard-enqueued the reindex; this only drives
 * the "reindexing after upgrade" / "reindex required" badge so the state is
 * never silent from the web dashboard.
 */
export function groupReindexRequired(status: IndexStatusReply | undefined): boolean {
  return !!status && status.repos.some((r) => r.reindex_required);
}

/**
 * Queryability predicate: true once the group's graph is written and servable —
 * i.e. there is at least one repo and EVERY repo has finished extraction
 * (`indexing === false`), so its graph.fb is on disk and the graph view can
 * stream. Background enhancement (`enhancing === true`) may still be running;
 * that does NOT gate queryability — an enhancing group is already visualizable —
 * so this deliberately ignores `enhancing`. Mirrors the TUI wizard, which only
 * lets the user proceed to the graph once the group is queryable (see #47).
 * False when the status plane is missing or reports zero repos.
 */
export function groupQueryable(status: IndexStatusReply | undefined): boolean {
  return !!status && status.repos.length > 0 && status.repos.every((r) => !r.indexing);
}

/**
 * Poll-continuation predicate: true while any repo is indexing OR enhancing, so
 * the wizard keeps polling the status plane until every repo has settled.
 */
export function anyRepoActive(status: IndexStatusReply | undefined): boolean {
  return !!status && status.repos.some((r) => r.indexing || r.enhancing);
}

/**
 * "View graph" enable-gate for the wizard — true once the group is navigable.
 *
 * Primary source: `groupQueryable(status)` (the status plane reports every repo
 * finished extraction). Defense-in-depth for the already-indexed / "up to date"
 * fast path: when the rebuild touches 0 repos the status plane can report zero
 * repos (or a stale/empty engine sidecar), so `groupQueryable` stays false even
 * though the graph is on disk and servable. In that case we fall back to the SSE
 * feed's own terminal state — the job finished (`jobDone`) AND every registered
 * per-repo row is terminal (done/error, i.e. it already renders "Indexed"). This
 * mirrors the TUI, which lets the user proceed to the graph once the group is
 * queryable. Deliberately stays FALSE during active indexing (job not done and
 * some row still non-terminal), so the button can't unlock early.
 */
export function viewGraphEnabled(
  status: IndexStatusReply | undefined,
  jobDone: boolean,
  rows: Pick<ProgressRow, "phase">[],
): boolean {
  if (groupQueryable(status)) return true;
  return (
    jobDone &&
    rows.length > 0 &&
    rows.every((r) => r.phase === "done" || r.phase === "error")
  );
}

/**
 * The engine CPU/RSS block for the badges, or undefined when there's nothing
 * meaningful to show (missing status, or an all-zero engine-liveness sidecar —
 * the backend degrades a stale/absent sidecar to a zero block).
 */
export function engineStats(
  status: IndexStatusReply | undefined,
): IndexStatusEngine | undefined {
  if (!status) return undefined;
  const { cpu_pct, rss_mb } = status.engine;
  if (cpu_pct <= 0 && rss_mb <= 0) return undefined;
  return { cpu_pct, rss_mb };
}
