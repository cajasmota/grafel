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

import type {
  ProgressEvent,
  ProgressGroup,
  ProgressRow,
  RepoNesting,
} from "@/data/types";

/**
 * Stable key for a row. ONE row per repo: keyed by repo_slug only so a repo-
 * level event and its module-scoped counterpart collapse into a single row
 * (#5326). Previously this appended the module, splitting each repo into two.
 */
export function rowKey(e: Pick<ProgressEvent, "repo_slug">): string {
  return e.repo_slug;
}

/**
 * Seed one PENDING row per expected repo, so ALL repos are visible the moment
 * the Index step starts — even before any SSE event arrives, and even if some
 * early per-repo events are dropped under the broker's drop policy (#5340).
 * Real events fold into these seeded rows by repo_slug. Seeded rows sit at the
 * first phase ("scanning") with a ts of 0 so the very first real event for that
 * repo always wins the stale-ts / phase-advance checks in `fold`.
 *
 * `slugs` is the per-repo slug list the wizard registered for this index (one
 * per selected child git repo, or one per selected monorepo package, or 1 for a
 * single repo) — the same slugs runIndex POSTs to /api/v2/groups/from-scan.
 */
export function seedRows(slugs: string[]): Map<string, ProgressRow> {
  const m = new Map<string, ProgressRow>();
  for (const slug of slugs) {
    if (!slug) continue;
    m.set(slug, {
      key: slug,
      repoSlug: slug,
      phase: "scanning",
      filesDone: 0,
      filesTotal: 0,
      entitiesSoFar: 0,
      ts: 0,
    });
  }
  return m;
}

/**
 * Fold the group-scoped phase (from the group event, repo_slug === group) into a
 * monotonic running value — never regressing to a coarser phase. `fold` excludes
 * that event from the rows; this tracks its phase so `overallPhaseLabel` can show
 * the cross-repo pass ("Detecting cross-repo links…") in the header instead of a
 * spurious group row (#5340). Mirrors indexview.go foldEvent's groupPhase update.
 */
export function groupPhase(
  prev: ProgressRow["phase"] | undefined,
  e: ProgressEvent,
  groupSlug: string,
): ProgressRow["phase"] | undefined {
  if (!groupSlug || e.repo_slug !== groupSlug) return prev;
  if (!prev || PHASE_ORDER[e.phase] >= PHASE_ORDER[prev]) return e.phase;
  return prev;
}

/**
 * Mark every non-terminal row Done — called once the index completes
 * successfully so a row frozen on its last intermediate phase (its final SSE
 * events arrived after the rebuild RPC returned and the forwarder stopped)
 * agrees with the overall "Done · 100%" (#5348/#5340). Failures keep their
 * state. Files/entities counts are preserved. Mirrors indexview.go finalizeRows.
 */
export function finalizeRows(prev: Map<string, ProgressRow>): Map<string, ProgressRow> {
  let changed = false;
  const next = new Map(prev);
  for (const [k, r] of prev) {
    if (r.phase === "done" || r.phase === "error") continue;
    next.set(k, { ...r, phase: "done" });
    changed = true;
  }
  return changed ? next : prev;
}

/**
 * Monotonic phase order so a stale lower phase never overwrites a higher one.
 *
 * #5334 — the graph-assembly tail is split into granular passes. The order
 * mirrors the backend's real emission sequence so a later, finer phase never
 * regresses to a coarse one:
 *   buildDocument → materializing(coarse) → running_algorithms(coarse) →
 *   building_communities → computing_centrality → computing_flows →
 *   writing_graph → done.
 * detecting_links is a GROUP-level pass that runs on its own row (keyed by the
 * group slug) after every member repo is done; it only needs to precede that
 * row's terminal `done`, so it slots just below writing_graph.
 * The coarse phases (running_algorithms, materializing) are retained as
 * fallback bands for any older event still keyed on them.
 */
const PHASE_ORDER: Record<ProgressRow["phase"], number> = {
  scanning: 0,
  extracting_ast: 1,
  resolving_refs: 2,
  materializing: 3,
  running_algorithms: 4,
  building_communities: 5,
  computing_centrality: 6,
  computing_flows: 7,
  detecting_links: 8,
  writing_graph: 9,
  done: 10,
  error: 10,
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
  groupSlug?: string,
): Map<string, ProgressRow> {
  // The group-scoped event (repo_slug === group, the cross-repo links/flows
  // pass) is NOT a repo: it must never spawn a per-repo row (#5340). Drop it
  // here so it can't become a spurious "<group> · Indexed" row — its phase is
  // routed to the OVERALL label via groupPhase()/overallPhaseLabel instead.
  // Mirrors internal/cli/wiztui/indexview.go foldEvent.
  if (groupSlug && e.repo_slug === groupSlug) return prev;
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

/**
 * Number of bands the [0,100] bar is split into — one per non-terminal phase
 * index (0..9), so each granular assembly pass advances the bar by ~10% as the
 * repo crosses into it. Must equal the `done` PHASE_ORDER value so a repo at
 * the last non-terminal phase reads <100% and only `done` pegs to 100% (#5334).
 */
const PHASE_BANDS = 10;

/**
 * Phase-weighted completion fraction (0..1) for a single repo row.
 *
 *   base   = phaseIndex / PHASE_BANDS   (each phase is one band)
 *   within `extracting_ast` we have real file granularity, so add the
 *          filesDone/filesTotal slice of that one band.
 *   done   = 1 (100%)
 *   error  = counts as 100% so a failed repo doesn't peg the average low.
 *
 * The sub-progress-less assembly phases (resolving_refs, running_algorithms,
 * building_communities, computing_centrality, computing_flows, writing_graph)
 * still advance the bar via their phase band as the repo crosses into them —
 * that's what keeps the assembly tail from looking stuck (#5334).
 */
export function rowFraction(row: Pick<ProgressRow, "phase" | "filesDone" | "filesTotal">): number {
  if (row.phase === "done" || row.phase === "error") return 1;
  const idx = PHASE_ORDER[row.phase]; // 0..9 for non-terminal phases
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
  // #5334 — granular graph-assembly labels, kept in lock-step with the CLI
  // renderer (internal/cli/repair_broker.go) so both surfaces speak the same
  // human phrase for the same phase.
  building_communities: "Building communities…",
  computing_centrality: "Computing centrality…",
  detecting_links: "Detecting cross-repo links…",
  computing_flows: "Computing flows…",
  writing_graph: "Writing graph…",
  done: "Done",
  error: "Done",
};

/**
 * Overall-phase label for the wizard header, derived from the LEAST-advanced
 * active (non-terminal) repo — that's the phase the whole index is gated on.
 * When `terminal` (or every row is terminal / there are no active rows), the
 * label is "Done".
 */
export function overallPhaseLabel(
  rows: ProgressRow[],
  terminal?: boolean,
  groupPhaseValue?: ProgressRow["phase"],
): string {
  if (terminal) return "Done";
  const active = rows.filter((r) => r.phase !== "done" && r.phase !== "error");
  // Least-advanced active per-repo phase gates overall progress.
  const repoLabel =
    active.length === 0
      ? "Done"
      : PHASE_LABEL[
          active.reduce((min, r) =>
            PHASE_ORDER[r.phase] < PHASE_ORDER[min.phase] ? r : min,
          ).phase
        ];
  // Once every repo is terminal but the group-scoped pass (cross-repo links /
  // flows) is still in flight, surface THAT group phase in the header instead of
  // a stale "Done" — the group work runs after all repos finish (#5340). Mirrors
  // indexview.go overallLabel.
  if (groupPhaseValue && groupPhaseValue !== "done" && groupPhaseValue !== "error") {
    const leastActiveRank =
      active.length === 0
        ? PHASE_ORDER.done
        : active.reduce(
            (min, r) => Math.min(min, PHASE_ORDER[r.phase]),
            PHASE_ORDER.done,
          );
    if (repoLabel === "Done" || PHASE_ORDER[groupPhaseValue] >= leastActiveRank) {
      return PHASE_LABEL[groupPhaseValue];
    }
  }
  return repoLabel;
}

/**
 * Status rank for sort ordering — lower sorts HIGHER (nearer the top) so the
 * rows the user cares about are always in view (#5495):
 *
 *   0  indexing  — actively working (non-terminal AND it has emitted at least
 *                  one real event, i.e. ts > 0)
 *   1  queued    — registered but not started yet (a seeded row still at its
 *                  initial "scanning"/ts === 0 state)
 *   2  done/error — terminal; sinks to the bottom as it finishes
 *
 * As a row crosses into a terminal phase its rank rises from 0→2 and it drops
 * below every still-active/queued row, so with a 30-module group the rows that
 * are actively indexing stay pinned at the top while completed rows pile up at
 * the bottom. Single-repo / all-same-status lists are unaffected (every row
 * shares one rank, so the stable secondary key — repo then module — is what
 * orders them, exactly as before).
 */
export function statusRank(row: Pick<ProgressRow, "phase" | "ts">): number {
  if (row.phase === "done" || row.phase === "error") return 2;
  // A seeded, not-yet-started row sits at "scanning" with ts === 0 → queued.
  if (row.ts <= 0) return 1;
  return 0; // a real event has advanced it → actively indexing.
}

/**
 * Stable sort for rendering: status first (active → queued → done, #5495), then
 * the original repo/module order WITHIN each status band so rows don't jump
 * around as unrelated repos advance. The status key is the only change from the
 * historical pure repo/module sort; within one status band the order is
 * identical to before, so the small/single-repo case is unaffected.
 */
export function sortRows(rows: Iterable<ProgressRow>): ProgressRow[] {
  return [...rows].sort((a, b) => {
    const ra = statusRank(a);
    const rb = statusRank(b);
    if (ra !== rb) return ra - rb;
    if (a.repoSlug !== b.repoSlug) return a.repoSlug.localeCompare(b.repoSlug);
    return (a.module ?? "").localeCompare(b.module ?? "");
  });
}

/* ------------------------------------------------------------------
   Nested per-module rows for monorepos (#47 phase 2).

   In the web wizard each monorepo package is registered as its OWN repo_slug
   (so it gets its own flat progress row). nestRows regroups those sibling
   package rows UNDER a synthesized monorepo parent — parent header row + indented
   per-module children — mirroring the TUI (internal/cli/wiztui/fold.go's
   per-module nesting + the repo-scoped PhaseDone that lifts sibling module rows).
   Related-repo / single-repo groups (no nesting entry) stay FLAT: one repo group
   per row. This is a PRESENTATION-layer regroup over the already-folded rows, so
   it never reintroduces the dropped/duplicate rows the fold code was hardened
   against (#5326) — it only buckets existing rows.
   ------------------------------------------------------------------ */

/** True once every row in the set has reached a terminal (done/error) phase. */
function allTerminal(rows: ProgressRow[]): boolean {
  return rows.length > 0 && rows.every((r) => r.phase === "done" || r.phase === "error");
}

/**
 * Aggregate phase for a set of sibling rows: `done` once every row is terminal
 * (or the whole index is `complete`), otherwise the LEAST-advanced active
 * phase — the phase the group is gated on. Never reports a false failure: an
 * all-terminal group is `done` unless a row genuinely carries `error`.
 */
function groupPhaseFor(rows: ProgressRow[], complete: boolean): ProgressRow["phase"] {
  if (complete || allTerminal(rows)) {
    return rows.some((r) => r.phase === "error") ? "error" : "done";
  }
  const active = rows.filter((r) => r.phase !== "done" && r.phase !== "error");
  if (active.length === 0) return "done";
  return active.reduce((min, r) => (PHASE_ORDER[r.phase] < PHASE_ORDER[min.phase] ? r : min)).phase;
}

/**
 * Lift any non-terminal child to `done` once the parent group is Done — mirrors
 * fold.go's repo-scoped PhaseDone lifting sibling module rows that froze at
 * "Extracting AST…" because the later repo-scoped phases carried no module.
 * Preserves each child's file/entity counts.
 */
function liftChildrenToDone(rows: ProgressRow[]): ProgressRow[] {
  return rows.map((r) => (r.phase === "done" || r.phase === "error" ? r : { ...r, phase: "done" }));
}

/**
 * Regroup flat progress rows into nested groups. Monorepo package rows (those
 * with a `nesting` entry, sharing a `parentSlug`) collapse into ONE
 * `kind:"monorepo"` group with indented children; every other row becomes its
 * own `kind:"repo"` group. `complete` (the wizard's successful-completion
 * signal) forces every group + child to Done so a straggler frozen mid-phase
 * agrees with the overall "Done" (#5348 analogue). Groups are ordered
 * active → queued → done (statusRank on the group's representative phase), then
 * by label, so the rows the user cares about stay in view (#5495 analogue).
 */
export function nestRows(
  rows: ProgressRow[],
  nesting?: RepoNesting,
  complete = false,
): ProgressGroup[] {
  const groups: ProgressGroup[] = [];
  // Parent buckets, keyed by parentSlug, in first-seen order.
  const parents = new Map<string, { label: string; children: ProgressRow[] }>();

  for (const r of rows) {
    const entry = nesting?.[r.repoSlug];
    if (entry) {
      let bucket = parents.get(entry.parentSlug);
      if (!bucket) {
        bucket = { label: entry.parentLabel, children: [] };
        parents.set(entry.parentSlug, bucket);
      }
      // Stamp the module label for the indented child display.
      bucket.children.push({ ...r, module: entry.moduleLabel });
    } else {
      const row = complete && r.phase !== "done" && r.phase !== "error" ? { ...r, phase: "done" as const } : r;
      groups.push({
        key: `repo:${r.repoSlug}`,
        kind: "repo",
        label: r.repoSlug,
        phase: row.phase,
        row,
        children: [],
        enhancing: row.enhancing,
        reindexRequired: row.reindexRequired,
      });
    }
  }

  for (const [parentSlug, bucket] of parents) {
    const phase = groupPhaseFor(bucket.children, complete);
    const parentDone = phase === "done" || phase === "error";
    const children = sortRows(parentDone ? liftChildrenToDone(bucket.children) : bucket.children);
    groups.push({
      key: `mono:${parentSlug}`,
      kind: "monorepo",
      label: bucket.label,
      phase,
      children,
      enhancing: bucket.children.some((c) => c.enhancing),
      reindexRequired: bucket.children.some((c) => c.reindexRequired),
    });
  }

  // Active groups on top, queued next, done sinks — same rank scheme as sortRows.
  return groups.sort((a, b) => {
    const ra = groupStatusRank(a);
    const rb = groupStatusRank(b);
    if (ra !== rb) return ra - rb;
    return a.label.localeCompare(b.label);
  });
}

/** statusRank for a whole group, from its representative row/aggregate phase. */
function groupStatusRank(g: ProgressGroup): number {
  if (g.kind === "repo" && g.row) return statusRank(g.row);
  if (g.phase === "done" || g.phase === "error") return 2;
  // A monorepo whose children have all emitted is "indexing"; otherwise queued.
  return g.children.some((c) => c.ts > 0) ? 0 : 1;
}
