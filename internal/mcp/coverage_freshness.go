// coverage_freshness.go — shared coverage-freshness signal for the coverage MCP
// tools (#5068).
//
// grafel can ingest a real line-coverage report (LCOV / Cobertura / JaCoCo,
// #5036) and stamp `coverage_source` / `coverage_measured_at` onto entities at
// index time (#5061). That ingested measurement can go STALE: if the source is
// re-indexed after the coverage report was produced, the report predates the
// graph it is annotating and "% covered" may no longer reflect current code.
//
// The dashboard provenance banner (#5038) computes this staleness verdict
// client-side (webui-v2/src/lib/coverage-provenance.ts): stale when
// measured_at < latest index time. This file mirrors that exact rule so an
// AGENT querying via the coverage MCP tools gets the same freshness/stale
// verdict directly, without re-deriving it from raw props.
//
// The signal degrades honestly:
//   - no entity carries coverage_source         → "no coverage report ingested"
//   - report ingested but no measured_at stamp  → freshness unknown (no verdict)
//   - measured_at present, no index timestamp   → echo measured_at, no verdict
//   - both present                              → FRESH or STALE + the delta
package mcp

import (
	"fmt"
	"sort"
	"time"

	"github.com/cajasmota/grafel/internal/coverage"
)

// coverageFreshness is the freshness verdict for a group's ingested
// line-coverage measurement, mirroring the dashboard provenance banner (#5038).
type coverageFreshness struct {
	// ingested is true when ANY entity in the group carried a coverage_source
	// prop (a report was actually ingested). When false, all other fields are
	// zero and the only honest statement is "no coverage report ingested".
	ingested bool
	// source is the first non-empty coverage_source seen (e.g. "lcov").
	source string
	// entities counts how many entities carried coverage_source — distinguishes
	// a real (if sparse) ingestion from none at all.
	entities int
	// measuredAt is the latest coverage_measured_at seen (RFC3339). Empty when
	// the ingestor stamped no timestamp.
	measuredAt string
	// indexedAt is the latest index time across the group's repos (the document
	// GeneratedAt). Zero when no repo carried a timestamp.
	indexedAt time.Time
	// haveVerdict is true only when both measuredAt parsed and indexedAt is set,
	// so a stale/fresh judgement could actually be made.
	haveVerdict bool
	// stale is the verdict (only meaningful when haveVerdict): the coverage
	// measurement predates the latest index → may no longer reflect the code.
	stale bool
	// delta is indexedAt - measuredAt (only meaningful when haveVerdict). When
	// stale it is the positive amount the index is newer than the measurement.
	delta time.Duration
}

// computeCoverageFreshness folds the loaded group's documents into a single
// freshness verdict. Pure over the loaded graph (no IO): scans entity props for
// coverage_source / coverage_measured_at and the per-repo document GeneratedAt.
//
// Repos are visited in sorted order so the "first source seen" is deterministic.
func computeCoverageFreshness(lg *LoadedGroup) coverageFreshness {
	var f coverageFreshness
	if lg == nil {
		return f
	}

	repoNames := make([]string, 0, len(lg.Repos))
	for name := range lg.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	for _, name := range repoNames {
		lr := lg.Repos[name]
		if lr == nil || lr.Doc == nil {
			continue
		}
		// Latest index time across repos drives the staleness comparison.
		if g := lr.Doc.GeneratedAt; g.After(f.indexedAt) {
			f.indexedAt = g
		}
		for i := range lr.Doc.Entities {
			e := &lr.Doc.Entities[i]
			if e.PropLen() == 0 {
				continue
			}
			src := e.PropGet(coverage.PropCoverageSource)
			if src == "" {
				continue
			}
			f.ingested = true
			f.entities++
			if f.source == "" {
				f.source = src
			}
			// RFC3339 timestamps sort lexicographically, so a string compare
			// picks the most recent measurement without parsing.
			if at := e.PropGet(coverage.PropCoverageMeasAt); at > f.measuredAt {
				f.measuredAt = at
			}
		}
	}

	// Verdict only when both sides of the comparison exist.
	if f.measuredAt != "" && !f.indexedAt.IsZero() {
		if mt, err := time.Parse(time.RFC3339, f.measuredAt); err == nil {
			f.haveVerdict = true
			f.stale = mt.Before(f.indexedAt)
			f.delta = f.indexedAt.Sub(mt)
		}
	}
	return f
}

// renderCoverageFreshness renders the freshness block as a markdown section for
// the coverage MCP tools. Always returns a non-empty, honest section: it states
// "no report ingested" when nothing is stamped, and degrades when timestamps
// are missing rather than asserting a verdict it cannot support.
func renderCoverageFreshness(f coverageFreshness) string {
	out := "\n### Coverage freshness\n\n"

	if !f.ingested {
		out += "_No coverage report ingested for this group_ — the counts above are " +
			"static reach-coverage (graph-derived TESTS edges), not a measured line %. " +
			"To ingest real line coverage, point grafel at your lcov/cobertura/jacoco " +
			"report (coverage.report_paths) and re-index.\n"
		return out
	}

	out += fmt.Sprintf("coverage_source   : %s (%d entities stamped)\n", f.source, f.entities)
	if f.measuredAt != "" {
		out += fmt.Sprintf("measured_at       : %s\n", f.measuredAt)
	} else {
		out += "measured_at       : (not stamped by ingestor)\n"
	}
	if !f.indexedAt.IsZero() {
		out += fmt.Sprintf("indexed_at        : %s\n", f.indexedAt.UTC().Format(time.RFC3339))
	}

	switch {
	case f.haveVerdict && f.stale:
		out += fmt.Sprintf(
			"verdict           : STALE — the coverage report predates the latest index by %s.\n"+
				"                    Coverage may be stale; re-run tests + reingest the report, then re-index.\n",
			humanizeDelta(f.delta),
		)
	case f.haveVerdict && !f.stale:
		out += fmt.Sprintf(
			"verdict           : FRESH — the coverage report is at or newer than the latest index (by %s).\n",
			humanizeDelta(f.delta.Abs()),
		)
	case f.measuredAt == "":
		out += "verdict           : UNKNOWN — ingestor stamped no measured_at, so staleness can't be judged.\n"
	default:
		out += "verdict           : UNKNOWN — no index timestamp available to compare against.\n"
	}
	return out
}

// humanizeDelta renders a duration compactly for the freshness line.
func humanizeDelta(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	default:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
}
