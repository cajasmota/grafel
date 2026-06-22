import { describe, it, expect } from "vitest";

import {
  aggregateProgress,
  fold,
  overallPhaseLabel,
  rowFraction,
  rowKey,
  rowsTerminal,
  sortRows,
} from "./index-progress-fold";
import type { ProgressEvent, ProgressRow } from "@/data/types";

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

describe("rowFraction — phase-weighted per-repo completion (#5332)", () => {
  it("done / error count as 100%", () => {
    expect(rowFraction(row({ phase: "done" }))).toBe(1);
    expect(rowFraction(row({ phase: "error" }))).toBe(1);
  });

  it("scanning is the bottom band (0%)", () => {
    expect(rowFraction(row({ phase: "scanning" }))).toBe(0);
  });

  it("extracting_ast adds the files slice within its band", () => {
    // base = 1/5 = 0.2; +50% of the 0.2 band = 0.3
    expect(rowFraction(row({ phase: "extracting_ast", filesDone: 50, filesTotal: 100 }))).toBeCloseTo(0.3);
  });

  it("sub-progress-less phases advance only via their band", () => {
    expect(rowFraction(row({ phase: "resolving_refs" }))).toBeCloseTo(0.4);
    expect(rowFraction(row({ phase: "running_algorithms" }))).toBeCloseTo(0.6);
    expect(rowFraction(row({ phase: "materializing" }))).toBeCloseTo(0.8);
  });
});

describe("aggregateProgress (#5332)", () => {
  it("single repo extracting at 50% files → sensible mid value", () => {
    const p = aggregateProgress([row({ phase: "extracting_ast", filesDone: 50, filesTotal: 100 })]);
    expect(p).toBe(30); // 0.3 * 100
  });

  it("one repo done + one extracting → between the two", () => {
    const p = aggregateProgress([
      row({ repoSlug: "a", phase: "done" }),
      row({ repoSlug: "b", phase: "extracting_ast", filesDone: 0, filesTotal: 100 }),
    ]);
    // (1 + 0.2) / 2 = 0.6
    expect(p).toBe(60);
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
    expect(p).toBe(80);
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
      "running_algorithms",
      "materializing",
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
