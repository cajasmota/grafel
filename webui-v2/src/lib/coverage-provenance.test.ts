/**
 * Branch-logic tests for resolveCoverageProvenance (#5038).
 *
 * Verifies the degradation precedence (line ▸ reachability ▸ capability), the
 * "how to enable" affordance gating, the freshness/stale verdict, and that we
 * never claim an authoritative line "%" when a report wasn't ingested.
 *
 * Run with: npx vitest run src/lib/coverage-provenance.test.ts
 */
import { describe, it, expect } from "vitest";
import {
  resolveCoverageProvenance,
  coverageStateFromReport,
  COVERAGE_DEFINITIONS,
  COVERAGE_MCP_TOOL,
  type CoverageSourceState,
} from "./coverage-provenance";

describe("resolveCoverageProvenance — source precedence", () => {
  it("picks ingested LINE coverage when present (authoritative %)", () => {
    const state: CoverageSourceState = {
      line: { source: "lcov", measuredAt: "2026-06-12T10:00:00Z", pct: 73.4 },
      reachabilityAvailable: true, // line still wins over reachability
      reportIngestionConfigured: true,
    };
    const p = resolveCoverageProvenance(state);
    expect(p.kind).toBe("line");
    expect(p.tone).toBe("success");
    expect(p.method).toContain("LCOV");
    expect(p.method).toContain("coverage_source: lcov");
    expect(p.method).toContain("measured 2026-06-12T10:00:00Z");
    // Ingestion is active ⇒ no "how to enable" nag.
    expect(p.howToEnable).toBeNull();
    expect(p.agentMeaning).toContain(COVERAGE_MCP_TOOL);
  });

  it("falls back to static REACHABILITY when no line report but reachability exists", () => {
    const state: CoverageSourceState = { reachabilityAvailable: true };
    const p = resolveCoverageProvenance(state);
    expect(p.kind).toBe("reachability");
    expect(p.tone).toBe("info");
    expect(p.method).toContain("static");
    // Must NOT imply a measured line %.
    expect(p.method.toLowerCase()).toContain("not a measured line");
    expect(p.freshness).toBeNull();
  });

  it("defaults to CAPABILITY coverage when nothing is wired", () => {
    const p = resolveCoverageProvenance({});
    expect(p.kind).toBe("capability");
    expect(p.tone).toBe("neutral");
    expect(p.method).toContain("NOT test execution");
  });

  it("treats null/undefined state as the capability default", () => {
    expect(resolveCoverageProvenance(null).kind).toBe("capability");
    expect(resolveCoverageProvenance(undefined).kind).toBe("capability");
  });

  it("ignores a line block with no source string", () => {
    // A malformed/empty line block must not be treated as authoritative.
    const state = { line: { source: "" }, reachabilityAvailable: true } as CoverageSourceState;
    const p = resolveCoverageProvenance(state);
    expect(p.kind).toBe("reachability");
  });
});

describe("resolveCoverageProvenance — how-to-enable affordance", () => {
  it("shows how-to-enable when reachability is shown and ingestion is NOT configured", () => {
    const p = resolveCoverageProvenance({
      reachabilityAvailable: true,
      reportIngestionConfigured: false,
    });
    expect(p.howToEnable).not.toBeNull();
    expect(p.howToEnable).toContain("lcov/cobertura/jacoco");
  });

  it("hides how-to-enable when ingestion IS configured (just no report yet)", () => {
    const p = resolveCoverageProvenance({
      reachabilityAvailable: true,
      reportIngestionConfigured: true,
    });
    expect(p.howToEnable).toBeNull();
  });

  it("shows how-to-enable on the capability default when ingestion not configured", () => {
    const p = resolveCoverageProvenance({});
    expect(p.howToEnable).not.toBeNull();
  });
});

describe("resolveCoverageProvenance — freshness / staleness", () => {
  it("flags stale when measurement predates latest index", () => {
    const p = resolveCoverageProvenance({
      line: { source: "lcov", measuredAt: "2026-06-01T00:00:00Z" },
      latestIndexAt: "2026-06-12T00:00:00Z",
    });
    expect(p.freshness?.stale).toBe(true);
    expect(p.freshness?.measuredAt).toBe("2026-06-01T00:00:00Z");
  });

  it("is fresh when measurement is at/after latest index", () => {
    const p = resolveCoverageProvenance({
      line: { source: "lcov", measuredAt: "2026-06-12T00:00:00Z" },
      latestIndexAt: "2026-06-01T00:00:00Z",
    });
    expect(p.freshness?.stale).toBe(false);
  });

  it("does not judge staleness without a latest-index timestamp", () => {
    const p = resolveCoverageProvenance({
      line: { source: "lcov", measuredAt: "2026-06-12T00:00:00Z" },
    });
    expect(p.freshness?.stale).toBe(false);
  });

  it("does not judge staleness on malformed timestamps", () => {
    const p = resolveCoverageProvenance({
      line: { source: "lcov", measuredAt: "not-a-date" },
      latestIndexAt: "also-not-a-date",
    });
    expect(p.freshness?.stale).toBe(false);
  });
});

describe("self-documenting definitions", () => {
  it("defines all three coverage concepts", () => {
    const kinds = COVERAGE_DEFINITIONS.map((d) => d.kind);
    expect(kinds).toEqual(["line", "reachability", "capability"]);
    for (const d of COVERAGE_DEFINITIONS) {
      expect(d.title.length).toBeGreaterThan(0);
      expect(d.body.length).toBeGreaterThan(0);
    }
  });
});

describe("coverageStateFromReport — #5066 endpoint wiring", () => {
  it("renders the INGESTED LINE COVERAGE state when the API reports line_coverage", () => {
    // What the /quality/coverage endpoint now returns once a report is ingested.
    const apiLineCoverage = {
      source: "lcov",
      covered_lines: 734,
      total_lines: 1000,
      coverage_pct: 73.4,
      measured_at: "2026-06-12T10:00:00Z",
      entities: 42,
    };
    const state = coverageStateFromReport(apiLineCoverage);
    // The banner upgrades automatically off this state.
    const p = resolveCoverageProvenance(state);
    expect(p.kind).toBe("line");
    expect(p.label).toBe("Line coverage");
    expect(p.method).toContain("ingested from LCOV");
    expect(p.method).toContain("measured 2026-06-12T10:00:00Z");
    expect(p.freshness?.measuredAt).toBe("2026-06-12T10:00:00Z");
    // Ingestion is active ⇒ no "how to enable" nag.
    expect(p.howToEnable).toBeNull();
    // And we carry the real line numbers through for the % display.
    expect(state.line?.pct).toBe(73.4);
    expect(state.line?.coveredLines).toBe(734);
    expect(state.reportIngestionConfigured).toBe(true);
  });

  it("flags the line measurement STALE when it predates the latest index", () => {
    const state = coverageStateFromReport(
      {
        source: "lcov",
        covered_lines: 1,
        total_lines: 2,
        coverage_pct: 50,
        measured_at: "2026-06-10T00:00:00Z",
        entities: 1,
      },
      "2026-06-12T00:00:00Z", // index newer than measurement
    );
    const p = resolveCoverageProvenance(state);
    expect(p.kind).toBe("line");
    expect(p.freshness?.stale).toBe(true);
  });

  it("DEGRADES to reachability when no line_coverage is present", () => {
    const state = coverageStateFromReport(undefined);
    expect(state.line).toBeUndefined();
    expect(state.reachabilityAvailable).toBe(true);
    const p = resolveCoverageProvenance(state);
    expect(p.kind).toBe("reachability");
    // Not yet configured ⇒ surfaces the how-to-enable affordance.
    expect(p.howToEnable).not.toBeNull();
  });
});
