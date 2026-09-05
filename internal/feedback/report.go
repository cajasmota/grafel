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

// SourceWindowStats captures START-LINE coverage only. It carries no end-line
// signal: TotalWithWindow counts entities with StartLine > 0 and never consults
// EndLine (#6827 — the name "window" and the old rendered label both promised a
// two-sided check that has never been performed).
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
	// RelKindByLanguage is the (language × relationship kind) matrix (#6479):
	// source-entity language → relationship kind → edge count. Every other
	// field on this struct is keyed on language OR on kind and never on the
	// pair, so a language emitting entities but no hierarchy edges passed
	// every sanity check green.
	//
	// DELIBERATELY NOT SUPPRESSED at `< 10`, unlike its four neighbours in
	// this file — EntitiesByLanguage, EntityKindDist, OrphanByKind and
	// FrameworkHits all drop small entries. A language that emits zero of a
	// relationship kind is by construction small on that axis, so inheriting
	// the floor would make this matrix unable to show the one thing it exists
	// to show. Counts are rendered as ranges, so the un-suppressed map does
	// not widen the fingerprinting surface beyond the existing tables.
	//
	// A language appears here as soon as it contributes ONE entity, even if it
	// sources no relationship at all — absence of a row means "language not
	// observed", never "language observed and fine".
	//
	// Report-only: nothing gates on this. A gate would fire on the languages
	// that emit no EXTENDS/IMPLEMENTS today and would immediately need a
	// suppression list for a third of its inputs.
	RelKindByLanguage map[string]map[string]int
	// RelKindUnattributed counts edges that could NOT be attributed to a
	// language: relationship kind -> count. An edge lands here when its FromID
	// names no entity in the graph (a dangling source) or names one whose
	// Language is empty. Kept and rendered as its own row rather than dropped:
	// #6479 is an issue about relationships going unnoticed, so a matrix that
	// silently discarded the edges it cannot place would rebuild the defect
	// inside the fix. A row rather than a prose total because WHICH kinds are
	// unattributable is the actionable part -- a large unattributed EXTENDS
	// bucket means the hierarchy edges exist and the attribution is broken,
	// which reads completely differently from a large unattributed CONTAINS.
	RelKindUnattributed map[string]int
	AnnotationCoverage  struct {
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
	// OrphanLeafByKind holds the same shape for orphans that are TERMINAL BY
	// SUBTYPE — storage leaves (`field`, `column`, `property`) inside a kind
	// that otherwise participates (#6599). The per-kind derivation below
	// cannot express these: SCOPE.Operation participates because methods call
	// things, so before #6599 every property leaf of the kind landed in the
	// defect count. They are excluded from OrphanByKind's numerator and
	// reported here instead — never in OrphanTerminalByKind, whose membership
	// history.go reads as proof the KIND never participated.
	OrphanLeafByKind map[string]KindStats

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
		RelKindByLanguage:    make(map[string]map[string]int),
		RelKindUnattributed:  make(map[string]int),
		OrphanByKind:         make(map[string]KindStats),
		OrphanTerminalByKind: make(map[string]KindStats),
		OrphanLeafByKind:     make(map[string]KindStats),
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
				// Seed the (language × relationship kind) matrix from the
				// ENTITY pass (#6479), not from the edge pass: a language that
				// sources no relationship of any kind must still get a row, so
				// the report can say "emits none" instead of omitting it.
				if r.RelKindByLanguage[lang] == nil {
					r.RelKindByLanguage[lang] = make(map[string]int)
				}
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
			// anchor, and it is the ONLY thing counted here. Requiring
			// EndLine > StartLine scored 0.0% against real production data,
			// because the graph.fb Entity table had no end-line slot at the
			// time: fbEntityToGraphEntity (internal/graph/load.go) left
			// EndLine == 0 for every FB-loaded entity.
			//
			// CORRECTION (#6827): that is no longer true of the schema. #6236
			// added the slot — internal/graph/fbwriter/writer.go writes EndLine
			// and fbEntityToGraphEntity reads it back — so end lines CAN
			// survive a round trip now. Nor is the pre-#6236 population a
			// reason: those files are not loaded span-less, they are REJECTED
			// (load.go's minSupportedFBFormatVersion is fbversion.Version = 6,
			// and #6236 raised it precisely to force the reindex that
			// repopulates spans), so that population is empty by construction.
			//
			// The check stays one-sided on a live measurement instead. The
			// golden fixture is a POST-#6236 v6 graph.fb — a12a59321
			// regenerated it — and TestGenerate_GoldenFB still observes 0 of
			// its 672 entities carrying an EndLine. So the current writer, on
			// the current schema, round-trips a real multi-language graph with
			// no spans at all. Extractors do set EndLine in many places
			// (~110 files under internal/extractor*), but no CORPUS-WIDE
			// measurement of how often it reaches a loaded graph exists, and
			// 0/672 is the only number there is. Widening this to a span check
			// needs that corpus number first; it must not be done on the
			// strength of the slot existing.
			//
			// Until then the metric is start-line-only and the rendered label
			// says so — see the caption in render.go. Do not restore a label
			// that promises a span this does not check.
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

			// (language × relationship kind) matrix (#6479). Attributed to the
			// language of the SOURCE entity — the extractor that emitted the
			// edge — and counted for EVERY kind, structural ones included:
			// isStructuralEdge's CONTAINS/DECLARES split is the classification
			// this issue is complaining about (it makes CALLS "semantic" and so
			// lets a language with no hierarchy edges look connected), so the
			// matrix does not reuse it. entityLang is complete here: pass 1
			// runs over every document before this loop starts.
			if lang := entityLang[rel.FromID]; lang != "" {
				if r.RelKindByLanguage[lang] == nil {
					r.RelKindByLanguage[lang] = make(map[string]int)
				}
				r.RelKindByLanguage[lang][rel.Kind]++
			} else {
				// Dangling FromID, or a source entity carrying no language. Both
				// are counted rather than dropped -- see RelKindUnattributed.
				r.RelKindUnattributed[rel.Kind]++
			}

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
	// (2) This derivation is per-KIND, which cannot see a STORAGE LEAF inside a
	// participating kind. That cost was accepted at #6346 and measured at
	// #6583: on 3 real VB.NET trees SCOPE.Operation read 51.3% orphan, of which
	// `property` leaves were 75.2% of the pool, and SCOPE.Schema `field` leaves
	// were 90.8% of that kind's. SCOPE.Operation participates by construction —
	// methods call things — so no per-kind rule can ever excuse them.
	//
	// #6599 therefore restores the leaf half of the exemption ONLY, keyed on
	// terminalLeafSubtypes, and leaves terminality itself per-kind (#6538 wants
	// to re-key it per (kind, subtype-class); that is a different change and is
	// pinned against by TestGenerate_MixedParticipationSchemaFlipsFieldLeaves).
	// Exempted leaves go to OrphanLeafByKind, NOT to the terminal bucket, and
	// the denominator is untouched: what counts as an orphan did not change,
	// only which orphans are charged as defects.
	kindSemanticParticipation := make(map[string]int)
	for id, es := range entityEdges {
		if es.semanticOut > 0 || es.semanticIn > 0 {
			kindSemanticParticipation[entityKind[id]]++
		}
	}

	// Compute orphan counts per kind, split into DEFECT vs expected/terminal.
	kindTerminalOrphans := make(map[string]int)
	kindLeafOrphans := make(map[string]int)
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

		// Terminal BY SUBTYPE: a storage leaf declares state and nothing else,
		// so it has nothing to link and is not a gap (#6599). Checked AFTER
		// the per-kind test so a wholly unwired kind still reports as terminal
		// — that is the bucket history.go joins on.
		if terminalLeafSubtypes[strings.ToLower(entitySubtype[id])] {
			kindLeafOrphans[kind]++
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

		if leaves := kindLeafOrphans[kind]; leaves > 0 {
			lpct := 100.0 * float64(leaves) / float64(total)
			r.OrphanLeafByKind[kind] = KindStats{
				Total:       total,
				OrphanCount: leaves,
				OrphanPct:   lpct,
			}
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
// have to be exempted (datastoreMemberBearingKinds) — including the six SQL
// subtypes that are not `table` — or the tail merely re-creates #6536 for a
// new population.
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

// datastoreMemberBearingKinds are the SCOPE.Datastore EMIT SITES that produce
// real member-bearing containers, keyed "<language>/<subtype>" (#6543).
//
// The unit here is the emit site, not the kind, not the language and not the
// subtype — because none of those three is a single population, and picking
// any one of them alone re-creates #6536 in a new place:
//
//   - Not the KIND. SCOPE.Datastore has 13 non-test emit sites and only one
//     of them owns members.
//   - Not the LANGUAGE. "sql" is SEVEN emit sites: table (sql.go:249), view
//     (:419), index (:444), function/trigger_function (:527), procedure
//     (:585), trigger (:634) and dbt_source (:695). Only `table` emits
//     CONTAINS(contained_kind=column) children. A language-keyed gate admits
//     all seven, so a schema file with one fully-columned table, a view and
//     an index reports 66% zero-field — and indexes alone outnumber tables in
//     most real schema files, so the reported SQL rate would be dominated by
//     the population that structurally cannot pass. Measured, not assumed:
//     see TestOnlyTableSubtypeIsMemberBearing_6543.
//   - Not the SUBTYPE. Erlang's ETS/Mnesia datastores are Subtype
//     `<engine>_table` (erlang/otp_deepen.go:407) — "ets_table",
//     "mnesia_table". A bare "table" allowlist does not admit them today, but
//     the two namespaces are one rename apart and nothing would catch the
//     collision.
//
// The excluded emitters, verified against extractor output rather than
// assumed: jcl DD datasets (extractor.go:664, :779) are file references, and
// the jcl extractor emits no member subtype at all; cobol IMS databases
// (ims.go:170) and CICS queues / file resources (depth.go:701, :864) are
// external resources, not declarations; erlang ETS tables are runtime tables
// with no declared columns.
//
// Note on cobol, since an earlier version of this comment cited the wrong
// code: cobol's genuinely member-bearing population is IMS SEGMENTS, which
// are kindIMSSegment = SCOPE.Schema / "ims-segment" (ims.go:295) with Subtype
// "field" children (ims.go:490, edge at :498-505). They are admitted by the
// "schema" tail and are untouched by this gate. Cobol's WORKING-STORAGE data
// items (extractor.go:748) are NOT the counter-example: group items carry
// Subtype "field" themselves, so nonClassSubtypes excludes them as leaves.
//
// Stated as an allowlist rather than a denylist deliberately: an emit site
// absent from this map is excluded, so a NEW SCOPE.Datastore emitter is out
// until someone checks that it owns members. The failure mode of guessing
// wrong in that direction is an unmeasured population; guessing wrong the
// other way puts a guaranteed-zero-field population in the denominator, which
// is what #6535, #6536 and #6543 are all about. That choice is observed by
// TestUnknownDatastoreEmitSitesAreExcluded_6543 — a denylist passes the known
// languages identically and is only distinguishable on an emit site nobody
// enumerated.
var datastoreMemberBearingKinds = map[string]bool{
	"sql/table": true,
}

// datastoreEmitSiteKey builds the "<language>/<subtype>" key for
// datastoreMemberBearingKinds, case-insensitively like the other language
// gates in this file.
func datastoreEmitSiteKey(language, subtype string) string {
	return strings.ToLower(language) + "/" + strings.ToLower(subtype)
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
//
// terminalLeafSubtypes are the subtypes whose entities are STORAGE LEAVES: they
// declare state and nothing else, so they have nothing to link and their being
// unwired is not an edge gap (#6599).
//
// It is the same judgement nonClassSubtypes already encodes for the
// field-extraction denominator, re-asked for the orphan bucket — but the two
// sets are NOT in a subset relation, and saying so precisely matters because
// the difference is where the judgement is actually being made. Their
// INTERSECTION ({"field", "column"}) is a strict subset of nonClassSubtypes;
// "property" is ADDED here deliberately, and is in neither set today —
// nonClassSubtypes is keyed on class-like KINDS, and a VB.NET property is
// SCOPE.Operation, one kind over, so the question never arose there.
//
// In the other direction, nonClassSubtypes exempts populations that are not
// leaves and whose unwired state IS signal. Copying it whole would excuse them:
//
//   - "file" — a file carrier is a CONTAINER, and an unwired one is a real
//     finding (#6597 measured 51 of 274 orphaned, 16.8% of that kind's pool).
//   - "import" — an import placeholder is a REFERENCE to a module, and a
//     reference that references nothing is a defect, not a leaf: it exists
//     solely to carry an IMPORTS edge, so an unwired one means the edge was
//     never built or the placeholder was never pruned. (It is NOT true that an
//     entry here would have hidden #6597's mechanism-B carriers: those were
//     SCOPE.Component with Subtype UNSET — which is precisely why the
//     Subtype == "import" prune predicate skipped them, considered=0 — so no
//     subtype-keyed rule could have reached them either way. #6601 fixed the
//     emit site, so the exclusion bites from now on rather than retroactively.)
//   - "enum" / "const" / "delegate" — exempt from the FIELD metric because they
//     own no members, which is a different question from whether they can be
//     referenced. They can: an enum with no inbound reference is a genuine gap.
//
// What is in it, and why each is a leaf rather than a container:
//
//   - "field" — the child of a container, extracted as its own entity
//     (BuildSchemaFieldStructuralRef). Its only edge is the structural
//     CONTAINS the orphan definition excludes by design.
//   - "column" — the SQL analogue of "field" (sql.go:358), same shape (#6543).
//     Included BY ANALOGY, not by measurement: columns appear in neither #6583
//     nor #6597, so unlike "field" and "property" there is no corpus number
//     behind this entry. It is expected to be inert in practice — a column
//     population is normally wholly terminal, so the per-KIND test above
//     catches it first and this entry is never reached — and it is here so a
//     mixed SQL kind behaves like the mixed schema kinds that WERE measured.
//     If a corpus ever shows columns carrying semantic edges, re-measure it.
//   - "property" — VB.NET/C# emit properties as SCOPE.Operation
//     (vbnet/extractor.go:253-256), i.e. the field-leaf shape one KIND over.
//     Measured (#6583, 4,304 properties): auto-properties 94.3% orphaned, full
//     properties with no call site 97.5%, and full properties WITH call sites
//     0.0% — every property that has something to link is already linked. So
//     this exemption only ever reaches the ones that declare storage; a wired
//     property is not an orphan and never enters this branch, and no property
//     leaves the denominator either way.
//
// The set is deliberately small. An over-broad entry here makes the orphan rate
// look healthy by excusing entities that genuinely should carry edges, which is
// strictly worse than the over-reporting #6599 exists to fix — so the set is
// asserted MEMBER BY MEMBER, and by length, in
// TestTerminalLeafSubtypesIsExactlyTheseThree_6599. A fixture loop over a
// hand-listed set of non-leaf subtypes cannot do that job: it can only fail on
// the subtypes someone thought to list, and "enum", "const", "variable" and
// "parameter" are all real emitted subtypes that such a list missed. Widening
// this set must be a deliberate edit to that test.
var terminalLeafSubtypes = map[string]bool{
	"field":    true,
	"column":   true,
	"property": true,
}

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
// reasonml, rescript, crystal, erlang).
//
// It ALSO used to be the only thing exempting their import placeholders, which
// set no Subtype at all so the subtype exclusions above could not see them.
// That second job is now mostly discharged upstream: #6369 (fsharp) and #6481
// arms A1 (haskell, elm, ocaml), A2 (reasonml, rescript) and A3 (idris) made
// buildImportEntities stamp Subtype "import", which nonClassSubtypes already
// excludes one step earlier and independently of the language. Two entries
// still rely on this set for their placeholders: erlang, whose placeholder sets
// no Subtype at all, and crystal, whose placeholder sets Subtype "module" —
// which nonClassSubtypes does not exclude either. The haskell and idris halves
// are observed, not merely asserted, in
// TestNonFieldBearingLanguageCarriersExempt_6536 and
// TestIdrisImportPlaceholdersMarked_6481.
//
// The `module` carriers rely on this set REGARDLESS, in every one of the nine:
// "module" is deliberately absent from nonClassSubtypes because a VB.NET
// `Module` is a genuine field-bearing container. So no entry may be removed
// here on the grounds that its import placeholders are now marked. Asserted
// rule-by-rule in TestNonFieldBearingLanguageCarriersExempt_6536.
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
	// SCOPE.Datastore is admitted only for the emit sites that own members —
	// today just the SQL `table`. Its six sibling SQL subtypes and the
	// jcl/cobol/erlang datastores own none (#6543).
	if kindTail(kind) == "datastore" && !datastoreMemberBearingKinds[datastoreEmitSiteKey(language, subtype)] {
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
