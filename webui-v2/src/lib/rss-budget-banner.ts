/**
 * Memory-budget presentation for the daemon status card.
 *
 * Why this is not a plain `rss_mb > rss_budget_mb` comparison (#6324):
 *
 *   - `rss_mb` is the daemon's **live, whole-process** resident set size.
 *   - `rss_budget_mb` is **delta-accounted and next-start-scoped**: per
 *     `cmd/grafel/daemon.go` it caps "the ADDITIONAL predicted RSS of
 *     concurrently running index jobs only — the daemon's idle RSS is never
 *     subtracted from it", and it is the value the *next* daemon start will
 *     use, not the running scheduler's live admission counter.
 *
 * Two independent mismatches, so the two numbers are not comparable, and the
 * old card asserted "Over budget" on a correctly configured daemon. The
 * backend itself says so: `rss_budget_scope` (#6323) is sent as `"next_start"`.
 *
 * The chosen framing is **informational and scope-qualified**: never claim the
 * daemon is over budget from numbers that cannot be compared, don't render RSS
 * as a fraction of the budget (`987 / 500 MB` is itself the false claim), and
 * name what each number actually is. A real over-budget warning is still
 * produced — but only for a daemon that labels its budget as live and
 * whole-process, which no daemon does today.
 */

export type RssBudgetTone = "info" | "warning" | "danger";

export interface RssBudgetStatusInput {
  /** Live whole-process RSS of the daemon, in MB. */
  rss_mb: number;
  /** Configured budget in MB, if any. See rss_budget_scope for what it covers. */
  rss_budget_mb?: number;
  /** Backend's own qualification of rss_budget_mb. Today always "next_start". */
  rss_budget_scope?: string;
}

export interface RssBudgetPresentation {
  /** Text for the Memory stat value. */
  value: string;
  /** Badge label, or null when no badge should render. */
  badgeLabel: string | null;
  /** Tone for the badge and value colouring, or null for no emphasis. */
  tone: RssBudgetTone | null;
  /** Explanatory tooltip, or undefined when there is nothing to explain. */
  tooltip?: string;
  /** True only when rss_mb and rss_budget_mb measure the same thing. */
  comparable: boolean;
}

/**
 * Scopes for which the budget is a live, whole-process ceiling and may
 * therefore be compared against `rss_mb` directly. Deliberately an allow-list:
 * an absent scope is *not* comparable either, because daemons predating #6323
 * accounted the budget the same delta-scoped way — they just didn't say so.
 */
const COMPARABLE_SCOPES = new Set(["live_process"]);

const mb = (n: number) => `${n.toFixed(0)} MB`;

export function rssBudgetPresentation(status: RssBudgetStatusInput): RssBudgetPresentation {
  const budget = status.rss_budget_mb;
  const hasBudget = budget != null && budget > 0;

  if (!hasBudget) {
    return {
      value: mb(status.rss_mb),
      badgeLabel: null,
      tone: null,
      tooltip: undefined,
      comparable: false,
    };
  }

  const comparable = COMPARABLE_SCOPES.has(status.rss_budget_scope ?? "");

  if (!comparable) {
    // Informational only: state both numbers and what separates them.
    return {
      value: mb(status.rss_mb),
      badgeLabel: "Next-start budget",
      tone: "info",
      tooltip:
        `Daemon is using ${mb(status.rss_mb)} of memory in total. ` +
        `The configured ${mb(budget)} budget is not a cap on that number: it limits the ` +
        `additional memory concurrent index jobs may claim, and it applies at the daemon's ` +
        `next start. The two are not directly comparable.`,
      comparable: false,
    };
  }

  const ratio = status.rss_mb / budget;
  const over = status.rss_mb > budget;

  if (!over) {
    return {
      value: `${status.rss_mb.toFixed(0)} / ${budget.toFixed(0)} MB`,
      badgeLabel: null,
      tone: null,
      tooltip: `Within budget — ${mb(status.rss_mb)} of ${mb(budget)}.`,
      comparable: true,
    };
  }

  return {
    value: `${status.rss_mb.toFixed(0)} / ${budget.toFixed(0)} MB`,
    badgeLabel: "Over budget",
    tone: ratio >= 1.5 ? "danger" : "warning",
    tooltip:
      `Over memory budget — using ${mb(status.rss_mb)} against a ${mb(budget)} budget ` +
      `(${Math.round(ratio * 100)}%). Consider restarting the daemon or indexing fewer repositories.`,
    comparable: true,
  };
}
