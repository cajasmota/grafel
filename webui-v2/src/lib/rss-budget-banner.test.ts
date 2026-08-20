import { describe, it, expect } from "vitest";
import { rssBudgetPresentation } from "./rss-budget-banner";

describe("rssBudgetPresentation", () => {
  it("never claims 'over budget' when live whole-process RSS exceeds a next-start, delta-scoped budget (#6324)", () => {
    const p = rssBudgetPresentation({
      rss_mb: 987,
      rss_budget_mb: 500,
      rss_budget_scope: "next_start",
    });
    expect(p.comparable).toBe(false);
    expect(p.tone).not.toBe("warning");
    expect(p.tone).not.toBe("danger");
    expect(p.badgeLabel ?? "").not.toMatch(/over budget/i);
    expect(p.tooltip ?? "").not.toMatch(/over (memory )?budget/i);
  });

  it("does not render RSS as a fraction of a non-comparable budget", () => {
    const p = rssBudgetPresentation({
      rss_mb: 987,
      rss_budget_mb: 500,
      rss_budget_scope: "next_start",
    });
    expect(p.value).toBe("987 MB");
    expect(p.value).not.toContain("/");
  });

  it("qualifies the budget with its scope, and says what each number is", () => {
    const p = rssBudgetPresentation({
      rss_mb: 987,
      rss_budget_mb: 500,
      rss_budget_scope: "next_start",
    });
    expect(p.tone).toBe("info");
    expect(p.badgeLabel).toBe("Next-start budget");
    expect(p.tooltip).toContain("500 MB");
    expect(p.tooltip).toContain("987 MB");
    expect(p.tooltip).toMatch(/next start/i);
    expect(p.tooltip).toMatch(/additional/i);
  });

  it("treats an absent scope as non-comparable too (pre-#6323 daemons budget the same delta)", () => {
    const p = rssBudgetPresentation({ rss_mb: 987, rss_budget_mb: 500 });
    expect(p.comparable).toBe(false);
    expect(p.tone).toBe("info");
    expect(p.value).toBe("987 MB");
  });

  it("stays silent when no budget is configured", () => {
    expect(rssBudgetPresentation({ rss_mb: 412 })).toEqual({
      value: "412 MB",
      badgeLabel: null,
      tone: null,
      tooltip: undefined,
      comparable: false,
    });
    const zero = rssBudgetPresentation({ rss_mb: 412, rss_budget_mb: 0 });
    expect(zero.tone).toBe(null);
    expect(zero.value).toBe("412 MB");
  });

  it("does compare, and warns, once a daemon labels the budget live whole-process", () => {
    const p = rssBudgetPresentation({
      rss_mb: 600,
      rss_budget_mb: 500,
      rss_budget_scope: "live_process",
    });
    expect(p.comparable).toBe(true);
    expect(p.tone).toBe("warning");
    expect(p.badgeLabel).toBe("Over budget");
    expect(p.value).toBe("600 / 500 MB");
  });

  it("escalates to danger at 1.5x on a comparable budget, and stays quiet within it", () => {
    const hot = rssBudgetPresentation({
      rss_mb: 750,
      rss_budget_mb: 500,
      rss_budget_scope: "live_process",
    });
    expect(hot.tone).toBe("danger");

    const ok = rssBudgetPresentation({
      rss_mb: 300,
      rss_budget_mb: 500,
      rss_budget_scope: "live_process",
    });
    expect(ok.tone).toBe(null);
    expect(ok.badgeLabel).toBe(null);
    expect(ok.tooltip).toContain("Within budget");
  });
});
