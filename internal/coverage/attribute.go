package coverage

import (
	"fmt"
	"strconv"

	"github.com/cajasmota/archigraph/internal/types"
)

// Property keys written onto attributed entities. Stored in
// EntityRecord.Properties (string map) rather than introducing new entity
// Kinds, per the prefer-Properties-over-Kinds rule.
const (
	PropCoveragePct    = "coverage_pct"     // "0".."100", one-decimal string
	PropCoveredLines   = "covered_lines"    // integer string
	PropTotalLines     = "total_lines"      // integer string
	PropCoverageSource = "coverage_source"  // e.g. "lcov"
	PropCoverageMeasAt = "coverage_measured_at"
)

// coverage_source values stamped onto attributed entities, one per parser.
const (
	SourceLCOV      = "lcov"
	SourceCobertura = "cobertura"
	SourceJaCoCo    = "jacoco"
)

// Attribution is the computed coverage for one graph entity.
type Attribution struct {
	EntityID     string
	CoveragePct  float64
	CoveredLines int
	TotalLines   int
	Source       string
}

// Properties renders the attribution as the property map merged onto the
// entity. measuredAt (RFC3339) is optional; when empty it is omitted.
func (a Attribution) Properties(measuredAt string) map[string]string {
	props := map[string]string{
		PropCoveragePct:    strconv.FormatFloat(a.CoveragePct, 'f', 1, 64),
		PropCoveredLines:   strconv.Itoa(a.CoveredLines),
		PropTotalLines:     strconv.Itoa(a.TotalLines),
		PropCoverageSource: a.Source,
	}
	if measuredAt != "" {
		props[PropCoverageMeasAt] = measuredAt
	}
	return props
}

// fileScopeKinds are the entity kinds whose coverage is the whole-file
// roll-up. There is no dedicated FILE kind in the graph (files are carried as
// the SourceFile field), so module/project entities act as the file-scope
// carrier when their span is unset.
//
// isFileScope reports whether an entity should receive whole-file coverage:
// either it explicitly spans the file (no usable line span) or it is a
// module-level aggregation.
func isFileScope(e types.EntityRecord) bool {
	if e.StartLine <= 0 || e.EndLine <= 0 || e.EndLine < e.StartLine {
		return true
	}
	switch types.EntityKind(e.Kind) {
	case types.EntityKindModule, types.EntityKindProject:
		return true
	}
	return false
}

// Attribute computes per-entity line coverage from a parsed LCOV report.
//
// For each entity it finds the report file whose path matches the entity's
// (Normalize-d) SourceFile, then:
//   - file-scope entities (no usable span, or Module/Project) get the whole-file
//     covered/total/pct;
//   - function/method/other span-bearing entities get the coverage of the lines
//     within [StartLine, EndLine] that the report instrumented.
//
// rootPrefix is the configurable LCOV path root (see Normalize). Entities whose
// source file is absent from the report are skipped (no Attribution emitted) so
// the caller can distinguish "measured 0%" from "not in report".
//
// This is a pure transformation: it does NOT mutate the entities or touch the
// indexer/daemon write path. The indexer hook is a deliberate seam — see
// ApplyAttributions and the package doc.
func Attribute(entities []types.EntityRecord, rep *Report, rootPrefix string) []Attribution {
	if rep == nil {
		return nil
	}
	// Pre-normalize report file paths once.
	type nf struct {
		norm string
		fc   *FileCoverage
	}
	normed := make([]nf, len(rep.Files))
	for i := range rep.Files {
		normed[i] = nf{norm: Normalize(rep.Files[i].Path, rootPrefix), fc: &rep.Files[i]}
	}

	// The coverage_source stamped on every attribution follows the report's
	// originating parser; default to LCOV for reports built without a Source.
	source := rep.Source
	if source == "" {
		source = SourceLCOV
	}

	out := make([]Attribution, 0, len(entities))
	for _, e := range entities {
		esrc := Normalize(e.SourceFile, "")
		var fc *FileCoverage
		for i := range normed {
			if samePath(normed[i].norm, esrc) {
				fc = normed[i].fc
				break
			}
		}
		if fc == nil {
			continue
		}

		var covered, total int
		if isFileScope(e) {
			covered, total = fc.CoveredLines, fc.TotalLines
		} else {
			covered, total = spanCoverage(fc, e.StartLine, e.EndLine)
			// A span with no instrumented lines (e.g. an interface or a
			// declaration-only entity) carries no coverage signal; skip it.
			if total == 0 {
				continue
			}
		}

		pct := 0.0
		if total > 0 {
			pct = 100.0 * float64(covered) / float64(total)
		}
		out = append(out, Attribution{
			EntityID:     entityID(e),
			CoveragePct:  pct,
			CoveredLines: covered,
			TotalLines:   total,
			Source:       source,
		})
	}
	return out
}

// spanCoverage counts instrumented (covered, total) lines within [start,end].
func spanCoverage(fc *FileCoverage, start, end int) (covered, total int) {
	for ln, hits := range fc.LineHits {
		if ln < start || ln > end {
			continue
		}
		total++
		if hits > 0 {
			covered++
		}
	}
	return covered, total
}

// entityID returns a stable identifier for the entity: the precomputed ID when
// present, otherwise a span-qualified fallback so distinct entities in one file
// don't collide.
func entityID(e types.EntityRecord) string {
	if e.ID != "" {
		return e.ID
	}
	return fmt.Sprintf("%s:%s:%d-%d", e.SourceFile, e.Name, e.StartLine, e.EndLine)
}

// ApplyAttributions merges the computed coverage properties onto a copy of the
// entities, keyed by entityID. It returns a new slice; inputs are not mutated.
//
// This is the indexer-hook SEAM for v1: the live indexer/enrichment pass can
// call ApplyAttributions on the entity batch after extraction, behind the
// per-group coverage config (see Config). It is intentionally NOT wired into
// the daemon write path in this changeset — doing so safely requires the
// offline enrichment-pass plumbing tracked as a follow-up (the indexer must
// resolve the report glob at index time, which this isolated v1 cannot
// live-validate against the daemon).
func ApplyAttributions(entities []types.EntityRecord, attrs []Attribution, measuredAt string) []types.EntityRecord {
	byID := make(map[string]Attribution, len(attrs))
	for _, a := range attrs {
		byID[a.EntityID] = a
	}
	out := make([]types.EntityRecord, len(entities))
	copy(out, entities)
	for i := range out {
		a, ok := byID[entityID(out[i])]
		if !ok {
			continue
		}
		if out[i].Properties == nil {
			out[i].Properties = map[string]string{}
		} else {
			// copy to avoid mutating shared map backing
			cp := make(map[string]string, len(out[i].Properties))
			for k, v := range out[i].Properties {
				cp[k] = v
			}
			out[i].Properties = cp
		}
		for k, v := range a.Properties(measuredAt) {
			out[i].Properties[k] = v
		}
	}
	return out
}
