package coverage

// enrich.go — the live indexer wiring (#5061) for the two pure coverage passes:
//
//   - LCOV line-coverage attribution (#5036): parse the configured report and
//     stamp coverage_pct / covered_lines / total_lines / coverage_source onto
//     entities by source-span overlap.
//   - static test-reachability (#5037): BFS over TESTS+CALLS edges and stamp
//     test_reachable / reaching_tests / reach_depth onto production entities.
//
// Both passes are deliberately pure (see attribute.go / reachability.go); this
// file is the SEAM the indexer hooks: it adapts a graph.Document into the flat
// []types.EntityRecord the pure passes consume, runs them behind the per-group
// Config, and writes the resulting Properties back onto the Document's entities.
//
// Persistence: the stamped values land in EntityRecord/Entity.Properties (a
// string map), which BOTH the graph.fb and graph.json serializers already
// round-trip — no schema change to either serializer is required (the
// prefer-Properties-over-Kinds / prefer-Properties-over-fb-field rule).
//
// It is OPT-IN and a strict no-op when no report is configured/discovered, so
// groups without coverage are never affected.

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// Stats summarizes one Enrich run for the indexer's verbose log line.
type Stats struct {
	// Skipped is true when the pass did no work (no report configured/found).
	Skipped    bool
	SkipReason string

	// ReportPath is the report that was parsed ("" when none / reachability-only).
	ReportPath string
	// ReportFiles is the number of file blocks in the parsed coverage report
	// (LCOV/Cobertura/JaCoCo).
	ReportFiles int
	// LCOVAttributed is the number of entities stamped with line coverage from
	// the parsed report (the field name is historical; it covers any format).
	LCOVAttributed int

	// Reachability stats (always run when the pass is enabled).
	ReachabilityProduction int // production entities considered
	ReachabilityReachable  int // of which test-reachable
}

// DefaultReportPath is the discovery fallback when a group has not configured an
// explicit report_paths glob: the conventional repo-root LCOV artifact.
const DefaultReportPath = "coverage/lcov.info"

// Enrich runs the coverage enrichment pass against an assembled graph.Document.
//
//   - repoRoot is the absolute path of the indexed repo (used to resolve the
//     report glob and as the LCOV path root when cfg.RootPrefix is empty).
//   - cfg is the per-group coverage config (see Config). When cfg is the zero
//     value, Enrich falls back to discovering DefaultReportPath; if that file
//     does not exist, the LCOV attribution sub-pass is a no-op (reachability
//     still runs, since it needs no report).
//
// The Document's Entity.Properties are mutated in place with the coverage and
// reachability keys. Returns Stats for logging. Never errors the index: a
// malformed/absent report degrades to "reachability-only" rather than failing.
func Enrich(doc *graph.Document, repoRoot string, cfg Config) Stats {
	if doc == nil {
		return Stats{Skipped: true, SkipReason: "nil document"}
	}

	var st Stats

	// 1. Adapt the Document into the flat record view the pure passes consume.
	//    The view carries each entity's ID, span, kind, subtype, tags,
	//    properties, plus the TESTS/CALLS/HANDLES relationships attached to its
	//    host (keyed by FromID) so the reachability BFS sees the edges.
	records := documentRecords(doc)

	// 2. Static test-reachability (#5037) — always runs when enabled; needs no
	//    external report, only the in-graph TESTS+CALLS edges.
	reach := ComputeReachability(records)
	for _, r := range reach {
		st.ReachabilityProduction++
		if r.Reachable {
			st.ReachabilityReachable++
		}
	}
	reachByID := make(map[string]map[string]string, len(reach))
	for _, r := range reach {
		reachByID[r.EntityID] = r.Properties()
	}

	// 3. LCOV line-coverage attribution (#5036) — opt-in, resolved from cfg or
	//    the default-discovery path. A missing report is a clean no-op.
	attrByID := map[string]map[string]string{}
	if reportPath, ok := resolveReport(repoRoot, cfg); ok {
		st.ReportPath = reportPath
		rootPrefix := cfg.RootPrefix
		if rep, err := parseReportFile(reportPath, cfg.Format); err == nil && rep != nil {
			st.ReportFiles = len(rep.Files)
			attrs := Attribute(records, rep, rootPrefix)
			for _, a := range attrs {
				attrByID[a.EntityID] = a.Properties("")
			}
			st.LCOVAttributed = len(attrs)
		} else if err != nil {
			st.SkipReason = "lcov parse failed: " + err.Error()
		}
	}

	if len(reachByID) == 0 && len(attrByID) == 0 {
		st.Skipped = true
		if st.SkipReason == "" {
			st.SkipReason = "no coverage signal (no report, no test edges)"
		}
		return st
	}

	// 4. Write the stamped properties back onto the Document's entities by ID.
	for i := range doc.Entities {
		id := doc.Entities[i].ID
		props := mergedProps(reachByID[id], attrByID[id])
		if len(props) == 0 {
			continue
		}
		if doc.Entities[i].PropLen() == 0 {
			doc.Entities[i].PropsReplace(map[string]string{})
		}
		for k, v := range props {
			doc.Entities[i].PropSet(k, v)
		}
	}

	return st
}

// mergedProps unions the reachability and attribution property maps for one
// entity. Either may be nil. Returns nil when both are empty.
func mergedProps(reach, attr map[string]string) map[string]string {
	if len(reach) == 0 && len(attr) == 0 {
		return nil
	}
	out := make(map[string]string, len(reach)+len(attr))
	for k, v := range reach {
		out[k] = v
	}
	for k, v := range attr {
		out[k] = v
	}
	return out
}

// documentRecords projects a graph.Document into the []types.EntityRecord view
// the pure passes consume. Each entity carries the identity/span/kind fields the
// passes read, plus the subset of relationships (host → target) needed for the
// reachability BFS, attached to their host entity by FromID.
func documentRecords(doc *graph.Document) []types.EntityRecord {
	// Bucket relationships by their FromID so each becomes the host entity's
	// embedded Relationships (the shape buildGraph expects).
	relsByFrom := make(map[string][]types.RelationshipRecord)
	for _, rel := range doc.Relationships {
		relsByFrom[rel.FromID] = append(relsByFrom[rel.FromID], types.RelationshipRecord{
			FromID: rel.FromID,
			ToID:   rel.ToID,
			Kind:   rel.Kind,
		})
	}

	out := make([]types.EntityRecord, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		out[i] = types.EntityRecord{
			ID:            e.ID,
			Name:          e.Name,
			QualifiedName: e.QualifiedName,
			Kind:          e.Kind,
			Subtype:       e.Subtype,
			SourceFile:    e.SourceFile,
			StartLine:     e.StartLine,
			EndLine:       e.EndLine,
			Language:      e.Language,
			Tags:          e.Tags,
			Relationships: relsByFrom[e.ID],
		}
	}
	return out
}

// resolveReport resolves the LCOV report path to parse, returning (path, true)
// when a readable report exists. Resolution order:
//
//  1. cfg.ReportPaths globs (when cfg.Enabled()), repo-relative, first match
//     that exists on disk wins; globs are evaluated deterministically.
//  2. DefaultReportPath under repoRoot when it exists (zero-config discovery).
//
// Returns ("", false) when nothing is found — the LCOV sub-pass then no-ops.
func resolveReport(repoRoot string, cfg Config) (string, bool) {
	if cfg.Enabled() {
		var matches []string
		for _, glob := range cfg.ReportPaths {
			pat := glob
			if !filepath.IsAbs(pat) {
				pat = filepath.Join(repoRoot, glob)
			}
			m, err := filepath.Glob(pat)
			if err != nil {
				continue
			}
			matches = append(matches, m...)
		}
		sort.Strings(matches)
		for _, m := range matches {
			if fileExists(m) {
				return m, true
			}
		}
		// cfg enabled but nothing matched: do NOT fall through to default
		// discovery — the group explicitly chose its paths.
		return "", false
	}

	// Zero-config discovery.
	def := filepath.Join(repoRoot, DefaultReportPath)
	if fileExists(def) {
		return def, true
	}
	return "", false
}

// parseReportFile opens and parses a coverage report file. format is the
// pinned Config.Format ("" auto-detects from content); it dispatches through
// ParseReport so LCOV/Cobertura/JaCoCo all share one ingestion path.
func parseReportFile(path, format string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseReport(format, f)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
