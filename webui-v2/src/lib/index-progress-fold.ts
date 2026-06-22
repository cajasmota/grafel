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

/**
 * True once the per-repo feed shows EVERY expected repo in a terminal
 * (done/error) phase.
 *
 * `expectedRepos` is the number of repos the wizard registered for this index
 * (one per selected child git repo, or one per selected monorepo package, or 1
 * for a single repo). It is REQUIRED to safely fire feed-terminal: under the
 * SSE broker's drop policy the first repo can reach `done` before the second
 * repo emits a single event, so `rows = [first:done]` would look terminal even
 * though more repos are still coming. Gating on `rows.length >= expectedRepos`
 * prevents that early fire (#5326) — only when all expected rows exist AND each
 * is terminal do we report terminal.
 *
 * When `expectedRepos` is unknown (undefined / 0), we DELIBERATELY do not fire
 * feed-terminal on partial rows: we return false and let the job poller be the
 * terminal source of truth, so a premature "all rows so far are done" can never
 * tear the feed down before later repos report.
 */
export function rowsTerminal(rows: ProgressRow[], expectedRepos?: number): boolean {
  if (rows.length === 0) return false;
  // Without a known expected count we can't tell "all repos done" from "the
  // only repos seen SO FAR are done" — defer to the job poller (return false).
  if (!expectedRepos || expectedRepos <= 0) return false;
  if (rows.length < expectedRepos) return false;
  return rows.every((r) => r.phase === "done" || r.phase === "error");
}

/* ------------------------------------------------------------------
   Aggregate progress + phase label (#5332).

   The wizard's MAIN progress bar used to be driven solely by the coarse
   job-poller value, which barely moves during indexing (it stayed near 0%
   even while every repo was at "Materializing"/"Indexed"), so the bar looked
   frozen. These pure helpers derive a real aggregate percentage and a
   human-readable overall-phase label straight from the per-repo feed rows,
   so the bar advances as repos cross phase boundaries — including through the
   long, sub-progress-less "Materializing" phase.
   ------------------------------------------------------------------ */

/** Number of bands the [0,100] bar is split into — one per non-terminal phase. */
const PHASE_BANDS = 5;

/**
 * Phase-weighted completion fraction (0..1) for a single repo row.
 *
 *   base   = phaseIndex / 5     (each phase is a 20% band)
 *   within `extracting_ast` we have real file granularity, so add the
 *          filesDone/filesTotal slice of that one band.
 *   done   = 1 (100%)
 *   error  = counts as 100% so a failed repo doesn't peg the average low.
 *
 * The other sub-progress-less phases (resolving_refs / running_algorithms /
 * materializing) still advance the bar via their phase band as the repo
 * crosses into them — that's what keeps "Materializing" from looking stuck.
 */
export function rowFraction(row: Pick<ProgressRow, "phase" | "filesDone" | "filesTotal">): number {
  if (row.phase === "done" || row.phase === "error") return 1;
  const idx = PHASE_ORDER[row.phase]; // 0..4 for non-terminal phases
  const base = idx / PHASE_BANDS;
  if (row.phase === "extracting_ast" && row.filesTotal > 0) {
    const slice = Math.min(1, Math.max(0, row.filesDone / row.filesTotal));
    return base + slice / PHASE_BANDS;
  }
  return base;
}

/**
 * Aggregate progress (0..100) across all repos, for the wizard's main bar.
 *
 * Each repo contributes its phase-weighted `rowFraction`; we average across
 * `expectedRepos` when known (so repos that have not reported yet count as 0
 * and the bar doesn't jump when a late repo first appears), otherwise across
 * the rows we actually have. Returns 0 when there's nothing to show.
 *
 * Callers should still display 100 once the wizard is terminal — this helper
 * only reflects the rows it's given (a repo can be at "done" while another is
 * mid-extraction).
 */
export function aggregateProgress(
  rows: Pick<ProgressRow, "phase" | "filesDone" | "filesTotal">[],
  expectedRepos?: number,
): number {
  const denom =
    expectedRepos && expectedRepos > 0 ? Math.max(expectedRepos, rows.length) : rows.length;
  if (denom <= 0) return 0;
  const sum = rows.reduce((acc, r) => acc + rowFraction(r), 0);
  const pct = (sum / denom) * 100;
  // Clamp into [0,100]; the wizard owns the final "100% once terminal" jump.
  return Math.min(100, Math.max(0, Math.round(pct)));
}

/** Human label for an overall indexing phase, shown in the wizard header. */
const PHASE_LABEL: Record<ProgressRow["phase"], string> = {
  scanning: "Scanning…",
  extracting_ast: "Extracting AST…",
  resolving_refs: "Resolving references…",
  running_algorithms: "Running algorithms…",
  materializing: "Materializing graph…",
  done: "Done",
  error: "Done",
};

/**
 * Overall-phase label for the wizard header, derived from the LEAST-advanced
 * active (non-terminal) repo — that's the phase the whole index is gated on.
 * When `terminal` (or every row is terminal / there are no active rows), the
 * label is "Done".
 */
export function overallPhaseLabel(rows: ProgressRow[], terminal?: boolean): string {
  if (terminal) return "Done";
  const active = rows.filter((r) => r.phase !== "done" && r.phase !== "error");
  if (active.length === 0) return "Done";
  // Least-advanced active phase gates overall progress.
  const least = active.reduce((min, r) =>
    PHASE_ORDER[r.phase] < PHASE_ORDER[min.phase] ? r : min,
  );
  return PHASE_LABEL[least.phase];
}

/** Stable sort for rendering: by repo, then module label. */
export function sortRows(rows: Iterable<ProgressRow>): ProgressRow[] {
  return [...rows].sort((a, b) => {
    if (a.repoSlug !== b.repoSlug) return a.repoSlug.localeCompare(b.repoSlug);
    return (a.module ?? "").localeCompare(b.module ?? "");
  });
}
