/**
 * coverage-provenance — pure provenance/branch logic for the dashboard
 * coverage-provenance banner (#5038).
 *
 * archigraph surfaces THREE distinct things all colloquially called
 * "coverage", and a bare "%" reads as authoritative when it may be any of
 * them, stale, or absent. This module turns the raw availability state for a
 * given view into a single, unambiguous provenance descriptor that the
 * {@link CoverageProvenanceBanner} renders. It is intentionally DOM-free and
 * side-effect-free so the branch selection is unit-testable under the default
 * (node) vitest environment — the component is a thin renderer over the
 * descriptor this returns.
 *
 * The three concepts (kept verbatim in {@link COVERAGE_DEFINITIONS} for the
 * self-documenting tooltip):
 *
 *   1. CAPABILITY coverage (registry.json) — what archigraph's EXTRACTOR
 *      supports per language/framework. NOT test execution.
 *   2. LINE coverage (dynamic, #5036) — real %, present ONLY if an
 *      LCOV/Cobertura/JaCoCo report was ingested. Stamps entity props
 *      `coverage_pct` / `covered_lines` / `total_lines` / `coverage_source` /
 *      `coverage_measured_at`.
 *   3. REACHABILITY (static, #5037) — graph-derived `test_reachable` /
 *      reaching tests, no execution.
 *
 * Degradation rule (v1): live wiring of ingested line-coverage into the
 * dashboard data layer is still pending (#5036 landed the parser/attribution,
 * not the dashboard hook), so most callers will only have reachability or
 * nothing. We pick the BEST available source and NEVER present a misleading
 * authoritative "%". When nothing is wired, we fall back to the
 * capability-coverage explanation as the safe default.
 */

/** Which of the three coverage concepts a rendered number actually is. */
export type CoverageProvenanceKind = "line" | "reachability" | "capability";

/** Coverage-report format archigraph can ingest (mirrors sonar.*.reportPaths). */
export type CoverageReportFormat = "lcov" | "cobertura" | "jacoco";

/**
 * Raw availability state for the CURRENT view, assembled by the caller from
 * whatever the data layer exposes. Every field is optional precisely because
 * v1 lives in a half-wired world — the logic below copes with any subset.
 */
export interface CoverageSourceState {
  /**
   * Line coverage ingested from a report for this group/view (#5036). Present
   * only when the dashboard data layer actually carries the stamped
   * `coverage_source` props. Absent (the common v1 case) ⇒ no report ingested.
   */
  line?: {
    /** `coverage_source` prop, e.g. "lcov". */
    source: string;
    /** `coverage_measured_at` — ISO-8601 string, if the backend stamped it. */
    measuredAt?: string;
    /** Optional already-computed percentage for display context. */
    pct?: number;
    coveredLines?: number;
    totalLines?: number;
  };
  /**
   * Static test-reachability is available for this view (#5037) — graph-derived
   * tested/untested with no execution. This is what `coverage_pct` on the
   * existing /quality/coverage endpoint actually is today.
   */
  reachabilityAvailable?: boolean;
  /**
   * Whether report ingestion is CONFIGURED/available for this group (a
   * coverage `format` + `report_paths` is set). Drives the "how to enable"
   * affordance: if ingestion is not configured we tell the user how to turn
   * it on.
   */
  reportIngestionConfigured?: boolean;
  /**
   * Latest index timestamp (ISO-8601) for the group, used only to flag a line
   * coverage measurement as STALE when it predates the most recent index.
   * Omitted ⇒ no staleness judgement is made (rendered without a stale badge).
   */
  latestIndexAt?: string;
}

/** The freshness verdict for an ingested line-coverage measurement. */
export interface CoverageFreshness {
  /** ISO-8601 measurement time, echoed for display. */
  measuredAt?: string;
  /** True when the measurement predates the latest index (so it may be stale). */
  stale: boolean;
}

/**
 * Fully-resolved, render-ready provenance descriptor. The banner component is
 * a pure function of this — no further branching lives in the component.
 */
export interface CoverageProvenance {
  kind: CoverageProvenanceKind;
  /** Visual tone the banner should use (maps to existing Badge tones). */
  tone: "success" | "info" | "neutral";
  /** Short headline, e.g. "Line coverage" / "Static test-reachability". */
  label: string;
  /** One-line statement of source + method. The load-bearing message. */
  method: string;
  /**
   * "How to enable" line — present ONLY when ingestion is not configured and
   * could meaningfully upgrade what's shown (i.e. we're degraded below line
   * coverage). Null when ingestion is already configured/active.
   */
  howToEnable: string | null;
  /** What this number means for AI agents querying via MCP. */
  agentMeaning: string;
  /** Freshness verdict — present only for ingested line coverage. */
  freshness: CoverageFreshness | null;
}

/** The MCP tool agents use to query coverage provenance. */
export const COVERAGE_MCP_TOOL = "archigraph_coverage";

/**
 * Self-documenting definitions of all three coverage concepts — rendered
 * verbatim in the banner's expandable/tooltip so the surface explains itself.
 */
export const COVERAGE_DEFINITIONS: ReadonlyArray<{
  kind: CoverageProvenanceKind;
  title: string;
  body: string;
}> = [
  {
    kind: "line",
    title: "Line coverage (dynamic)",
    body:
      "A real, executed percentage — % of source lines a test suite actually ran. " +
      "Present only when a coverage report (LCOV / Cobertura / JaCoCo) was ingested. " +
      "archigraph reads the report (SonarQube model); it never runs your tests.",
  },
  {
    kind: "reachability",
    title: "Static test-reachability",
    body:
      "Graph-derived: % of production entities a test can structurally reach " +
      "(a test CALLS the handler), with no execution. Available without any " +
      "coverage report. Tells you what is wired to a test, not what a test ran.",
  },
  {
    kind: "capability",
    title: "Capability coverage",
    body:
      "What archigraph's EXTRACTOR supports for a language/framework " +
      "(from registry.json). This is NOT test coverage and NOT test execution — " +
      "it describes how completely archigraph can model your code.",
  },
];

const AGENT_MEANING: Record<CoverageProvenanceKind, string> = {
  line:
    `Agents query this via ${COVERAGE_MCP_TOOL}; entities carry ingested ` +
    "covered_lines/total_lines, so an agent can target the least-covered code first.",
  reachability:
    `Agents query this via ${COVERAGE_MCP_TOOL}; uncovered endpoints (no TESTS edge) ` +
    "are flagged in parity checks so an agent knows what to test next.",
  capability:
    `Agents read this from the capability registry, not ${COVERAGE_MCP_TOOL}; it tells ` +
    "them how much of the code archigraph can model, not what tests cover.",
};

const HOW_TO_ENABLE =
  "Enable real line coverage: point archigraph at your lcov/cobertura/jacoco " +
  "report path (coverage.report_paths, mirroring sonar.*.reportPaths), then re-index.";

/** Format the measured-at line, defensively (the input may be malformed). */
function parseTime(iso: string | undefined): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : t;
}

/**
 * Resolve the raw availability state into the single provenance descriptor the
 * banner renders. Pure; the whole degradation policy lives here.
 *
 * Precedence (best available wins, never a misleading authoritative %):
 *   1. Ingested LINE coverage if present (real, dynamic).
 *   2. Else static REACHABILITY if available (graph-derived).
 *   3. Else CAPABILITY coverage (the safe default when nothing is wired).
 */
export function resolveCoverageProvenance(
  state: CoverageSourceState | null | undefined,
): CoverageProvenance {
  const s = state ?? {};

  // 1. Real ingested line coverage — the only authoritative "%".
  if (s.line && s.line.source) {
    const measuredAt = s.line.measuredAt;
    const measuredT = parseTime(measuredAt);
    const indexT = parseTime(s.latestIndexAt);
    const stale = measuredT != null && indexT != null ? measuredT < indexT : false;

    const sourceUpper = s.line.source.toUpperCase();
    const measuredSuffix = measuredAt ? `, measured ${measuredAt}` : "";
    return {
      kind: "line",
      tone: "success",
      label: "Line coverage",
      method: `Line coverage — ingested from ${sourceUpper} (coverage_source: ${s.line.source})${measuredSuffix}.`,
      // Ingestion is clearly active here, so no "how to enable".
      howToEnable: null,
      agentMeaning: AGENT_MEANING.line,
      freshness: { measuredAt, stale },
    };
  }

  // 2. Static reachability fallback — graph-derived, no execution.
  if (s.reachabilityAvailable) {
    return {
      kind: "reachability",
      tone: "info",
      label: "Static test-reachability",
      method:
        "No coverage report ingested for this group — showing static " +
        "test-reachability (graph-derived: a test structurally reaches the code, " +
        "not a measured line %).",
      // Only offer "how to enable" when ingestion isn't already configured.
      howToEnable: s.reportIngestionConfigured ? null : HOW_TO_ENABLE,
      agentMeaning: AGENT_MEANING.reachability,
      freshness: null,
    };
  }

  // 3. Safe default — capability coverage. Nothing test-related is wired.
  return {
    kind: "capability",
    tone: "neutral",
    label: "Capability coverage",
    method:
      "Capability coverage — what archigraph's extractor supports for this " +
      "language/framework. NOT test execution and NOT test reachability.",
    howToEnable: s.reportIngestionConfigured ? null : HOW_TO_ENABLE,
    agentMeaning: AGENT_MEANING.capability,
    freshness: null,
  };
}

/**
 * Group-level ingested line-coverage roll-up, as surfaced by the dashboard
 * `/quality/coverage` endpoint's optional `line_coverage` field (#5066). Mirror
 * of the Go `LineCoverageSummary` wire shape. Kept here (rather than importing
 * from data/types) so this module stays dependency-free and node-testable.
 */
export interface LineCoverageReport {
  source: string;
  covered_lines: number;
  total_lines: number;
  coverage_pct: number;
  measured_at?: string;
  entities: number;
}

/**
 * Build the banner's {@link CoverageSourceState} from what the coverage
 * endpoint returns (#5066 wiring). When `line_coverage` is present a report was
 * ingested, so we populate the authoritative `line` state and mark ingestion
 * configured; otherwise we degrade to static reachability and the banner shows
 * the "how to enable" affordance. Pure so the wiring is unit-testable without a
 * DOM (the CoverageTab call site is a thin pass-through over this).
 *
 * @param lineCoverage  the endpoint's `line_coverage` field (or undefined)
 * @param latestIndexAt optional ISO index time for the staleness check
 */
export function coverageStateFromReport(
  lineCoverage: LineCoverageReport | null | undefined,
  latestIndexAt?: string,
): CoverageSourceState {
  if (lineCoverage && lineCoverage.source) {
    return {
      line: {
        source: lineCoverage.source,
        measuredAt: lineCoverage.measured_at,
        pct: lineCoverage.coverage_pct,
        coveredLines: lineCoverage.covered_lines,
        totalLines: lineCoverage.total_lines,
      },
      // line_coverage only exists because a report was ingested.
      reportIngestionConfigured: true,
      reachabilityAvailable: true,
      latestIndexAt,
    };
  }
  return { reachabilityAvailable: true };
}
