/**
 * reachability-summary — pure branch logic for the dashboard's static
 * test-reachability surface (#5037/#5062).
 *
 * #5037 computes, WITHOUT executing anything, which HTTP endpoints are
 * reachable from at least one test (graph traversal over TESTS + CALLS edges).
 * The pass stamps `test_reachable` onto endpoint entities at index time (#5061);
 * the dashboard coverage endpoint rolls those into a `ReachabilitySummary`. The
 * highest-value display is the ORPHAN surface — endpoints with NO test path
 * reaching their handler.
 *
 * This module turns the raw summary into a single view-state descriptor with
 * exactly three branches, so the renderer is a thin switch and the branch
 * selection is unit-testable under the default (node) vitest environment — the
 * same convention as {@link ./coverage-provenance}.
 *
 *   - "not-computed": the reachability pass never ran for this group (pre-#5061
 *     index, or the summary is absent). We MUST NOT imply everything is
 *     untested — show a "reindex to compute" notice instead.
 *   - "all-tested": every endpoint has >=1 test reaching it (zero orphans).
 *   - "has-orphans": one or more untested endpoints — highlight them.
 */

import type { ReachabilitySummary, ReachOrphanEndpoint } from "../data/types";

/** Which branch the reachability surface should render. */
export type ReachabilityViewKind = "not-computed" | "all-tested" | "has-orphans";

/** Resolved, render-ready descriptor for the reachability surface. */
export interface ReachabilityView {
  kind: ReachabilityViewKind;
  /** Endpoint-level roll-up counts (all 0 when not computed). */
  total: number;
  tested: number;
  orphans: number;
  /** 100 * tested / total, rounded for display; 0 when not computed. */
  reachablePct: number;
  /** Capped orphan rows to render (empty unless kind === "has-orphans"). */
  orphanRows: ReachOrphanEndpoint[];
  /** Orphan rows elided beyond the backend cap (0 when none). */
  orphansMore: number;
  /** One-line human summary suitable for a badge/heading. */
  label: string;
}

/**
 * resolveReachabilityView turns the (possibly absent / not-computed) summary
 * into a single descriptor. Tolerant of an absent summary (older backend) — it
 * collapses to the "not-computed" branch, never implying untested.
 */
export function resolveReachabilityView(
  summary: ReachabilitySummary | undefined,
): ReachabilityView {
  // Absent, or the pass never ran for this group ⇒ degrade. Critically, we do
  // NOT treat "no data" as "everything untested".
  if (!summary || !summary.computed) {
    return {
      kind: "not-computed",
      total: 0,
      tested: 0,
      orphans: 0,
      reachablePct: 0,
      orphanRows: [],
      orphansMore: 0,
      label: "Test-reachability not computed — reindex to compute",
    };
  }

  const total = summary.total_endpoints;
  const tested = summary.tested_endpoints;
  const orphans = summary.orphan_endpoints;
  const reachablePct = summary.reachable_pct;

  if (orphans <= 0) {
    return {
      kind: "all-tested",
      total,
      tested,
      orphans: 0,
      reachablePct,
      orphanRows: [],
      orphansMore: 0,
      label: `All ${total} endpoint${total === 1 ? "" : "s"} reachable from a test`,
    };
  }

  return {
    kind: "has-orphans",
    total,
    tested,
    orphans,
    reachablePct,
    orphanRows: summary.orphans ?? [],
    orphansMore: summary.orphans_more ?? 0,
    label: `${orphans} untested endpoint${orphans === 1 ? "" : "s"} (no test reaches handler)`,
  };
}
