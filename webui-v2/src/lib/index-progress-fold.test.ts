import { describe, it, expect } from "vitest";

import {
  aggregateProgress,
  finalizeRows,
  fold,
  groupPhase,
  nestRows,
  overallPhaseLabel,
  rowFraction,
  rowKey,
  rowsTerminal,
  seedRows,
  sortRows,
  statusRank,
  streamTerminal,
} from "./index-progress-fold";
import type { ProgressEvent, ProgressRow, RepoNesting } from "@/data/types";

/** Minimal ProgressRow builder for the aggregate/label tests. */
function row(p: Partial<ProgressRow>): ProgressRow {
  return {
    key: p.repoSlug ?? "backend",
    repoSlug: "backend",
    phase: "extracting_ast",
    filesDone: 0,
    filesTotal: 0,
    entitiesSoFar: 0,
    ts: 1,
    ...p,
  };
}

function ev(p: Partial<ProgressEvent>): ProgressEvent {
  return {
    group_slug: "g",
    repo_slug: "backend",
    phase: "extracting_ast",
    files_done: 0,
    files_total: 0,
    entities_so_far: 0,
    ts: 1,
    ...p,
  };
}

function applyAll(events: ProgressEvent[]): ProgressRow[] {
  let m = new Map<string, ProgressRow>();
  for (const e of events) m = fold(m, e);
  return sortRows(m.values());
}

describe("rowKey", () => {
  it("keys by repo_slug only — a module-scoped event collapses into the repo row", () => {
    expect(rowKey({ repo_slug: "backend" })).toBe("backend");
    // The historical bug: module appended → second key → duplicate row.
    expect(rowKey({ repo_slug: "backend" })).toBe(rowKey({ repo_slug: "backend" }));
  });
});

describe("fold — one row per repo (#5326 bug 2)", () => {
  it("merges a repo-level event and its module-scoped duplicate into ONE row", () => {
    const rows = applyAll([
      // stale module-scoped event froze at extraction
      ev({ repo_slug: "backend", module: "backend", phase: "extracting_ast", files_done: 160, files_total: 173, ts: 1 }),
      // repo-level event advanced further
      ev({ repo_slug: "backend", phase: "running_algorithms", files_done: 173, files_total: 173, entities_so_far: 3272, ts: 2 }),
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0].repoSlug).toBe("backend");
    expect(rows[0].phase).toBe("running_algorithms");
    expect(rows[0].filesDone).toBe(173);
    expect(rows[0].entitiesSoFar).toBe(3272);
  });

  it("keeps two SEPARATE repos as two rows", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 1 }),
      ev({ repo_slug: "frontend", phase: "done", ts: 1 }),
    ]);
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
  });

  it("does not let a late lower-phase event regress a more-advanced phase", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", files_done: 173, files_total: 173, ts: 5 }),
      // a delayed module event from mid-extraction arrives later (higher ts)
      ev({ repo_slug: "backend", module: "backend", phase: "extracting_ast", files_done: 160, files_total: 173, ts: 6 }),
    ]);
    expect(rows).toHaveLength(1);
    expect(rows[0].phase).toBe("done");
    expect(rows[0].filesDone).toBe(173);
  });

  it("ignores events older than what the row already has", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "running_algorithms", files_done: 100, ts: 10 }),
      ev({ repo_slug: "backend", phase: "scanning", files_done: 1, ts: 2 }),
    ]);
    expect(rows[0].phase).toBe("running_algorithms");
    expect(rows[0].filesDone).toBe(100);
  });

  it("does not badge a single repo as a module when module == repo_slug", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", module: "backend", phase: "done", ts: 1 }),
    ]);
    expect(rows[0].module).toBeUndefined();
  });

  it("retains a genuine sub-module label distinct from the repo slug", () => {
    const rows = applyAll([
      ev({ repo_slug: "monorepo", module: "packages/api", phase: "done", ts: 1 }),
    ]);
    expect(rows[0].module).toBe("packages/api");
  });
});

describe("rowsTerminal — wizard terminal fallback (#5326 bug 1)", () => {
  it("is false until every repo row is terminal", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 1 }),
      ev({ repo_slug: "frontend", phase: "running_algorithms", ts: 1 }),
    ]);
    expect(rowsTerminal(rows, 2)).toBe(false);
  });

  it("is true once all repo rows reach done/error", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 1 }),
      ev({ repo_slug: "frontend", phase: "error", error: "boom", ts: 1 }),
    ]);
    expect(rowsTerminal(rows, 2)).toBe(true);
  });

  it("is false for an empty feed (nothing to be terminal about)", () => {
    expect(rowsTerminal([], 2)).toBe(false);
  });
});

describe("rowsTerminal — expected-repo-count gate (#5326 multi-repo regression)", () => {
  it("THE BUG: repo A done while repo B has not emitted yet → NOT terminal", () => {
    // The exact race that broke multi-repo wizards: under the broker's drop
    // policy the first repo finishes before the second emits a single event, so
    // only one row exists. Without the expected count this looked terminal and
    // the feed tore down before repo B ever appeared.
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", files_done: 173, files_total: 173, ts: 5 }),
    ]);
    expect(rows).toHaveLength(1);
    expect(rowsTerminal(rows, 2)).toBe(false);
  });

  it("both expected repos done → terminal", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 5 }),
      ev({ repo_slug: "frontend", phase: "done", ts: 5 }),
    ]);
    expect(rowsTerminal(rows, 2)).toBe(true);
  });

  it("all expected rows present but one still in-flight → not terminal", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 5 }),
      ev({ repo_slug: "frontend", phase: "resolving_refs", ts: 5 }),
    ]);
    expect(rowsTerminal(rows, 2)).toBe(false);
  });

  it("unknown expectedRepos → never prematurely terminal on partial rows", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 5 }),
    ]);
    // Defer to the job poller rather than firing early.
    expect(rowsTerminal(rows, undefined)).toBe(false);
    expect(rowsTerminal(rows)).toBe(false);
    expect(rowsTerminal(rows, 0)).toBe(false);
  });

  it("regression: single-repo group still reaches terminal (expectedRepos = 1)", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", files_done: 173, files_total: 173, ts: 5 }),
    ]);
    expect(rowsTerminal(rows, 1)).toBe(true);
  });

  it("more rows than expected (defensive) → still terminal when all done", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 5 }),
      ev({ repo_slug: "frontend", phase: "done", ts: 5 }),
      ev({ repo_slug: "shared", phase: "done", ts: 5 }),
    ]);
    expect(rowsTerminal(rows, 2)).toBe(true);
  });
});

describe("streamTerminal — group-scoped terminal fallback", () => {
  it("treats group done as terminal even when a repo row is stuck mid-phase", () => {
    expect(
      streamTerminal(
        [row({ repoSlug: "backend", phase: "extracting_ast", ts: 1 })],
        2,
        "done",
      ),
    ).toBe(true);
  });

  it("falls back to per-repo terminal when no group terminal is available", () => {
    expect(
      streamTerminal(
        [
          row({ repoSlug: "backend", phase: "done", ts: 1 }),
          row({ repoSlug: "frontend", phase: "done", ts: 2 }),
        ],
        2,
      ),
    ).toBe(true);
  });
});

describe("rowFraction — phase-weighted per-repo completion (#5332)", () => {
  it("done / error count as 100%", () => {
    expect(rowFraction(row({ phase: "done" }))).toBe(1);
    expect(rowFraction(row({ phase: "error" }))).toBe(1);
  });

  it("scanning is the bottom band (0%)", () => {
    expect(rowFraction(row({ phase: "scanning" }))).toBe(0);
  });

  it("extracting_ast adds the files slice within its band", () => {
    // base = 1/10 = 0.1; +50% of the 0.1 band = 0.15 (#5334: 10 bands)
    expect(rowFraction(row({ phase: "extracting_ast", filesDone: 50, filesTotal: 100 }))).toBeCloseTo(0.15);
  });

  it("sub-progress-less phases advance only via their band", () => {
    // #5334 — 10 bands, emission-order indices.
    expect(rowFraction(row({ phase: "resolving_refs" }))).toBeCloseTo(0.2);
    expect(rowFraction(row({ phase: "materializing" }))).toBeCloseTo(0.3);
    expect(rowFraction(row({ phase: "running_algorithms" }))).toBeCloseTo(0.4);
  });

  it("granular graph-assembly phases each occupy a higher band (#5334)", () => {
    expect(rowFraction(row({ phase: "building_communities" }))).toBeCloseTo(0.5);
    expect(rowFraction(row({ phase: "computing_centrality" }))).toBeCloseTo(0.6);
    expect(rowFraction(row({ phase: "computing_flows" }))).toBeCloseTo(0.7);
    expect(rowFraction(row({ phase: "detecting_links" }))).toBeCloseTo(0.8);
    expect(rowFraction(row({ phase: "writing_graph" }))).toBeCloseTo(0.9);
  });

  it("rowFraction is strictly increasing across the assembly sequence (#5334)", () => {
    const seq: ProgressRow["phase"][] = [
      "scanning",
      "extracting_ast",
      "resolving_refs",
      "materializing",
      "running_algorithms",
      "building_communities",
      "computing_centrality",
      "computing_flows",
      "detecting_links",
      "writing_graph",
      "done",
    ];
    let last = -1;
    for (const phase of seq) {
      const f = rowFraction(row({ phase }));
      expect(f).toBeGreaterThan(last);
      last = f;
    }
  });
});

describe("aggregateProgress (#5332)", () => {
  it("single repo extracting at 50% files → sensible mid value", () => {
    const p = aggregateProgress([row({ phase: "extracting_ast", filesDone: 50, filesTotal: 100 })]);
    expect(p).toBe(15); // 0.15 * 100 (#5334: 10 bands)
  });

  it("one repo done + one extracting → between the two", () => {
    const p = aggregateProgress([
      row({ repoSlug: "a", phase: "done" }),
      row({ repoSlug: "b", phase: "extracting_ast", filesDone: 0, filesTotal: 100 }),
    ]);
    // (1 + 0.1) / 2 = 0.55 (#5334: 10 bands)
    expect(p).toBe(55);
    expect(p).toBeGreaterThan(0);
    expect(p).toBeLessThan(100);
  });

  it("all done → 100", () => {
    const p = aggregateProgress([
      row({ repoSlug: "a", phase: "done" }),
      row({ repoSlug: "b", phase: "done" }),
    ]);
    expect(p).toBe(100);
  });

  it("unknown expectedRepos → averages over present rows", () => {
    const p = aggregateProgress([row({ phase: "materializing" })], undefined);
    expect(p).toBe(30); // 0.3 * 100 (#5334: 10 bands)
  });

  it("expectedRepos counts not-yet-reporting repos as 0 (bar doesn't jump)", () => {
    // 1 of 4 expected repos reporting, that one done → 1/4 = 25%
    const p = aggregateProgress([row({ phase: "done" })], 4);
    expect(p).toBe(25);
  });

  it("empty / zero denominator → 0", () => {
    expect(aggregateProgress([])).toBe(0);
    expect(aggregateProgress([], 0)).toBe(0);
  });

  it("is clamped to [0,100]", () => {
    const p = aggregateProgress([row({ phase: "done" })], 1);
    expect(p).toBe(100);
  });

  it("does not go backwards across a normal phase sequence (monotonic-ish)", () => {
    const seq: ProgressRow["phase"][] = [
      "scanning",
      "extracting_ast",
      "resolving_refs",
      "materializing",
      "running_algorithms",
      "building_communities",
      "computing_centrality",
      "computing_flows",
      "detecting_links",
      "writing_graph",
      "done",
    ];
    let last = -1;
    for (const phase of seq) {
      const p = aggregateProgress([row({ phase, filesDone: 100, filesTotal: 100 })], 1);
      expect(p).toBeGreaterThanOrEqual(last);
      last = p;
    }
  });
});

describe("overallPhaseLabel (#5332)", () => {
  it("maps each phase to its human label", () => {
    expect(overallPhaseLabel([row({ phase: "scanning" })])).toBe("Scanning…");
    expect(overallPhaseLabel([row({ phase: "extracting_ast" })])).toBe("Extracting AST…");
    expect(overallPhaseLabel([row({ phase: "resolving_refs" })])).toBe("Resolving references…");
    expect(overallPhaseLabel([row({ phase: "running_algorithms" })])).toBe("Running algorithms…");
    expect(overallPhaseLabel([row({ phase: "materializing" })])).toBe("Materializing graph…");
  });

  it("maps the granular graph-assembly phases to friendly labels (#5334)", () => {
    expect(overallPhaseLabel([row({ phase: "building_communities" })])).toBe("Building communities…");
    expect(overallPhaseLabel([row({ phase: "computing_centrality" })])).toBe("Computing centrality…");
    expect(overallPhaseLabel([row({ phase: "detecting_links" })])).toBe("Detecting cross-repo links…");
    expect(overallPhaseLabel([row({ phase: "computing_flows" })])).toBe("Computing flows…");
    expect(overallPhaseLabel([row({ phase: "writing_graph" })])).toBe("Writing graph…");
  });

  it("reflects the LEAST-advanced active repo", () => {
    const label = overallPhaseLabel([
      row({ repoSlug: "a", phase: "materializing" }),
      row({ repoSlug: "b", phase: "extracting_ast" }),
    ]);
    expect(label).toBe("Extracting AST…");
  });

  it("ignores terminal rows when picking the least-advanced active phase", () => {
    const label = overallPhaseLabel([
      row({ repoSlug: "a", phase: "done" }),
      row({ repoSlug: "b", phase: "materializing" }),
    ]);
    expect(label).toBe("Materializing graph…");
  });

  it("terminal flag → Done", () => {
    expect(overallPhaseLabel([row({ phase: "extracting_ast" })], true)).toBe("Done");
  });

  it("all rows terminal → Done", () => {
    expect(overallPhaseLabel([row({ phase: "done" })])).toBe("Done");
    expect(overallPhaseLabel([])).toBe("Done");
  });
});

/* ----------------------------------------------------------------------------
   #5340 — per-repo rows in the dashboard index wizard: exclude the group-scoped
   event from rows (route to overall), seed rows from the repo list, finalize on
   completion.
   -------------------------------------------------------------------------- */

/** Fold a stream WITH a known group slug, tracking groupPhase alongside rows. */
function applyWithGroup(
  events: ProgressEvent[],
  group: string,
  seedSlugs?: string[],
): { rows: ProgressRow[]; groupPhase: ProgressRow["phase"] | undefined } {
  let m = seedSlugs ? seedRows(seedSlugs) : new Map<string, ProgressRow>();
  let gp: ProgressRow["phase"] | undefined;
  for (const e of events) {
    gp = groupPhase(gp, e, group);
    m = fold(m, e, group);
  }
  return { rows: sortRows(m.values()), groupPhase: gp };
}

describe("fold — group-scoped event is NOT a per-repo row (#5340)", () => {
  it("realistic order: backend + frontend rows only, NO group row, group phase in overall", () => {
    const { rows, groupPhase: gp } = applyWithGroup(
      [
        ev({ repo_slug: "backend", phase: "extracting_ast", files_done: 50, files_total: 100, ts: 1 }),
        ev({ repo_slug: "frontend", phase: "extracting_ast", files_done: 20, files_total: 80, ts: 2 }),
        ev({ repo_slug: "backend", phase: "done", ts: 3 }),
        ev({ repo_slug: "frontend", phase: "done", ts: 4 }),
        // group-scoped cross-repo pass — must NOT spawn a row
        ev({ repo_slug: "ivivo", phase: "detecting_links", ts: 5 }),
        ev({ repo_slug: "ivivo", phase: "done", ts: 6 }),
      ],
      "ivivo",
    );
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
    expect(rows.every((r) => r.phase === "done")).toBe(true);
    expect(rows.find((r) => r.repoSlug === "ivivo")).toBeUndefined();
    // group phase tracked; once both repos terminal, overall surfaces it
    expect(gp).toBe("done");
    // mid-pass: while detecting_links is live the overall shows it
    const mid = applyWithGroup(
      [
        ev({ repo_slug: "backend", phase: "done", ts: 1 }),
        ev({ repo_slug: "frontend", phase: "done", ts: 2 }),
        ev({ repo_slug: "ivivo", phase: "detecting_links", ts: 3 }),
      ],
      "ivivo",
    );
    expect(mid.rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
    expect(overallPhaseLabel(mid.rows, false, mid.groupPhase)).toBe("Detecting cross-repo links…");
  });

  it("group-event-FIRST ordering still yields no group row", () => {
    const { rows } = applyWithGroup(
      [
        // group terminal arrives before any per-repo event (the live bug shape)
        ev({ repo_slug: "ivivo", phase: "detecting_links", ts: 1 }),
        ev({ repo_slug: "ivivo", phase: "done", ts: 2 }),
        ev({ repo_slug: "backend", phase: "done", ts: 3 }),
        ev({ repo_slug: "frontend", phase: "done", ts: 4 }),
      ],
      "ivivo",
    );
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
  });

  it("interleaved group + repo events: group never becomes a row", () => {
    const { rows } = applyWithGroup(
      [
        ev({ repo_slug: "backend", phase: "extracting_ast", ts: 1 }),
        ev({ repo_slug: "ivivo", phase: "detecting_links", ts: 2 }),
        ev({ repo_slug: "frontend", phase: "extracting_ast", ts: 3 }),
        ev({ repo_slug: "ivivo", phase: "computing_flows", ts: 4 }),
        ev({ repo_slug: "backend", phase: "done", ts: 5 }),
        ev({ repo_slug: "frontend", phase: "done", ts: 6 }),
      ],
      "ivivo",
    );
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
  });

  it("single-repo case is unaffected (one repo → one row)", () => {
    const { rows } = applyWithGroup(
      [ev({ repo_slug: "solo", phase: "done", ts: 1 })],
      "solo-group",
    );
    expect(rows.map((r) => r.repoSlug)).toEqual(["solo"]);
  });

  it("no groupSlug passed → legacy behavior, group-named event still a row", () => {
    // Back-compat: callers that don't pass a group slug get the old folding.
    const rows = applyAll([ev({ repo_slug: "ivivo", phase: "done", ts: 1 })]);
    expect(rows.map((r) => r.repoSlug)).toEqual(["ivivo"]);
  });

  it("group phase is monotonic — a late coarse group event doesn't regress it", () => {
    let gp: ProgressRow["phase"] | undefined;
    // writing_graph(9) then a stale detecting_links(8) must NOT regress.
    gp = groupPhase(gp, ev({ repo_slug: "g", phase: "writing_graph" }), "g");
    gp = groupPhase(gp, ev({ repo_slug: "g", phase: "detecting_links" }), "g");
    expect(gp).toBe("writing_graph");
  });
});

describe("seedRows — show every repo before any event (#5340)", () => {
  it("seeds one pending row per slug", () => {
    const m = seedRows(["backend", "frontend"]);
    const rows = sortRows(m.values());
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
    expect(rows.every((r) => r.phase === "scanning")).toBe(true);
    expect(rows.every((r) => r.ts === 0)).toBe(true);
  });

  it("real events fold into the seeded rows by repo_slug (no duplicate rows)", () => {
    const { rows } = applyWithGroup(
      [
        ev({ repo_slug: "backend", phase: "extracting_ast", files_done: 10, files_total: 100, ts: 1 }),
        ev({ repo_slug: "backend", phase: "done", ts: 2 }),
        ev({ repo_slug: "frontend", phase: "done", ts: 3 }),
      ],
      "ivivo",
      ["backend", "frontend"],
    );
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend"]);
    expect(rows.every((r) => r.phase === "done")).toBe(true);
  });

  it("a repo that never emits stays visible (seeded, pending)", () => {
    const { rows } = applyWithGroup(
      [ev({ repo_slug: "backend", phase: "done", ts: 1 })],
      "ivivo",
      ["backend", "frontend"],
    );
    // Both repos are present; the still-queued frontend now sorts ABOVE the
    // completed backend (active/queued on top, done sinks — #5495).
    expect(new Set(rows.map((r) => r.repoSlug))).toEqual(new Set(["backend", "frontend"]));
    expect(rows.map((r) => r.repoSlug)).toEqual(["frontend", "backend"]);
    const fe = rows.find((r) => r.repoSlug === "frontend")!;
    expect(fe.phase).toBe("scanning"); // still seeded
  });

  it("skips empty slugs", () => {
    expect(sortRows(seedRows(["", "backend"]).values()).map((r) => r.repoSlug)).toEqual(["backend"]);
  });
});

describe("sortRows / statusRank — active on top, done sinks (#5495)", () => {
  it("ranks indexing(0) < queued(1) < done/error(2)", () => {
    expect(statusRank(row({ phase: "extracting_ast", ts: 5 }))).toBe(0);
    expect(statusRank(row({ phase: "scanning", ts: 0 }))).toBe(1); // seeded/queued
    expect(statusRank(row({ phase: "done", ts: 9 }))).toBe(2);
    expect(statusRank(row({ phase: "error", ts: 9 }))).toBe(2);
  });

  it("a still-scanning row that has emitted (ts > 0) counts as actively indexing", () => {
    expect(statusRank(row({ phase: "scanning", ts: 3 }))).toBe(0);
  });

  it("sorts active rows above queued above completed, regardless of name", () => {
    const rows = sortRows([
      row({ repoSlug: "zeta", phase: "done", ts: 9 }), // completed
      row({ repoSlug: "alpha", phase: "scanning", ts: 0 }), // queued (seeded)
      row({ repoSlug: "mu", phase: "extracting_ast", ts: 4 }), // indexing
    ]);
    expect(rows.map((r) => r.repoSlug)).toEqual(["mu", "alpha", "zeta"]);
  });

  it("is stable by name WITHIN a status band", () => {
    const rows = sortRows([
      row({ repoSlug: "frontend", phase: "extracting_ast", ts: 4 }),
      row({ repoSlug: "backend", phase: "resolving_refs", ts: 4 }),
      row({ repoSlug: "api", phase: "done", ts: 9 }),
    ]);
    // backend, frontend are both indexing → name order; api (done) sinks last.
    expect(rows.map((r) => r.repoSlug)).toEqual(["backend", "frontend", "api"]);
  });

  it("single-repo / all-same-status list is unchanged (name order preserved)", () => {
    const rows = sortRows([
      row({ repoSlug: "backend", phase: "done", ts: 9 }),
      row({ repoSlug: "api", phase: "done", ts: 9 }),
    ]);
    expect(rows.map((r) => r.repoSlug)).toEqual(["api", "backend"]);
  });
});

describe("finalizeRows — mark stuck rows Done on completion (#5348/#5340)", () => {
  it("advances a row frozen mid-phase to done", () => {
    const m = seedRows(["backend", "frontend"]);
    let m2 = fold(m, ev({ repo_slug: "backend", phase: "done", ts: 1 }), "g");
    // frontend froze at building_communities (its final events were dropped)
    m2 = fold(m2, ev({ repo_slug: "frontend", phase: "building_communities", ts: 2 }), "g");
    const finalized = sortRows(finalizeRows(m2).values());
    expect(finalized.every((r) => r.phase === "done")).toBe(true);
  });

  it("leaves error rows untouched", () => {
    let m = new Map<string, ProgressRow>();
    m = fold(m, ev({ repo_slug: "backend", phase: "error", error: "boom", ts: 1 }), "g");
    m = fold(m, ev({ repo_slug: "frontend", phase: "resolving_refs", ts: 2 }), "g");
    const finalized = sortRows(finalizeRows(m).values());
    expect(finalized.find((r) => r.repoSlug === "backend")!.phase).toBe("error");
    expect(finalized.find((r) => r.repoSlug === "frontend")!.phase).toBe("done");
  });

  it("preserves file/entity counts when finalizing", () => {
    let m = new Map<string, ProgressRow>();
    m = fold(m, ev({ repo_slug: "backend", phase: "writing_graph", files_done: 9, files_total: 9, entities_so_far: 42, ts: 1 }), "g");
    const r = sortRows(finalizeRows(m).values())[0];
    expect(r.phase).toBe("done");
    expect(r.entitiesSoFar).toBe(42);
    expect(r.filesDone).toBe(9);
  });

  it("returns the same map (no-op) when nothing to finalize", () => {
    let m = new Map<string, ProgressRow>();
    m = fold(m, ev({ repo_slug: "backend", phase: "done", ts: 1 }), "g");
    expect(finalizeRows(m)).toBe(m);
  });
});

/* ----------------------------------------------------------------------------
   #47 phase 2 — nested per-module rows for monorepos. Each monorepo package is
   registered as its own repo_slug, so nestRows GROUPS the sibling package rows
   under a synthesized monorepo parent (mirroring the TUI's per-module nesting),
   while related-repo (non-monorepo) groups stay flat. When the parent reaches
   Done, any child still mid-phase is lifted to Done (mirrors fold.go's repo-
   scoped PhaseDone lifting sibling module rows).
   -------------------------------------------------------------------------- */

/** A two-package monorepo nesting descriptor keyed by child repo slug. */
function monorepoNesting(): RepoNesting {
  return {
    "shop-api": { repoSlug: "shop-api", parentSlug: "shop", parentLabel: "shop", moduleLabel: "packages/api" },
    "shop-web": { repoSlug: "shop-web", parentSlug: "shop", parentLabel: "shop", moduleLabel: "packages/web" },
  };
}

describe("nestRows — nested per-module rows for monorepos (#47 phase 2)", () => {
  it("nests sibling package rows under a single monorepo parent group", () => {
    const rows = [
      row({ repoSlug: "shop-api", phase: "extracting_ast", filesDone: 5, filesTotal: 10, ts: 3 }),
      row({ repoSlug: "shop-web", phase: "resolving_refs", ts: 3 }),
    ];
    const groups = nestRows(rows, monorepoNesting());
    expect(groups).toHaveLength(1);
    const g = groups[0];
    expect(g.kind).toBe("monorepo");
    expect(g.label).toBe("shop");
    expect(g.children.map((c) => c.repoSlug).sort()).toEqual(["shop-api", "shop-web"]);
    // The child rows carry the module label for display.
    expect(g.children.find((c) => c.repoSlug === "shop-api")!.module).toBe("packages/api");
    // Parent phase reflects the least-advanced active child (extracting_ast).
    expect(g.phase).toBe("extracting_ast");
  });

  it("child module rows flip to Done when the parent reaches Done", () => {
    // shop-api finished; shop-web froze mid-phase (its final events were dropped).
    const rows = [
      row({ repoSlug: "shop-api", phase: "done", ts: 5 }),
      row({ repoSlug: "shop-web", phase: "building_communities", ts: 5 }),
    ];
    // complete = the job/parent reached Done → lift the stragglers.
    const groups = nestRows(rows, monorepoNesting(), true);
    expect(groups).toHaveLength(1);
    expect(groups[0].phase).toBe("done");
    expect(groups[0].children.every((c) => c.phase === "done")).toBe(true);
  });

  it("a monorepo parent is Done once every child is terminal (no complete flag needed)", () => {
    const rows = [
      row({ repoSlug: "shop-api", phase: "done", ts: 5 }),
      row({ repoSlug: "shop-web", phase: "done", ts: 5 }),
    ];
    const groups = nestRows(rows, monorepoNesting());
    expect(groups[0].phase).toBe("done");
  });

  it("keeps related-repo (non-monorepo) rows FLAT — one repo group each", () => {
    const rows = [
      row({ repoSlug: "backend", phase: "extracting_ast", ts: 3 }),
      row({ repoSlug: "frontend", phase: "done", ts: 3 }),
    ];
    const groups = nestRows(rows, {}); // no nesting → all flat
    expect(groups.map((g) => g.kind)).toEqual(["repo", "repo"]);
    expect(groups.map((g) => g.label).sort()).toEqual(["backend", "frontend"]);
    expect(groups.every((g) => g.children.length === 0)).toBe(true);
    expect(groups.find((g) => g.label === "backend")!.row!.repoSlug).toBe("backend");
  });

  it("mixes a monorepo group with standalone repos in the same feed", () => {
    const rows = [
      row({ repoSlug: "shop-api", phase: "extracting_ast", ts: 3 }),
      row({ repoSlug: "shop-web", phase: "extracting_ast", ts: 3 }),
      row({ repoSlug: "standalone", phase: "done", ts: 3 }),
    ];
    const nesting: RepoNesting = {
      "shop-api": { repoSlug: "shop-api", parentSlug: "shop", parentLabel: "shop", moduleLabel: "packages/api" },
      "shop-web": { repoSlug: "shop-web", parentSlug: "shop", parentLabel: "shop", moduleLabel: "packages/web" },
    };
    const groups = nestRows(rows, nesting);
    const mono = groups.find((g) => g.kind === "monorepo")!;
    const repo = groups.find((g) => g.kind === "repo")!;
    expect(mono.children).toHaveLength(2);
    expect(repo.label).toBe("standalone");
  });

  it("no nesting arg → every row is its own flat repo group (back-compat)", () => {
    const rows = [row({ repoSlug: "solo", phase: "done", ts: 1 })];
    const groups = nestRows(rows);
    expect(groups).toHaveLength(1);
    expect(groups[0].kind).toBe("repo");
    expect(groups[0].label).toBe("solo");
  });

  it("preserves each child's file/entity counts when lifting to Done", () => {
    const rows = [
      row({ repoSlug: "shop-api", phase: "done", ts: 5 }),
      row({ repoSlug: "shop-web", phase: "writing_graph", filesDone: 9, filesTotal: 9, entitiesSoFar: 42, ts: 5 }),
    ];
    const groups = nestRows(rows, monorepoNesting(), true);
    const web = groups[0].children.find((c) => c.repoSlug === "shop-web")!;
    expect(web.phase).toBe("done");
    expect(web.entitiesSoFar).toBe(42);
    expect(web.filesDone).toBe(9);
  });

  it("surfaces group-level enhancing when any child is enhancing", () => {
    const rows = [
      row({ repoSlug: "shop-api", phase: "done", enhancing: true, ts: 5 }),
      row({ repoSlug: "shop-web", phase: "done", ts: 5 }),
    ];
    const groups = nestRows(rows, monorepoNesting());
    expect(groups[0].enhancing).toBe(true);
  });
});
