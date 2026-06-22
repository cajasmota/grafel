/* ============================================================
   index-progress-fold.ts — pure row-folding for the live indexing feed (#1527,
   dedup fix #5326).

   The daemon SSE stream (/api/index-progress/:group) emits a firehose of
   progress.Event records. For a single repo it emits BOTH a repo-level event
   (module="") AND, while extracting, a module-scoped event (module="<repo>").
   Keying rows by repo+module produced TWO rows per repo — a live repo-level row
   plus a stale "<repo> module" duplicate that froze mid-extraction (#5326 bug 2).

   The fix: there is exactly ONE row per repo, keyed by repo_slug alone. Module-
   scoped events fold into that same repo row, and the latest event wins so the
   row always reflects the most recent phase/file/entity counts. We retain the
   richest module label seen (for the optional "module" badge) but never split.

   This logic is extracted from use-index-progress.ts so it is unit-testable
   under `vitest run src/lib` without a DOM.
   ============================================================ */

import type { ProgressEvent, ProgressRow } from "@/data/types";

/**
 * Stable key for a row. ONE row per repo: keyed by repo_slug only so a repo-
 * level event and its module-scoped counterpart collapse into a single row
 * (#5326). Previously this appended the module, splitting each repo into two.
 */
export function rowKey(e: Pick<ProgressEvent, "repo_slug">): string {
  return e.repo_slug;
}

/** Monotonic phase order so a stale lower phase never overwrites a higher one. */
const PHASE_ORDER: Record<ProgressRow["phase"], number> = {
  scanning: 0,
  extracting_ast: 1,
  resolving_refs: 2,
  running_algorithms: 3,
  materializing: 4,
  done: 5,
  error: 5,
};

/**
 * Fold a new event into the existing row map. There is one row per repo; the
 * latest event per repo wins for phase/files/entities. We never regress
 * files_done or the phase (events can arrive slightly out of order under the
 * broker's drop policy), and a terminal (done/error) row is never pulled back
 * to an in-flight phase by a late module-scoped event.
 */
export function fold(
  prev: Map<string, ProgressRow>,
  e: ProgressEvent,
): Map<string, ProgressRow> {
  const key = rowKey(e);
  const existing = prev.get(key);
  // Ignore stale events that predate what we already have for this row.
  if (existing && e.ts < existing.ts) return prev;

  // Don't let a late, lower-ordered phase event overwrite a more-advanced phase
  // already shown for this repo (the duplicate stale-module-row symptom #5326).
  const advance = !existing || PHASE_ORDER[e.phase] >= PHASE_ORDER[existing.phase];
  const phase = advance ? e.phase : existing.phase;

  const next = new Map(prev);
  next.set(key, {
    key,
    repoSlug: e.repo_slug,
    // One row per repo (#5326): each monorepo package is already registered as
    // its OWN repo (distinct repo_slug), so the module-scoped event is just a
    // redundant duplicate of the repo row. We intentionally drop the module
    // label here so a repo never splits into a "<repo> module" duplicate and
    // single repos don't get a spurious "module" badge. Keep a module label only
    // when it genuinely differs from the repo slug (true sub-module reporting).
    module: e.module && e.module !== e.repo_slug ? e.module : existing?.module,
    phase,
    filesDone: existing ? Math.max(existing.filesDone, e.files_done) : e.files_done,
    filesTotal: e.files_total || existing?.filesTotal || 0,
    entitiesSoFar: Math.max(existing?.entitiesSoFar ?? 0, e.entities_so_far),
    currentFile: e.current_file || existing?.currentFile,
    etaMs: e.eta_ms,
    error: e.error || existing?.error,
    ts: e.ts,
  });
  return next;
}

/** True once the per-repo feed shows every repo in a terminal (done/error) phase. */
export function rowsTerminal(rows: ProgressRow[]): boolean {
  return rows.length > 0 && rows.every((r) => r.phase === "done" || r.phase === "error");
}

/** Stable sort for rendering: by repo, then module label. */
export function sortRows(rows: Iterable<ProgressRow>): ProgressRow[] {
  return [...rows].sort((a, b) => {
    if (a.repoSlug !== b.repoSlug) return a.repoSlug.localeCompare(b.repoSlug);
    return (a.module ?? "").localeCompare(b.module ?? "");
  });
}
