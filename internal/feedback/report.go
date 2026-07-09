package feedback

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
)

// minEntitiesForReport is the hard floor below which the report is suppressed
// to avoid statistically unreliable metrics and fingerprinting risk.
const minEntitiesForReport = 50

// Opts controls report generation behaviour.
type Opts struct {
	// GroupName is the grafel group name — used in the report header.
	GroupName string
	// Version is the grafel binary version string — used in the header.
	Version string
}

// KindStats holds per-entity-kind orphan metrics.
type KindStats struct {
	Total       int
	OrphanCount int
	OrphanPct   float64
}

// ResolutionVector holds the disposition breakdown for graph relationships.
type ResolutionVector struct {
	ResolvedPct        float64
	ExternalKnownPct   float64
	ExternalUnknownPct float64
	BugExtractorPct    float64
	BugResolverPct     float64
	DynamicPct         float64
}

// EntityKindLang is one row in the entity kind × language table.
type EntityKindLang struct {
	Kind     string
	Language string
	Count    int
}

// SourceWindowStats captures completeness of start/end line coverage.
type SourceWindowStats struct {
	TotalWithWindow int
	TotalEntities   int
	PctComplete     float64
}

// Report is the fully computed, anonymized feedback report.
type Report struct {
	// Meta
	GeneratedAt time.Time
	GroupName   string
	Version     string

	// Summary counts — used in the header profile line.
	TotalEntities      int
	TotalRelationships int
	Languages          []string

	// Section 1 — Extractor Coverage
	EntitiesByLanguage map[string]int    // lang → count (suppressed when < 10)
	EntityKindDist     []EntityKindLang  // kind × lang rows (suppressed when < 10)
	SourceWindow       SourceWindowStats // source-window completeness
	AnnotationCoverage struct {
		TotalAnnotated int
		Total          int
		PctAnnotated   float64
	}
	FieldExtractionRate struct {
		ClassTotal    int
		ZeroFieldsPct float64
	}

	// Section 2 — Orphan Rate
	OrphanByKind map[string]KindStats // kind → orphan stats (kinds with N < 10 suppressed)

	// Section 3 — Resolution Disposition
	Resolution      ResolutionVector
	ResolutionTotal int // total edges examined

	// Section 4 — Framework Recognition
	FrameworkHits          map[string]int // framework → entity count
	FrameworkFilesDetected int            // number of files with known-framework signals

	// Sanity + Confidence
	SanityResults []SanityResult
	Confidence    int // percentage 0–100

	// suppressed is true when TotalEntities < minEntitiesForReport.
	suppressed bool
}

// Generate loads the graphs for the given repos, computes anonymized metrics,
// and returns a Report. The salt is generated ephemerally inside this function
// and is never persisted or logged.
//
// docs is a slice of already-loaded graph.Document values (one per repo in the
// group). Pass them in from the caller so this package stays free of daemon RPC
// logic.
func Generate(_ context.Context, docs []*graph.Document, opts Opts) (*Report, error) {
	// Generate per-report ephemeral salt. Never stored, never logged.
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("feedback.Generate: generate salt: %w", err)
	}

	r := &Report{
		GeneratedAt:        time.Now().UTC(),
		GroupName:          opts.GroupName,
		Version:            opts.Version,
		EntitiesByLanguage: make(map[string]int),
		OrphanByKind:       make(map[string]KindStats),
		FrameworkHits:      make(map[string]int),
	}

	// Merge all docs into aggregate structures.
	// We collect raw (un-suppressed) counts first, then suppress after.
	kindTotals := make(map[string]int)                // kind → total
	kindOrphans := make(map[string]int)               // kind → orphan count
	kindLangCounts := make(map[string]map[string]int) // kind → lang → count

	// To detect orphans: an entity is orphan when its only outgoing edges are
	// CONTAINS / DECLARES, or it has no outgoing edges at all.
	// We track outgoing semantic edges per entity ID.
	type edgeSummary struct {
		semanticOut int // edges that are not CONTAINS/DECLARES
	}
	entityEdges := make(map[string]*edgeSummary)

	// Index of entity ID → kind for the orphan lookup pass.
	entityKind := make(map[string]string)
	entityLang := make(map[string]string)

	// Resolution disposition counters.
	var (
		resResolved        int
		resExternalKnown   int
		resExternalUnknown int
		resBugExtractor    int
		resBugResolver     int
		resDynamic         int
	)

	annotated := 0
	totalAnnotated := 0
	classCount := 0
	classZeroFields := 0

	for _, doc := range docs {
		if doc == nil {
			continue
		}
		r.TotalEntities += len(doc.Entities)
		r.TotalRelationships += len(doc.Relationships)

		for i := range doc.Entities {
			e := &doc.Entities[i]
			lang := strings.ToLower(e.Language)
			kind := e.Kind

			if lang != "" {
				r.EntitiesByLanguage[lang]++
			}
			kindTotals[kind]++
			if kindLangCounts[kind] == nil {
				kindLangCounts[kind] = make(map[string]int)
			}
			if lang != "" {
				kindLangCounts[kind][lang]++
			}

			entityKind[e.ID] = kind
			entityLang[e.ID] = lang

			// Initialise edge summary so even no-edge entities appear.
			if _, ok := entityEdges[e.ID]; !ok {
				entityEdges[e.ID] = &edgeSummary{}
			}

			// Source-window completeness: start_line > 0 is the navigable-window
			// anchor. The graph.fb schema has NO end-line slot — fbEntityToGraphEntity
			// (internal/graph/load.go) populates StartLine from SourceLine() and
			// leaves EndLine == 0 for every FB-loaded entity — so requiring
			// EndLine > StartLine scored 0.0% against real production data. Start
			// line alone is what get_source anchors on, so it is the correct signal.
			if e.StartLine > 0 {
				r.SourceWindow.TotalWithWindow++
			}

			// Annotation coverage.
			totalAnnotated++
			if e.Properties["framework"] != "" {
				annotated++
				r.FrameworkHits[e.Properties["framework"]]++
			}

			// Field extraction for Class/Model kinds. Real FB-loaded entities carry
			// canonical kinds — bare ("Model") or namespaced ("SCOPE.Class",
			// "SCOPE.Schema") — never the lowercase "class"/"struct"/"model" that
			// the in-memory unit fixtures used, which scored "No class or model
			// entities found" against production data. Match case-insensitively on
			// the namespace-stripped tail (see kindTail, mirroring
			// internal/graph/coverage.go) and include schema.
			if isClassLikeKind(kind) {
				classCount++
				// Fields are typically emitted as child entities; we use
				// Properties["field_count"] when available.
				if e.Properties["field_count"] == "0" || e.Properties["field_count"] == "" {
					classZeroFields++
				}
			}
		}

		// Count framework-file signals from Properties["framework"] presence on ANY entity.
		frameworkFilesSeen := make(map[string]bool)
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if src := e.SourceFile; src != "" && e.Properties["framework"] != "" {
				frameworkFilesSeen[src] = true
			}
		}
		r.FrameworkFilesDetected += len(frameworkFilesSeen)

		// Process relationships.
		for i := range doc.Relationships {
			rel := &doc.Relationships[i]

			// Semantic edge tracking for orphan detection.
			if !isStructuralEdge(rel.Kind) {
				if es, ok := entityEdges[rel.FromID]; ok {
					es.semanticOut++
				}
			}

			// Resolution disposition, derived STRUCTURALLY from the edge ToID shape
			// — the same classification `orient view=stats` uses to compute import
			// fidelity (internal/resolve.IsResolvedToID / IsBugEdgeToID). The
			// pipeline never writes a Properties["resolution"] tag, so the previous
			// property switch always reported "no resolution property found on
			// edges". A 16-hex ToID is a bound entity ID (resolved); an ext:-prefixed
			// ToID is a known external (external-known); any other non-empty ToID is
			// an unresolved stub (bug-extractor). Empty ToIDs carry no disposition.
			switch {
			case rel.ToID == "":
				// no disposition — nothing to resolve
			case resolve.IsResolvedToID(rel.ToID):
				if len(rel.ToID) > 4 && rel.ToID[:4] == "ext:" {
					resExternalKnown++
				} else {
					resResolved++
				}
			default:
				resBugExtractor++
			}
		}
	}

	// Compute orphan counts per kind.
	for id, es := range entityEdges {
		if es.semanticOut == 0 {
			kind := entityKind[id]
			kindOrphans[kind]++
		}
	}

	// Build OrphanByKind (suppress kinds with N < 10).
	for kind, total := range kindTotals {
		if total < 10 {
			continue
		}
		orphans := kindOrphans[kind]
		pct := 0.0
		if total > 0 {
			pct = 100.0 * float64(orphans) / float64(total)
		}
		r.OrphanByKind[kind] = KindStats{
			Total:       total,
			OrphanCount: orphans,
			OrphanPct:   pct,
		}
	}

	// Build EntityKindDist (suppress rows where kind+lang count < 10).
	for kind, langMap := range kindLangCounts {
		for lang, count := range langMap {
			if count < 10 {
				continue
			}
			// Anonymize: we do NOT hash the kind or language — those are
			// structural labels, not user identifiers.
			r.EntityKindDist = append(r.EntityKindDist, EntityKindLang{
				Kind:     kind,
				Language: lang,
				Count:    bucketCount(count),
			})
		}
	}

	// Suppress per-language counts < 10.
	for lang, count := range r.EntitiesByLanguage {
		if count < 10 {
			delete(r.EntitiesByLanguage, lang)
		}
	}

	// Build language list (sorted by count desc for the header).
	r.Languages = sortedLanguages(r.EntitiesByLanguage)

	// Source-window completeness.
	r.SourceWindow.TotalEntities = r.TotalEntities
	if r.TotalEntities > 0 {
		r.SourceWindow.PctComplete = 100.0 * float64(r.SourceWindow.TotalWithWindow) / float64(r.TotalEntities)
	}

	// Annotation coverage.
	r.AnnotationCoverage.Total = totalAnnotated
	r.AnnotationCoverage.TotalAnnotated = annotated
	if totalAnnotated > 0 {
		r.AnnotationCoverage.PctAnnotated = 100.0 * float64(annotated) / float64(totalAnnotated)
	}

	// Field extraction rate.
	r.FieldExtractionRate.ClassTotal = classCount
	if classCount > 0 {
		r.FieldExtractionRate.ZeroFieldsPct = 100.0 * float64(classZeroFields) / float64(classCount)
	}

	// Resolution disposition vector.
	total := resResolved + resExternalKnown + resExternalUnknown + resBugExtractor + resBugResolver + resDynamic
	r.ResolutionTotal = total
	if total > 0 {
		tf := float64(total)
		r.Resolution = ResolutionVector{
			ResolvedPct:        100.0 * float64(resResolved) / tf,
			ExternalKnownPct:   100.0 * float64(resExternalKnown) / tf,
			ExternalUnknownPct: 100.0 * float64(resExternalUnknown) / tf,
			BugExtractorPct:    100.0 * float64(resBugExtractor) / tf,
			BugResolverPct:     100.0 * float64(resBugResolver) / tf,
			DynamicPct:         100.0 * float64(resDynamic) / tf,
		}
	}

	// Suppress report if too few entities.
	if r.TotalEntities < minEntitiesForReport {
		r.suppressed = true
	}

	// Framework hits: suppress entries with count < 10.
	for fw, count := range r.FrameworkHits {
		if count < 10 {
			delete(r.FrameworkHits, fw)
		}
	}

	// Run sanity checks.
	r.SanityResults, r.Confidence = runSanityChecks(r)

	return r, nil
}

// IsSuppressed reports whether the report was suppressed due to insufficient data.
func (r *Report) IsSuppressed() bool { return r.suppressed }

// isStructuralEdge returns true for CONTAINS / DECLARES edge kinds that represent
// containment rather than semantic connectivity.
func isStructuralEdge(kind string) bool {
	return kind == "CONTAINS" || kind == "DECLARES"
}

// kindTail returns the lower-cased, namespace-stripped kind for matching:
// "SCOPE.Class" → "class", "Model" → "model". Mirrors the normalizer used by
// internal/graph/coverage.go so raw and canonical SCOPE.* kinds are treated
// identically and language-agnostically.
func kindTail(kind string) string {
	k := strings.ToLower(kind)
	if i := strings.LastIndex(k, "."); i >= 0 {
		k = k[i+1:]
	}
	return k
}

// classLikeKindTails is the set of namespace-stripped kind tails that carry
// class/model/field-bearing semantics for the field-extraction metric. Real
// FB-loaded graphs use canonical kinds (SCOPE.Class, SCOPE.Schema, SCOPE.Model,
// bare Model) — never the lowercase literals the in-memory unit fixtures used.
var classLikeKindTails = map[string]bool{
	"class":  true,
	"struct": true,
	"model":  true,
	"schema": true,
}

// isClassLikeKind reports whether kind is a class/model/schema-shaped entity
// kind, matched case-insensitively on the namespace-stripped tail.
func isClassLikeKind(kind string) bool {
	return classLikeKindTails[kindTail(kind)]
}

// bucketCount maps a raw count to a privacy-preserving range bucket.
// Instead of exact numbers, the report shows ranges to prevent fingerprinting.
func bucketCount(n int) int {
	switch {
	case n <= 5:
		return 3 // centre of 1-5
	case n <= 20:
		return 13 // centre of 6-20
	case n <= 100:
		return 60 // centre of 21-100
	default:
		return 200 // 100+
	}
}

// sortedLanguages returns language names sorted by entity count descending.
func sortedLanguages(m map[string]int) []string {
	type lc struct {
		lang  string
		count int
	}
	rows := make([]lc, 0, len(m))
	for l, c := range m {
		rows = append(rows, lc{l, c})
	}
	// Simple insertion sort (small N).
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].count > rows[j-1].count; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	langs := make([]string, len(rows))
	for i, r := range rows {
		langs[i] = r.lang
	}
	return langs
}
