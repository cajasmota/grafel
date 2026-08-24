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
	// HistoryDir is the directory holding previously written reports
	// (~/.grafel/feedback). Prior reports for GroupName are read from here to
	// decide whether a kind with zero semantic participation LOST edges it
	// used to have (a regression) or never had any (terminal by design) —
	// see history.go and check 2b in sanity.go (#6377). Empty disables the
	// longitudinal comparison, which is the first-run behaviour.
	HistoryDir string
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
	OrphanByKind map[string]KindStats // kind → DEFECT orphan stats (kinds with N < 10 suppressed)
	// OrphanTerminalByKind holds the same shape for orphans of kinds with ZERO
	// observed semantic participation anywhere in the group — i.e. the graph
	// gives no evidence the kind ever carries a semantic edge in either
	// direction, so it is terminal by construction as far as we can tell.
	// Derived from the graph, not from a list of kind names (#6346). These are
	// reported separately from the defect count so the raw signal survives
	// instead of being silently dropped.
	OrphanTerminalByKind map[string]KindStats

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

	// priorParticipation is per-kind semantic participation observed in
	// previously stored reports for this group: true = participated, false =
	// observed but never participated, absent = no history for that kind.
	// Populated from Opts.HistoryDir; consumed by check 2b (#6377).
	priorParticipation map[string]bool
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
		GeneratedAt:          time.Now().UTC(),
		GroupName:            opts.GroupName,
		Version:              opts.Version,
		EntitiesByLanguage:   make(map[string]int),
		OrphanByKind:         make(map[string]KindStats),
		OrphanTerminalByKind: make(map[string]KindStats),
		FrameworkHits:        make(map[string]int),
	}

	// Merge all docs into aggregate structures.
	// We collect raw (un-suppressed) counts first, then suppress after.
	kindTotals := make(map[string]int)                // kind → total
	kindOrphans := make(map[string]int)               // kind → orphan count
	kindLangCounts := make(map[string]map[string]int) // kind → lang → count

	// To detect orphans: an entity is orphan when it has NO semantic edge in
	// EITHER direction — its only edges are CONTAINS / DECLARES, or it has
	// none at all. Direction matters (#6313): several kinds are sinks by
	// construction and source nothing. `http_endpoint_definition` is the
	// worked example — internal/engine/http_endpoint_resolve.go
	// bridgeEndpointToHandler emits `FromID: handler, ToID: definition`, so a
	// CORRECTLY-wired endpoint has zero outgoing semantic edges. Counting
	// outgoing edges only reported 33.4% of endpoints orphaned across 10
	// corpus repos while 0.00% were actually isolated, and degenerated toward
	// 100% at group scale because the one outgoing kind a definition sources
	// (ENTRY_POINT_OF) is gated behind the 300-process cap in
	// internal/engine/process_flow.go. It was also framework-unfair: per
	// internal/graph/coverage.go the handler<->definition edge points
	// definition->handler in Spring/Express and handler->definition in
	// NestJS/Django/Flask, so two graphs of identical quality scored 0% and
	// ~100%. Tracking both directions removes all three effects at once.
	type edgeSummary struct {
		semanticOut int // non-CONTAINS/DECLARES edges sourced by this entity
		semanticIn  int // non-CONTAINS/DECLARES edges targeting this entity
	}
	entityEdges := make(map[string]*edgeSummary)

	// Index of entity ID → kind/subtype for the orphan lookup pass.
	entityKind := make(map[string]string)
	entityLang := make(map[string]string)
	entitySubtype := make(map[string]string)

	// classCandidate records a class-like entity (Subtype != "field") that is
	// eligible for the field-extraction metric, along with its raw
	// Properties["field_count"] (empty if the extractor never set it — true
	// for the dominant Go/Java/Python producers, which emit fields as child
	// entities instead; see fieldChildCount below).
	type classCandidate struct {
		id            string
		fieldCountRaw string
	}
	var classCandidates []classCandidate

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

	// Pass 1: entities. Populates all per-entity indices used by pass 2
	// (relationships) below. Kept as a separate pass — rather than
	// interleaved per-doc like before — so cross-doc relationships can
	// reliably look up entity kind/subtype regardless of doc order.
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
			// NOTE (#6378): kindTotals is deliberately NOT incremented here.
			// This loop runs once per entity OCCURRENCE per document, while
			// the orphan numerator (entityEdges/kindOrphans, below) is keyed
			// by unique entity ID. Incrementing here mixed the units: emitting
			// the same document twice doubled the denominator without moving
			// the numerator, so an unwired kind went 11/12 (91.7%, gate FIRES)
			// → 11/24 (45.8%, gate SILENT). kindTotals is derived from
			// entityKind after this pass instead — see below.
			if kindLangCounts[kind] == nil {
				kindLangCounts[kind] = make(map[string]int)
			}
			if lang != "" {
				kindLangCounts[kind][lang]++
			}

			entityKind[e.ID] = kind
			entityLang[e.ID] = lang
			entitySubtype[e.ID] = e.Subtype

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
			if e.PropGet("framework") != "" {
				annotated++
				r.FrameworkHits[e.PropGet("framework")]++
			}

			// Field extraction for Class/Model kinds. Real FB-loaded entities carry
			// canonical kinds — bare ("Model") or namespaced ("SCOPE.Class",
			// "SCOPE.Schema") — never the lowercase "class"/"struct"/"model" that
			// the in-memory unit fixtures used, which scored "No class or model
			// entities found" against production data. Match case-insensitively on
			// the namespace-stripped tail (see kindTail, mirroring
			// internal/graph/coverage.go) and include schema.
			//
			// Entities that are not field-bearing class containers are
			// excluded from the denominator — see isFieldExtractionCandidate
			// for the exemption set and the reasoning (#6536).
			if isFieldExtractionCandidate(kind, e.Subtype, e.Language) {
				classCandidates = append(classCandidates, classCandidate{
					id:            e.ID,
					fieldCountRaw: e.PropGet("field_count"),
				})
			}
		}

		// Count framework-file signals from Properties["framework"] presence on ANY entity.
		frameworkFilesSeen := make(map[string]bool)
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if src := e.SourceFile; src != "" && e.PropGet("framework") != "" {
				frameworkFilesSeen[src] = true
			}
		}
		r.FrameworkFilesDetected += len(frameworkFilesSeen)
	}

	// kindTotals: unique entity IDs per kind (#6378).
	//
	// OrphanPct is the answer to "what share of the entities of this kind have
	// no semantic edge in either direction" — a question about ENTITIES, not
	// about how many times an entity was emitted. Every consumer reads it that
	// way, and its numerator (kindOrphans, derived from entityEdges) is already
	// keyed by unique ID, so the denominator must be too or the ratio is
	// dimensionally mixed and dilutable by duplication.
	//
	// entityKind is the same index entityEdges is keyed on, so deriving from it
	// guarantees the two sides can never drift apart again. Duplicate IDs are
	// not hypothetical: #6368 was an EntityID collision (solidity import
	// placeholders deduped by path, named by basename), and any group whose
	// repos share files emits the same entity into more than one document.
	//
	// Blast radius: kindTotals also drives the N >= 10 publication floor below,
	// so a kind whose entities were only ever double-counted over the floor now
	// drops out of the report. That is correct — it never had 10 distinct
	// entities.
	for _, kind := range entityKind {
		kindTotals[kind]++
	}

	// Pass 2: relationships. fieldChildCount is built here (over ALL docs)
	// before any orphan/field-extraction classification happens, so it doesn't
	// matter which doc emitted the child entity vs. the structural edge
	// pointing at it.
	fieldChildCount := make(map[string]int) // parent entity ID → count of field children

	for _, doc := range docs {
		if doc == nil {
			continue
		}
		for i := range doc.Relationships {
			rel := &doc.Relationships[i]

			if isStructuralEdge(rel.Kind) {
				// Fields are extracted as CHILD entities (Kind tail "schema",
				// Subtype "field") linked to their parent by a structural
				// CONTAINS/DECLARES edge (internal/extractor/structural_ref.go
				// BuildSchemaFieldStructuralRef) — never via a
				// Properties["field_count"] on the parent. Count the real
				// children so the field-extraction metric reflects the graph
				// instead of reading a property the dominant extractors never
				// write.
				//
				// "field" is not the only name a container gives its
				// declared members: a SQL table's members are Subtype
				// "column" (#6543). Match the MEMBER-subtype set rather
				// than the single literal — see memberChildSubtypes.
				if memberChildSubtypes[entitySubtype[rel.ToID]] {
					fieldChildCount[rel.FromID]++
				}
			} else {
				// Semantic edge tracking for orphan detection, in BOTH
				// directions (#6313). CONTAINS/DECLARES edges do NOT reduce
				// the orphan count (handled above).
				if es, ok := entityEdges[rel.FromID]; ok {
					es.semanticOut++
				}
				if es, ok := entityEdges[rel.ToID]; ok {
					es.semanticIn++
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

	// Finalise the field-extraction metric: honor Properties["field_count"]
	// when the (niche) extractor set it, otherwise fall back to the real
	// field-child count, which is the source of truth for the dominant
	// Go/Java/Python producers.
	classCount := len(classCandidates)
	classZeroFields := 0
	for _, c := range classCandidates {
		if c.fieldCountRaw != "" {
			if c.fieldCountRaw == "0" {
				classZeroFields++
			}
			continue
		}
		if fieldChildCount[c.id] == 0 {
			classZeroFields++
		}
	}

	// Derive which kinds are TERMINAL BY CONSTRUCTION from the graph itself,
	// rather than from a hand-maintained list of kind/subtype names (#6346;
	// a name list is the #6361 failure class). The question a name list was
	// answering is "can this kind carry a semantic edge at all?", and the
	// group's own graph answers it: count how many entities of each kind
	// participate in ANY semantic edge, in either direction. A kind with zero
	// observed participation gives us no evidence it ever carries semantics,
	// so its unwired entities are reported in the expected/terminal bucket
	// instead of the defect count. One participating entity is enough to
	// prove the kind CAN be wired, at which point every unwired sibling is a
	// genuine gap and belongs in the defect bucket.
	//
	// Two known limitations, stated rather than hidden.
	//
	// (1) A resolver regression that wipes out EVERY semantic edge of a kind
	// is indistinguishable, from the graph alone, from a kind that is terminal
	// by design. Both look like zero participation. We resolve the ambiguity
	// toward "terminal" HERE — but not silently: sanity.go check 2b raises a
	// `kind-carries-semantic-edges` failure for every such kind, so a total
	// regression still costs confidence and still gets named. render.go labels
	// the bucket accordingly and the entities stay counted and visible.
	//
	// (2) This derivation is per-KIND, which is strictly coarser than the two
	// per-entity rules it replaced (a field leaf was exempted individually by
	// Subtype; a container Component by a subtype name list). One participating
	// non-field member of a kind therefore flips every unwired field leaf of
	// that kind into the defect bucket. That is the deliberate trade — the
	// per-subtype exemption is exactly the hand-maintained name list #6346
	// asked us to delete, and the #6361 failure class — but it is a real cost,
	// measured: of the 9 kinds that newly fail the orphan-rate gate across 12
	// corpus repos, 2 (express-realworld and laravel-routing SCOPE.Schema) are
	// this effect rather than new signal, dominated by field leaves (43/60 and
	// 109/112 of their unwired entities). TestGenerate_MixedParticipation-
	// SchemaFlipsFieldLeaves pins both sides of it.
	kindSemanticParticipation := make(map[string]int)
	for id, es := range entityEdges {
		if es.semanticOut > 0 || es.semanticIn > 0 {
			kindSemanticParticipation[entityKind[id]]++
		}
	}

	// Compute orphan counts per kind, split into DEFECT vs expected/terminal.
	kindTerminalOrphans := make(map[string]int)
	for id, es := range entityEdges {
		// Direction-aware: an entity pointed AT by a semantic edge is attached
		// to the graph even when it sources nothing (#6313).
		if es.semanticOut != 0 || es.semanticIn != 0 {
			continue
		}
		kind := entityKind[id]

		if kindSemanticParticipation[kind] == 0 {
			kindTerminalOrphans[kind]++
			continue
		}

		kindOrphans[kind]++
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

		if terminal := kindTerminalOrphans[kind]; terminal > 0 {
			tpct := 100.0 * float64(terminal) / float64(total)
			r.OrphanTerminalByKind[kind] = KindStats{
				Total:       total,
				OrphanCount: terminal,
				OrphanPct:   tpct,
			}
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
	// Longitudinal history (#6377). Read-only and best-effort: a failure to
	// read prior reports degrades check 2b to its first-run behaviour rather
	// than failing report generation.
	if prior, err := loadKindParticipation(opts.HistoryDir, opts.GroupName); err == nil {
		r.priorParticipation = prior
	}

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
// "component" is here because the entire C# family emits types under it: every
// C#/VB.NET class, structure, module, interface and delegate is SCOPE.Component
// (internal/extractors/vbnet/extractor.go entityKind, which follows
// internal/extractors/csharp/csharp.go). Omitting it meant the metric had never
// sampled a real class in any C#-family codebase — the only survivors of the
// candidate filter were the SCOPE.Schema enums and consts, which cannot own
// fields, so the reported rate was a guaranteed 100% rather than a measurement
// (#6536, surfaced by #6535). Any change to this set must be checked against
// the kinds the extractors actually emit, not against these literals.
//
// "datastore" is here because every SQL table is SCOPE.Datastore
// (internal/extractors/sql/sql.go:249) with CONTAINS(contained_kind=column)
// children — a class-like container with declared members, and exactly what
// this metric is for, which had never been sampled (#6543). Admitting the tail
// is only half the fix: the members are Subtype "column", so the numerator has
// to see them (memberChildSubtypes) and the non-SQL emitters of the same kind
// have to be exempted (datastoreMemberBearingLanguages), or the tail merely
// re-creates #6536 for a new population.
var classLikeKindTails = map[string]bool{
	"class":     true,
	"struct":    true,
	"model":     true,
	"schema":    true,
	"component": true,
	"datastore": true,
}

// memberChildSubtypes are the child subtypes that count as a container's
// DECLARED MEMBERS — the numerator of the field-extraction metric (#6543).
//
// The metric asks "does this container's members show up in the graph", and
// different extractors give the same relationship different names. Matching
// the single literal "field" meant SQL's members were invisible: a table's
// children are Subtype "column" (internal/extractors/sql/sql.go:358, :1021),
// declared on the CONTAINS edge itself as contained_kind=column (sql.go:240-242).
//
// Every subtype here must also appear in nonClassSubtypes below: a member is a
// LEAF, and a leaf that counts as its parent's member must never be counted as
// a container in its own right, or it enters the denominator as a permanent
// zero-field failure. That symmetry is what "field" already had and what
// "column" was missing — before #6543 the three columns of a three-column
// table were themselves 3 of the 3 entities in the denominator.
var memberChildSubtypes = map[string]bool{
	"field":  true,
	"column": true,
}

// datastoreMemberBearingLanguages are the languages whose SCOPE.Datastore
// entities are real member-bearing containers, and so belong in the
// field-extraction denominator (#6543).
//
// SCOPE.Datastore has 13 non-test emit sites and only the SQL ones are
// container-shaped. The other three emitters produce datastores with no member
// children anywhere in their output:
//
//   - jcl — a DD dataset (extractor.go:664, :779) is a file reference; the
//     jcl extractor emits no Subtype "field" or "column" at all.
//   - cobol — an IMS database / message queue (ims.go:170) and a CICS queue
//     or file resource (depth.go:701, :864) are external resources, not
//     declarations. cobol DOES emit Subtype "field" (extractor.go:748,
//     ims.go:491) for its data items, which is why the exemption is keyed on
//     the KIND-plus-language rather than on the language wholesale: a blanket
//     cobol exemption would drop its genuine field-bearing records.
//   - erlang — an ETS table (otp_deepen.go:407) is a runtime table with no
//     declared columns. (erlang is already exempt wholesale via
//     nonFieldBearingLanguages; it is listed here so the population is
//     recorded in one place and stays excluded if that ever changes.)
//
// Stated as an allowlist rather than a denylist deliberately: a new extractor
// emitting SCOPE.Datastore is excluded until someone checks that its entities
// actually own members. The failure mode of guessing wrong in that direction
// is an unmeasured population; guessing wrong the other way puts a
// guaranteed-zero-field population in the denominator, which is the defect
// this whole line of issues (#6535, #6536, #6543) is about.
var datastoreMemberBearingLanguages = map[string]bool{
	"sql": true,
}

// nonClassSubtypes are the subtypes that carry a class-like KIND but are not
// field-bearing class/model CONTAINERS, and so are excluded from the
// field-extraction denominator (#6536).
//
// The decision, stated rather than left incidental: they are exempt.
//
//   - "field" — a field LEAF is itself the child, not a container. The tail
//     "schema" matches these leaves too, so without this exclusion every field
//     would double as a "class" with zero fields.
//   - "enum" / "const" — a const is a single value and an enum's members are
//     enum members, never fields; no extractor that emits these subtypes
//     (vbnet, csharp, cpp, php, proto, avro, solidity) emits a Subtype "field"
//     child under one. Counting them means every such entity is permanently a
//     zero-field failure, which puts a floor of false failures under the
//     metric and is exactly what produced the misleading 100% in #6535.
//   - "file" — every extractor's file carrier is a SCOPE.Component
//     (internal/extractor.FileEntity). It is a container, but it is not a
//     class, and admitting "component" above would otherwise enrol one
//     guaranteed-zero-field entity per indexed file.
//   - "import" — an import PLACEHOLDER: a reference to a module, never a
//     declaration, so it can never own a field child. cross/imports emits one
//     SCOPE.Component/import per imported module for C#, Java, Go, Python,
//     Ruby, Rust, JS/TS and Elixir (cross/imports/extractor.go), and eighteen
//     more extractor packages emit the same kind/subtype pair. That is one to
//     several guaranteed-zero-field entities PER FILE — a population that
//     outnumbers classes in most repos and, left in, reproduces #6535's
//     dominated-denominator symptom with the "file" hole merely plugged.
//   - "delegate" — a delegate is a signature, not a container; the VB.NET and
//     C# extractors give it no members at all (verified against extractor
//     output: `Public Delegate Sub Handler(...)` emits zero CONTAINS edges).
//
// A metric whose denominator contains populations that cannot pass reports
// noise, not coverage.
//   - "column" — the SQL analogue of "field": a member LEAF, not a container.
//     Its kind tail is "schema" (internal/extractors/sql/sql.go:358), which
//     this set already admits, so before #6543 every column was in the
//     denominator as a permanent zero-field failure — a three-column table
//     contributed three guaranteed failures and zero passes.
var nonClassSubtypes = map[string]bool{
	"field":    true,
	"column":   true,
	"enum":     true,
	"const":    true,
	"file":     true,
	"import":   true,
	"delegate": true,
}

// interfaceSubtypeFieldBearingLanguages are the languages in which a
// Subtype "interface" entity really can own Subtype "field" children, and so
// must STAY in the field-extraction denominator (#6536 round 2).
//
// "interface" is exempt everywhere else — verified against extractor output
// rather than assumed: VB.NET's `IThing` contains only its methods, C# and
// VB.NET forbid interface fields outright, a Java interface's constants are
// emitted as a SCOPE.Enum constant group and not as field children, Go
// interfaces hold only method specs, and the JS/TS extractor emits no
// Subtype "field" at all.
//
// Kotlin is the exception and it is a real one: `interface Shape { val sides:
// Int }` emits `Shape` CONTAINS `Shape.sides` with Subtype "field". Exempting
// interfaces wholesale would drop a population that genuinely passes, which is
// the same error as counting one that cannot — just pointed the other way.
var interfaceSubtypeFieldBearingLanguages = map[string]bool{
	"kotlin": true,
}

// nonFieldBearingLanguages are the languages whose extractor package emits no
// Subtype "field" ANYWHERE, so every entity it produces is a guaranteed
// zero-field failure regardless of subtype (#6536 round 2).
//
// This is what enrolled their per-file `Subtype: "module"` carriers — a file
// carrier under another name (haskell/extractor.go, elm, ocaml, fsharp, idris,
// reasonml, rescript, crystal, erlang) — and Haskell's import placeholders,
// which set no Subtype at all so the subtype exclusions above cannot see them.
//
// It is deliberately an allowlist of EXEMPTIONS keyed on the language, not a
// blanket exclusion of the "module" subtype: a VB.NET `Module` is a genuine
// field-bearing container (verified: `Public Module Util` CONTAINS its Shared
// and Private fields), so exempting "module" globally would delete real
// passers from the C#-family population #6535 is about.
//
// If any of these extractors gains field extraction, remove it from this set
// or the new fields are silently unmeasured.
var nonFieldBearingLanguages = map[string]bool{
	"haskell":  true,
	"elm":      true,
	"ocaml":    true,
	"fsharp":   true,
	"idris":    true,
	"reasonml": true,
	"rescript": true,
	"crystal":  true,
	"erlang":   true,
}

// isFieldExtractionCandidate reports whether an entity belongs in the
// field-extraction denominator: it must carry a class-like kind AND be a
// container that can actually own a Subtype "field" child.
//
// The governing rule, stated once: a population that cannot pass must not be
// in the denominator, and a population that can pass must not be dropped from
// it. Both halves are checked against what the extractors emit (#6536).
func isFieldExtractionCandidate(kind, subtype, language string) bool {
	if !isClassLikeKind(kind) {
		return false
	}
	if nonClassSubtypes[subtype] {
		return false
	}
	if nonFieldBearingLanguages[strings.ToLower(language)] {
		return false
	}
	if subtype == "interface" && !interfaceSubtypeFieldBearingLanguages[strings.ToLower(language)] {
		return false
	}
	// SCOPE.Datastore is admitted for SQL tables, which own Subtype "column"
	// members; the jcl/cobol/erlang emitters of the same kind own none (#6543).
	if kindTail(kind) == "datastore" && !datastoreMemberBearingLanguages[strings.ToLower(language)] {
		return false
	}
	return true
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
