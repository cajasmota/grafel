import { describe, it, expect } from "vitest";

import { fold, rowKey, rowsTerminal, sortRows } from "./index-progress-fold";
import type { ProgressEvent, ProgressRow } from "@/data/types";

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
    expect(rowsTerminal(rows)).toBe(false);
  });

  it("is true once all repo rows reach done/error", () => {
    const rows = applyAll([
      ev({ repo_slug: "backend", phase: "done", ts: 1 }),
      ev({ repo_slug: "frontend", phase: "error", error: "boom", ts: 1 }),
    ]);
    expect(rowsTerminal(rows)).toBe(true);
  });

  it("is false for an empty feed (nothing to be terminal about)", () => {
    expect(rowsTerminal([])).toBe(false);
  });
});
