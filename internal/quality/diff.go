package quality

import (
	"strings"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
)

// isPlaceholderAnchor reports whether e is a stand-in that its own producer
// declares is not the entity it is named after, and which therefore cannot
// satisfy an ExpectedEntity (Refs #6277).
//
// A must-have entity expectation asserts that the indexer EXTRACTED a given
// declaration. Matching on (Kind, Name) — even narrowed by SourceFile — cannot
// tell that apart from an entity that merely carries those strings.
//
// This check covers ONE producer: internal/extractors/cross/ormlink. Its
// package doc (extractor.go:43-49) says it emits no entity kind of its own, and
// that the SCOPE.Component it does emit is a "thin sentinel" tagged with
// SubtypeSentinel whose only job is to give the resolver's
// embedded-relationship rewrite loop an anchor for the MAPS_TO edge. It holds
// none of the model's members.
//
// It is NOT the only stand-in in the graph, and this predicate is knowingly
// incomplete. internal/extractors/cross/hierarchy synthesises nodes for
// supertypes it has only seen referenced — seventeen sites set
// Properties["provenance"]="INFERRED_FROM_CLASS_HIERARCHY" — and those wear
// REAL subtypes ("class", "interface", "trait") plus the REFERENCING file as
// SourceFile, so no subtype-keyed rule can see them; their marker is the
// provenance property, for which a predicate already exists at
// internal/engine/classfold.go:124 (IsClassHierarchyShadow). One is live in the
// corpus today: java-quartz-mini emits SCOPE.Component Job, subtype "interface",
// SourceFile jobs/SendEmailJob.java, StartLine 0 — an inferred stand-in for the
// Quartz interface, which has no source in the fixture. It satisfies no
// expectation java-quartz-mini declares, so nothing is mis-scored by it today.
// Extending to that producer is a separate change: classfold.go:138-139 says a
// hierarchy shadow that folds into nothing is KEPT as the single node for its
// class, so rejecting them wholesale would deny real classes, and picking the
// stand-ins out of the survivors needs its own reasoning and blast radius.
//
// This is checked against the producer's own subtype tag rather than against
// the shape of the entity. A "no source span" rule looks equivalent — two of
// the three anchors in the golden corpus carry StartLine 0 — but measured over
// the graphs the twenty golden fixtures produce it would additionally reject 17
// must-haves that are synthesised by design and legitimately carry no span,
// across five fixtures: php-slim-mini's five http_endpoint_definition entities,
// rust-tokio-mini's five file modules, swift-package-mini's three SwiftPM
// products, kotlin-spring-mini's three REST SCOPE.Operations, and
// java-spring-mini's own Route /users — the fixture this check exists for. It
// would also MISS the anchor in elixir-phoenix-mini, which is stamped StartLine
// 1 / EndLine 24. See TestZeroSpanEntitiesStillSatisfyMustHaves.
//
// Deliberately scoped to entity expectations. resolveExpectedEdge still binds
// these entities, because the anchor is what a MAPS_TO edge hangs off and
// refusing it there would make that edge unassertable.
func isPlaceholderAnchor(e *graph.Entity) bool {
	return e.Subtype == ormlink.SubtypeSentinel
}

// isSynthesisedStandIn reports whether e is a node the graph produced as a
// stand-in for a declaration it never saw, rather than a declaration the
// indexer extracted. It is the union of the two producers the doc above names:
// the ormlink sentinel, and the class-hierarchy pass's inferred supertype.
//
// This is a SEPARATE predicate from isPlaceholderAnchor on purpose. That one
// gates entity expectations and edge-endpoint binding, where the doc above
// spells out why widening it to hierarchy shadows is its own change with its
// own blast radius (classfold.go:138-139 keeps a shadow that folds into nothing
// as the single node for its class, so rejecting them wholesale would deny real
// classes). This one gates NOTHING that scores: its only consumer is the
// nameIsEntity set that classifies a to_bare_name target for the #6476
// diagnostic, where a false positive costs a wrong sentence and never a point.
//
// The hierarchy arm exists because the #6476 advice — "use to_name + to_kind" —
// is actively HARMFUL for a shadow. Binding a fixture expectation to a
// synthesised stand-in is precisely the false positive #6277 exists to prevent:
// the expectation would then pass on a node the indexer inferred rather than on
// the declaration the fixture means to assert. java-quartz-mini's SCOPE.Component
// Job is live in the corpus today. So a bare-name row whose only same-named
// entity is a shadow falls through to the pre-existing "to-entity not extracted"
// message, which is the honest reading: nothing was extracted for that name.
//
// internal/engine.IsClassHierarchyShadow (classfold.go:124) is the same
// predicate one layer earlier, but it is typed on *types.EntityRecord — the
// PRE-assembly record — and cannot be called with a post-assembly graph.Entity.
// The provenance property survives assembly (load.go copies all Properties),
// and internal/mcp/denoise.go:143 already reads it off a graph.Entity the same
// way. Importing internal/engine here would compile (engine does not import
// quality), but it would buy a heavyweight dependency and still need the
// conversion, so the property read is inlined instead.
func isSynthesisedStandIn(e *graph.Entity) bool {
	return isPlaceholderAnchor(e) ||
		e.PropGet("provenance") == "INFERRED_FROM_CLASS_HIERARCHY"
}

// firstExtracted returns the FIRST candidate that is a real extracted entity,
// skipping placeholder anchors. nil when every candidate is a placeholder (or
// there are none).
//
// First, not last, and that is load-bearing when two REAL entities share a
// (Kind, Name, SourceFile) key: candidates are appended in doc.Entities order,
// so this returns the earliest-emitted one. It makes the file-narrowed lookup
// agree with the unnarrowed byKindName path in resolveEntity, which has always
// taken es[0]. The pre-#6277 single-value map disagreed with that path — it
// kept whichever entity was written LAST — so picking first is a deliberate
// choice to have one rule rather than two. See
// TestFileNarrowedLookupPrefersTheEarliestOfTwoRealCandidates.
func firstExtracted(cands []*graph.Entity) *graph.Entity {
	for _, e := range cands {
		if !isPlaceholderAnchor(e) {
			return e
		}
	}
	return nil
}

// lastOf narrows a candidate bucket to its final element, or none. It exists so
// the relationship path keeps the exact pre-#6277 semantics of byKindNameFile
// when that index was a single map value written once per entity: last write
// wins. See the call sites in resolveExpectedEdge.
func lastOf(cands []*graph.Entity) []*graph.Entity {
	if len(cands) == 0 {
		return nil
	}
	return cands[len(cands)-1:]
}

// EntityResult records the outcome of evaluating one ExpectedEntity.
type EntityResult struct {
	Expected ExpectedEntity
	Found    bool
	// MatchedID is the Entity.ID we bound the expectation to (empty when
	// no match). Useful for debugging fixture authors.
	MatchedID string
}

// RelationshipResult records the outcome of evaluating one
// ExpectedRelationship.
//
// FromResolved / ToResolved report whether the expectation's endpoints
// could even be resolved to extracted entities — if not, the recall miss
// is most likely a missing ENTITY rather than a missing edge. The reporter
// surfaces this so fixture authors can fix the root cause.
type RelationshipResult struct {
	Expected     ExpectedRelationship
	Found        bool
	FromResolved bool
	ToResolved   bool
	// ToBareNameIsEntity reports that the row's to_bare_name target is the
	// NAME of an extracted, non-synthesised entity — and that no entity
	// carries that string as its ID (case-insensitively). Such a row cannot
	// match a resolved target: with ToName empty there are no "to" candidates,
	// so the only path left is literal ToID equality, and an entity that
	// resolved carries a hashed ID rather than the bare string. The miss is
	// the fixture's, not the extractor's (#6476, and the mechanism-1 check of
	// #6441), and the reporter says so instead of blaming the extractor.
	//
	// It describes the TO endpoint ONLY, and says nothing about the row's
	// other defects. report.go therefore prints its advice only once BOTH
	// endpoints resolved: "use to_name + to_kind" repairs the to side and
	// cannot repair a from endpoint that was never extracted. The field is
	// serialised (to_bare_name_is_entity) regardless of which arm printed, so
	// a machine consumer reading to_resolved:true is not left to conclude the
	// extractor dropped an edge no extractor could have satisfied.
	//
	// Invariant: false on every path that MATCHED — a row that hit is
	// matchable by definition, whatever its target looks like. Pinned by
	// TestMatchedBareNameRowNeverCarriesTheFlag_6476.
	ToBareNameIsEntity bool
	// MatchedRelID is the Relationship.ID of the edge we matched, when one
	// was found. Empty otherwise.
	MatchedRelID string
}

// Report is the full diff between a Fixture and an extracted graph.Document.
type Report struct {
	FixtureName string

	// Entity scoring.
	EntityResults    []EntityResult
	EntityExpected   int // total must_exist
	EntityFound      int // must_exist AND found
	EntityExtractedN int // |doc.Entities| — extra context, not in recall

	// Relationship scoring.
	RelResults    []RelationshipResult
	RelExpected   int
	RelFound      int
	RelExtractedN int // |doc.Relationships| — extra context

	// Forbidden-relationship hits — false positives. Each entry is an
	// extracted relationship that matches a `forbidden_relationships`
	// entry. A non-zero count is a hard quality regression.
	ForbiddenHits []RelationshipResult

	// Nice-to-have stats — surfaced separately so authors see what they
	// could add without being penalised on must-have recall.
	NiceEntityFound int
	NiceEntityTotal int
	NiceRelFound    int
	NiceRelTotal    int
}

// EntityRecall returns the recall ratio over MUST_EXIST entities. Returns 0
// when nothing is expected (the harness reports that as N/A).
func (r *Report) EntityRecall() float64 {
	if r.EntityExpected == 0 {
		return 0
	}
	return float64(r.EntityFound) / float64(r.EntityExpected)
}

// RelationshipRecall returns the recall ratio over MUST_EXIST relationships.
func (r *Report) RelationshipRecall() float64 {
	if r.RelExpected == 0 {
		return 0
	}
	return float64(r.RelFound) / float64(r.RelExpected)
}

// Evaluate diffs an extracted graph.Document against a Fixture and returns
// a Report. It does NOT mutate either argument.
//
// The matching strategy is deliberately forgiving: a single extracted edge
// satisfies any expected edge that resolves to the same (FromID, ToID,
// Kind) triple. This keeps fixtures small and authoring practical.
func Evaluate(fix *Fixture, doc *graph.Document) *Report {
	rep := &Report{
		FixtureName:      fix.Name,
		EntityExtractedN: len(doc.Entities),
		RelExtractedN:    len(doc.Relationships),
	}

	// Build entity lookup tables once. We index by:
	//   - (kind, name)              -> []*Entity (case-sensitive)
	//   - (kind, name, sourceFile)  -> []*Entity (file-narrowed lookup)
	//   - qualified_name            -> *Entity
	//
	// Slice values rather than scalars because nothing in graph.Document
	// guarantees name+kind uniqueness for small fixtures (e.g. two `Meta`
	// inner classes).
	//
	// byKindNameFile is a slice for the same reason, which it was not before
	// #6277: adding SourceFile to the key narrows collisions but does not
	// remove them — ormlink's anchor collides with the real class on all three
	// fields — and a single map value silently kept whichever entity was
	// emitted LAST, so the file-narrowed lookup could not reach the real entity
	// past an anchor emitted after it. On its own that is masked by the
	// unnarrowed (Kind, Name) fallback below, which still holds the real
	// entity; it becomes observable once a same-named entity exists in another
	// file, because then the fallback answers with the wrong file's entity. See
	// TestFileNarrowedLookupSurvivesAnAnchorInTheSameFile.
	byKindName := make(map[string][]*graph.Entity)
	byKindNameFile := make(map[string][]*graph.Entity)
	byQName := make(map[string]*graph.Entity)
	// Kind-agnostic name/ID sets, used only to classify a to_bare_name target
	// (#6476). Kind-agnostic on purpose: a bare-name row states no ToKind, so
	// there is nothing to narrow by, and the question being asked — "did this
	// string resolve to an entity?" — does not depend on which kind it became.
	nameIsEntity := make(map[string]bool, len(doc.Entities))
	idIsEntity := make(map[string]bool, len(doc.Entities))
	for k := range doc.Entities {
		e := &doc.Entities[k]
		kn := e.Kind + "\x00" + e.Name
		knf := kn + "\x00" + e.SourceFile
		byKindName[kn] = append(byKindName[kn], e)
		byKindNameFile[knf] = append(byKindNameFile[knf], e)
		if e.QualifiedName != "" {
			byQName[e.QualifiedName] = e
		}
		// Empty keys are excluded from BOTH sets. An entity with a blank Name
		// or ID is not something a bare name can meaningfully "be", and
		// admitting "" would make a degenerate row (to_bare_name: "   ") trip
		// the flag against any such entity.
		if n := strings.TrimSpace(e.Name); n != "" && !isSynthesisedStandIn(e) {
			nameIsEntity[n] = true
		}
		if id := strings.TrimSpace(e.ID); id != "" {
			idIsEntity[strings.ToLower(id)] = true
		}
	}

	// Resolve each expected entity. We accept a hit when ANY extracted
	// entity matches — small fixtures with collisions can disambiguate
	// via the MatchBy / SourceFile fields.
	// Placeholder anchors are skipped on every path (#6277) — see
	// isPlaceholderAnchor. When the only candidate is an anchor the
	// expectation is reported as a MISS, which is the true state: nothing in
	// the graph is the declaration the fixture names.
	resolveEntity := func(ee ExpectedEntity) *graph.Entity {
		if ee.MatchBy == "qualified_name" && ee.QualifiedName != "" {
			if e, ok := byQName[ee.QualifiedName]; ok && !isPlaceholderAnchor(e) {
				return e
			}
			return nil
		}
		if ee.SourceFile != "" {
			if e := firstExtracted(byKindNameFile[ee.Kind+"\x00"+ee.Name+"\x00"+ee.SourceFile]); e != nil {
				return e
			}
		}
		return firstExtracted(byKindName[ee.Kind+"\x00"+ee.Name])
	}

	for _, ee := range fix.ExpectedEntities {
		ent := resolveEntity(ee)
		res := EntityResult{Expected: ee, Found: ent != nil}
		if ent != nil {
			res.MatchedID = ent.ID
		}
		rep.EntityResults = append(rep.EntityResults, res)
		switch {
		case ee.NiceToHave:
			rep.NiceEntityTotal++
			if res.Found {
				rep.NiceEntityFound++
			}
		case ee.MustExist:
			rep.EntityExpected++
			if res.Found {
				rep.EntityFound++
			}
		}
	}

	// Build a relationship lookup keyed on (FromID, ToID, Kind). We also
	// keep a (Kind, ToID) bucket for the bare-name match path below.
	type relKey struct{ from, to, kind string }
	relByTriple := make(map[relKey]*graph.Relationship, len(doc.Relationships))
	relByKindTo := make(map[string][]*graph.Relationship)
	relByKindFrom := make(map[string][]*graph.Relationship)
	for k := range doc.Relationships {
		r := &doc.Relationships[k]
		relByTriple[relKey{r.FromID, r.ToID, r.Kind}] = r
		relByKindTo[r.Kind+"\x00"+r.ToID] = append(relByKindTo[r.Kind+"\x00"+r.ToID], r)
		relByKindFrom[r.Kind+"\x00"+r.FromID] = append(relByKindFrom[r.Kind+"\x00"+r.FromID], r)
	}

	// resolveExpectedEdge tries every combination of from/to candidates so
	// fixtures don't have to spell out the SourceFile when there is no
	// collision. Returns (matched Relationship or nil, fromResolved,
	// toResolved, toBareNameIsEntity).
	//
	// toBareNameIsEntity is false on every path that MATCHED: a row that hit
	// is matchable by definition, whatever its target looks like. It is the
	// unsatisfiable-row flag of #6476, not a description of the target.
	resolveExpectedEdge := func(er ExpectedRelationship) (*graph.Relationship, bool, bool, bool) {
		// Candidate "from" entities.
		var fromCands []*graph.Entity
		if er.FromFile != "" {
			// lastOf, not the whole bucket. byKindNameFile became a slice for
			// the entity path (#6277); before that it was a single map value,
			// so a colliding key resolved to whichever entity was written LAST.
			// Handing the full slice here would let an edge match on a
			// candidate this lookup could never previously return — a widening,
			// in a change whose whole claim is that only entity scoring moves.
			// Declined deliberately: nothing in #6277 needs it, and whether the
			// edge path should consider every colliding candidate (as the
			// unnarrowed byKindName path below already does) is a real question
			// with its own blast radius, not a side effect of a re-keying.
			fromCands = lastOf(byKindNameFile[er.FromKind+"\x00"+er.FromName+"\x00"+er.FromFile])
		}
		if len(fromCands) == 0 {
			fromCands = byKindName[er.FromKind+"\x00"+er.FromName]
		}
		if len(fromCands) == 0 && er.FromKind == "" {
			// Best-effort: scan all kinds when fixture author left it blank.
			for k := range doc.Entities {
				e := &doc.Entities[k]
				if e.Name == er.FromName {
					fromCands = append(fromCands, e)
				}
			}
		}
		fromResolved := len(fromCands) > 0

		// Candidate "to" entities OR bare-name target.
		var toCands []*graph.Entity
		if er.ToFile != "" {
			toCands = lastOf(byKindNameFile[er.ToKind+"\x00"+er.ToName+"\x00"+er.ToFile])
		}
		if len(toCands) == 0 && er.ToName != "" {
			toCands = byKindName[er.ToKind+"\x00"+er.ToName]
			if len(toCands) == 0 && er.ToKind == "" {
				for k := range doc.Entities {
					e := &doc.Entities[k]
					if e.Name == er.ToName {
						toCands = append(toCands, e)
					}
				}
			}
		}
		// A to_bare_name row used to be declared resolved unconditionally
		// (`|| er.ToBareName != ""`), which made ToResolved carry no
		// information for any of the 47 bare-name rows in the golden set and
		// sent every such miss down report.go's "both endpoints exist; edge
		// not emitted" arm — i.e. at the extractor. Classify the bare target
		// instead (#6476):
		//
		//   - equal to some entity's ID  -> the row is matchable via the
		//     literal relByTriple ToID path below; resolved, no complaint.
		//     Compared case-INSENSITIVELY, because the relByKindFrom fallback
		//     below matches with strings.EqualFold: a bare "Vapor" against an
		//     entity whose ID is "vapor" is still matchable, and flagging it
		//     would be the overclaim this guard exists to avoid.
		//   - equal to some entity's NAME (and no entity's ID) -> the target
		//     resolved to an entity whose real ID is a hash, so this row can
		//     only ever hit an unresolved STUB edge carrying the bare string
		//     itself as its ToID. Resolved, and flagged as the fixture defect.
		//   - matching nothing -> genuinely unresolved, which is what the
		//     pre-existing "to-entity not extracted" message already says.
		//
		// A blank bare name classifies as nothing: both sets exclude the empty
		// key, so `to_bare_name: "   "` cannot trip either arm.
		bare := strings.TrimSpace(er.ToBareName)
		bareIsEntityID := bare != "" && idIsEntity[strings.ToLower(bare)]
		bareIsEntityName := bare != "" && !bareIsEntityID && nameIsEntity[bare]
		toResolved := len(toCands) > 0 || bareIsEntityID || bareIsEntityName

		// First pass: try the strict (from, to, kind) triple lookup over
		// every candidate combination.
		for _, fc := range fromCands {
			for _, tc := range toCands {
				if r, ok := relByTriple[relKey{fc.ID, tc.ID, er.Kind}]; ok {
					return r, fromResolved, toResolved, false
				}
			}
			if er.ToBareName != "" {
				if r, ok := relByTriple[relKey{fc.ID, er.ToBareName, er.Kind}]; ok {
					return r, fromResolved, true, false
				}
				// Bare-name comparison is whitespace-insensitive; the
				// indexer may emit a slightly mangled stub.
				for _, r := range relByKindFrom[er.Kind+"\x00"+fc.ID] {
					if strings.EqualFold(strings.TrimSpace(r.ToID), strings.TrimSpace(er.ToBareName)) {
						return r, fromResolved, true, false
					}
				}
			}
		}
		return nil, fromResolved, toResolved, bareIsEntityName
	}

	for _, er := range fix.ExpectedRelationships {
		match, fromOk, toOk, bareIsEnt := resolveExpectedEdge(er)
		res := RelationshipResult{
			Expected:           er,
			Found:              match != nil,
			FromResolved:       fromOk,
			ToResolved:         toOk,
			ToBareNameIsEntity: bareIsEnt,
		}
		if match != nil {
			res.MatchedRelID = match.ID
		}
		rep.RelResults = append(rep.RelResults, res)
		switch {
		case er.NiceToHave:
			rep.NiceRelTotal++
			if res.Found {
				rep.NiceRelFound++
			}
		case er.MustExist:
			rep.RelExpected++
			if res.Found {
				rep.RelFound++
			}
		}
	}

	// Forbidden edges — count any extracted edge that satisfies one of
	// the fixture's forbidden patterns.
	for _, fb := range fix.ForbiddenRelationships {
		match, fromOk, toOk, _ := resolveExpectedEdge(fb)
		if match != nil {
			rep.ForbiddenHits = append(rep.ForbiddenHits, RelationshipResult{
				Expected:     fb,
				Found:        true,
				FromResolved: fromOk,
				ToResolved:   toOk,
				MatchedRelID: match.ID,
			})
		}
	}

	return rep
}
