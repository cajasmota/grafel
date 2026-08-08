package quality

import (
	"testing"

	"github.com/cajasmota/grafel/internal/extractors/cross/ormlink"
	"github.com/cajasmota/grafel/internal/graph"
)

// Refs #6277. A must-have entity expectation asserts that the indexer
// EXTRACTED a given thing. Before this change Evaluate resolved that
// expectation by (Kind, Name) — optionally narrowed by SourceFile — and
// returned whichever entity carried those strings, so a stand-in that its own
// producer declares is not the real thing satisfied the expectation and the
// fixture scored green.
//
// The concrete instance is ormlink's anchor entity. Its package doc
// (internal/extractors/cross/ormlink/extractor.go:43-49, read) states it emits
// no entity kind of its own and that the SCOPE.Component it does emit is a
// "thin sentinel" carrying Subtype=SubtypeSentinel, whose purpose is to give
// the resolver's embedded-relationship rewrite loop an anchor for the MAPS_TO
// edge. It is not the ORM model class; it holds none of the class's members.
//
// These tests are hermetic: they build graph.Document values directly rather
// than indexing a fixture, so they pin the matcher's behaviour independently of
// whatever the Java/Python/Elixir extractors happen to emit today.

// realUser is the shape of the entity a fixture author means when they write
// {"name": "User", "kind": "SCOPE.Component", "source_file": "model/User.java"}:
// a declaration with a source span.
func realUser() graph.Entity {
	return graph.Entity{
		ID:         "real-user",
		Name:       "User",
		Kind:       "SCOPE.Component",
		SourceFile: "model/User.java",
		StartLine:  7,
		EndLine:    20,
		Language:   "java",
	}
}

// anchorUser is ormlink's stand-in, reproduced field-for-field from what
// `grafel quality --keep-graph internal/quality/golden/java-spring-mini`
// actually emits: same Kind, same Name, SAME SourceFile as the real class,
// Subtype=orm_model_sentinel, and no source span.
func anchorUser() graph.Entity {
	return graph.Entity{
		ID:            "anchor-user",
		Name:          "User",
		Kind:          "SCOPE.Component",
		Subtype:       ormlink.SubtypeSentinel,
		SourceFile:    "model/User.java",
		QualifiedName: "scope:ormmodel:model/User.java#User",
		StartLine:     0,
		EndLine:       0,
		Language:      "java",
	}
}

func userExpectation() ExpectedEntity {
	return ExpectedEntity{
		Name:       "User",
		Kind:       "SCOPE.Component",
		SourceFile: "model/User.java",
		MustExist:  true,
	}
}

// TestPlaceholderAnchorAloneDoesNotSatisfyMustHave is the assertion the
// identity check exists for. The graph contains the anchor and nothing else
// named User, which is exactly the state java-spring-mini and
// elixir-phoenix-mini are in: their fixtures score the must-have green while
// the only entity satisfying it is the anchor.
//
// It asserts the named expectation is unsatisfied AND that nothing was bound to
// it, not merely that some count moved.
func TestPlaceholderAnchorAloneDoesNotSatisfyMustHave(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{anchorUser()}}
	fix := &Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{userExpectation()}}

	rep := Evaluate(fix, doc)

	if len(rep.EntityResults) != 1 {
		t.Fatalf("EntityResults = %d, want 1", len(rep.EntityResults))
	}
	res := rep.EntityResults[0]
	if res.Found {
		t.Errorf("SCOPE.Component User was reported FOUND, bound to %q — that entity "+
			"carries Subtype=%q, which its producer emits as an anchor for a MAPS_TO "+
			"edge, not as the model class",
			res.MatchedID, ormlink.SubtypeSentinel)
	}
	if res.MatchedID != "" {
		t.Errorf("MatchedID = %q, want empty: nothing in the graph is the expected entity",
			res.MatchedID)
	}
	if rep.EntityFound != 0 {
		t.Errorf("EntityFound = %d, want 0", rep.EntityFound)
	}
	if rep.EntityExpected != 1 {
		t.Errorf("EntityExpected = %d, want 1", rep.EntityExpected)
	}
}

// TestFileNarrowedLookupSurvivesAnAnchorInTheSameFile pins the OTHER half of
// the change: byKindNameFile holds every candidate for a (Kind, Name, File) key
// rather than one.
//
// Adding SourceFile to the key narrows collisions but does not remove them —
// ormlink's anchor collides with the real class on all three fields — and a
// single map value silently kept whichever entity was emitted LAST.
//
// The decoy is what makes that observable. Without it, collapsing the bucket to
// the anchor merely empties the file-narrowed lookup and the expectation is
// rescued by the unnarrowed (Kind, Name) fallback, which still holds the real
// entity; the bug is real but invisible. With a same-named entity in a
// different file emitted FIRST, that fallback returns the decoy instead, so
// only a matcher that can actually see past the anchor inside the right file
// binds the right entity. Verified by mutation: reverting the slice keying
// fails this test and no other.
func TestFileNarrowedLookupSurvivesAnAnchorInTheSameFile(t *testing.T) {
	decoy := realUser()
	decoy.ID = "decoy-user"
	decoy.SourceFile = "other/User.java"

	doc := &graph.Document{Entities: []graph.Entity{decoy, realUser(), anchorUser()}}
	fix := &Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{userExpectation()}}

	rep := Evaluate(fix, doc)

	res := rep.EntityResults[0]
	if !res.Found {
		t.Fatalf("SCOPE.Component User not found, but the real declaration is in the graph")
	}
	if res.MatchedID != "real-user" {
		t.Errorf("bound MatchedID = %q, want %q — the expectation names %q and must "+
			"resolve inside that file, not to a same-named entity elsewhere",
			res.MatchedID, "real-user", "model/User.java")
	}
	if rep.EntityFound != 1 {
		t.Errorf("EntityFound = %d, want 1", rep.EntityFound)
	}
}

// TestRealEntityWinsWhenAnchorIsEmittedFirst is the adversarial case for the
// placeholder check itself: the anchor and the real declaration collide on
// (Kind, Name, SourceFile), so nothing about provenance separates them, and the
// anchor is first in the bucket. Only skipping it binds the declaration.
func TestRealEntityWinsWhenAnchorIsEmittedFirst(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{anchorUser(), realUser()}}
	fix := &Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{userExpectation()}}

	if got := Evaluate(fix, doc).EntityResults[0].MatchedID; got != "real-user" {
		t.Errorf("bound MatchedID = %q, want %q", got, "real-user")
	}
}

// TestFileNarrowedLookupPrefersTheEarliestOfTwoRealCandidates pins which of two
// REAL colliding candidates wins, which the anchor tests cannot: their buckets
// hold one real entity and one anchor, so "first non-anchor" and "last
// non-anchor" agree and a first/last flip is invisible.
//
// This is a genuine semantics choice, not a no-op. The pre-#6277 byKindNameFile
// was a single map value written once per entity, so a colliding key resolved
// to the LAST one emitted; firstExtracted resolves to the first. First is
// chosen so this path agrees with the unnarrowed byKindName path in
// resolveEntity, which has always taken es[0] — one rule for both lookups
// rather than two that disagree. Reversing firstExtracted's loop fails here and
// nowhere else.
func TestFileNarrowedLookupPrefersTheEarliestOfTwoRealCandidates(t *testing.T) {
	first := realUser()
	first.ID = "first-real"
	second := realUser()
	second.ID = "second-real"
	second.StartLine = 40
	second.EndLine = 55

	doc := &graph.Document{Entities: []graph.Entity{first, second}}
	fix := &Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{userExpectation()}}

	if got := Evaluate(fix, doc).EntityResults[0].MatchedID; got != "first-real" {
		t.Errorf("bound MatchedID = %q, want %q — two real entities share this "+
			"(Kind, Name, SourceFile) key and the earliest-emitted one must win, "+
			"matching what the unnarrowed byKindName path does", got, "first-real")
	}
}

// TestPlaceholderAnchorDoesNotSatisfyQualifiedNameMatch closes the second
// resolution path. ormlink stamps its anchor with a QualifiedName
// ("scope:ormmodel:<file>#<model>", observed in the java-spring-mini graph), so
// an expectation using match_by=qualified_name would otherwise resolve to it
// through a lookup the Kind/Name path never touches.
func TestPlaceholderAnchorDoesNotSatisfyQualifiedNameMatch(t *testing.T) {
	doc := &graph.Document{Entities: []graph.Entity{anchorUser()}}
	fix := &Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{{
		Name:          "User",
		Kind:          "SCOPE.Component",
		QualifiedName: "scope:ormmodel:model/User.java#User",
		MatchBy:       "qualified_name",
		MustExist:     true,
	}}}

	res := Evaluate(fix, doc).EntityResults[0]
	if res.Found || res.MatchedID != "" {
		t.Errorf("qualified_name match bound the anchor (%q) — the placeholder check "+
			"must apply to every resolution path, not only Kind+Name", res.MatchedID)
	}
}

// TestPlaceholderAnchorDoesNotSatisfyNiceToHave keeps the nice-to-have tally
// honest too. Those figures are reported to fixture authors as "capabilities we
// already have"; crediting one to an anchor misreports the same way a must-have
// does, and NiceEntityFound is a separate counter that the must-have tests
// above do not touch.
func TestPlaceholderAnchorDoesNotSatisfyNiceToHave(t *testing.T) {
	ee := userExpectation()
	ee.MustExist = false
	ee.NiceToHave = true
	doc := &graph.Document{Entities: []graph.Entity{anchorUser()}}

	rep := Evaluate(&Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{ee}}, doc)

	if rep.NiceEntityFound != 0 {
		t.Errorf("NiceEntityFound = %d, want 0", rep.NiceEntityFound)
	}
	if rep.NiceEntityTotal != 1 {
		t.Errorf("NiceEntityTotal = %d, want 1", rep.NiceEntityTotal)
	}
}

// TestZeroSpanEntitiesStillSatisfyMustHaves guards the discriminator against
// being widened into "reject anything without a source span".
//
// That rule looks equivalent — both anchors observed in java-spring-mini and
// python-django-mini carry StartLine 0 — and it is wrong in both directions.
// Measured over the graphs the twenty golden fixtures actually produce, it
// would additionally reject 17 must-haves that are synthesised by design and
// legitimately carry no span, across five fixtures: php-slim-mini's five
// http_endpoint_definition entities, rust-tokio-mini's five SCOPE.Component file
// modules, swift-package-mini's three SCOPE.External swiftpm products,
// kotlin-spring-mini's three REST SCOPE.Operations, and java-spring-mini's own
// Route /users. That last one is the sharpest argument against the rule: the
// fixture this whole check exists for carries a legitimate zero-span must-have,
// so the cheap discriminator would have broken it while fixing it.
//
// It also MISSES the anchor in elixir-phoenix-mini, which is stamped with
// StartLine 1 / EndLine 24.
//
// A span is a property of how an entity was derived, not of whether it is real.
// The producer's own declaration is.
func TestZeroSpanEntitiesStillSatisfyMustHaves(t *testing.T) {
	cases := []graph.Entity{
		{ID: "ep", Name: "http:GET:/invoices", Kind: "http_endpoint_definition"},
		{ID: "mod", Name: "store.rs", Kind: "SCOPE.Component", Subtype: "file"},
		{ID: "prod", Name: "Vapor", Kind: "SCOPE.External", Subtype: "swiftpm_product"},
		{ID: "rest", Name: "GET /health", Kind: "SCOPE.Operation", Subtype: "rest"},
		{ID: "route", Name: "/users", Kind: "Route"},
	}
	for _, ent := range cases {
		t.Run(ent.Name, func(t *testing.T) {
			if ent.StartLine != 0 || ent.EndLine != 0 {
				t.Fatalf("test setup: %s must have no source span", ent.Name)
			}
			doc := &graph.Document{Entities: []graph.Entity{ent}}
			fix := &Fixture{Name: "t", ExpectedEntities: []ExpectedEntity{
				{Name: ent.Name, Kind: ent.Kind, MustExist: true},
			}}
			res := Evaluate(fix, doc).EntityResults[0]
			if !res.Found || res.MatchedID != ent.ID {
				t.Errorf("%s %s not satisfied (MatchedID=%q) — a zero source span is "+
					"how synthesised-by-design entities are emitted; it is not evidence "+
					"that an entity is a placeholder", ent.Kind, ent.Name, res.MatchedID)
			}
		})
	}
}

// TestPlaceholderAnchorStillResolvesRelationshipEndpoints scopes the change.
//
// The anchor exists so an embedded MAPS_TO edge has something to hang off
// (ormlink/extractor.go:43-49). Refusing to resolve it as an EDGE ENDPOINT
// would make that edge unassertable, which is a different question from
// "was this entity extracted". No fixture under internal/quality/golden/
// currently asserts a MAPS_TO edge — verified by grep over the twenty
// expected.json files — so this pins intent rather than defending a live case.
//
// This case leaves FromFile empty, so it exercises the unnarrowed byKindName
// lookup. The file-narrowed lookup is covered by the sibling below.
func TestPlaceholderAnchorStillResolvesRelationshipEndpoints(t *testing.T) {
	anchor := anchorUser()
	doc := &graph.Document{
		Entities: []graph.Entity{anchor},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: anchor.ID, ToID: "users", Kind: ormlink.RelMapsTo},
		},
	}
	fix := &Fixture{Name: "t", ExpectedRelationships: []ExpectedRelationship{{
		FromName:   "User",
		FromKind:   "SCOPE.Component",
		Kind:       ormlink.RelMapsTo,
		ToBareName: "users",
		MustExist:  true,
	}}}

	rep := Evaluate(fix, doc)
	if rep.RelFound != 1 {
		t.Errorf("MAPS_TO edge anchored on the placeholder was not matched (RelFound=%d); "+
			"the placeholder check must apply to entity expectations only", rep.RelFound)
	}
}

// TestFileNarrowedEndpointLookupKeepsPreChangeSemantics pins the relationship
// path's file-narrowed lookup, which byKindNameFile's re-keying passes through.
//
// Before #6277 that index was a single map value written once per entity, so a
// colliding (Kind, Name, SourceFile) key resolved to the LAST entity emitted —
// here the anchor, which is exactly what the MAPS_TO edge hangs off. lastOf
// preserves that. Handing resolveExpectedEdge the whole bucket instead would
// let an edge match on a candidate this lookup could never previously return,
// which is a widening of the instrument inside a change that claims only entity
// scoring moves; this test fails if that widening is reintroduced, because the
// real entity would then also become a candidate and the FORBIDDEN edge below
// would start matching.
func TestFileNarrowedEndpointLookupKeepsPreChangeSemantics(t *testing.T) {
	real, anchor := realUser(), anchorUser()
	doc := &graph.Document{
		// The real entity FIRST, the anchor LAST — the emission order in which
		// the two orderings differ.
		Entities: []graph.Entity{real, anchor},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: anchor.ID, ToID: "users", Kind: ormlink.RelMapsTo},
			// An edge that leaves the REAL entity. It is reachable through this
			// lookup only if the bucket is widened.
			{ID: "r2", FromID: real.ID, ToID: "audit_log", Kind: ormlink.RelMapsTo},
		},
	}
	edge := ExpectedRelationship{
		FromName:   "User",
		FromKind:   "SCOPE.Component",
		FromFile:   "model/User.java",
		Kind:       ormlink.RelMapsTo,
		ToBareName: "users",
		MustExist:  true,
	}

	// The anchor is last in the bucket, so the pre-change lookup returns it and
	// the MAPS_TO edge it carries matches.
	if got := Evaluate(&Fixture{Name: "t",
		ExpectedRelationships: []ExpectedRelationship{edge}}, doc).RelFound; got != 1 {
		t.Errorf("RelFound = %d, want 1 — the file-narrowed endpoint lookup must still "+
			"return the anchor the MAPS_TO edge is attached to", got)
	}

	// r2 must stay out of reach. Stated as a forbidden edge so a widening is a
	// hard failure rather than a silently larger match set.
	leaked := edge
	leaked.ToBareName = "audit_log"
	leaked.MustExist = false
	rep := Evaluate(&Fixture{Name: "t",
		ForbiddenRelationships: []ExpectedRelationship{leaked}}, doc)
	if len(rep.ForbiddenHits) != 0 {
		t.Errorf("ForbiddenHits = %d, want 0 — the edge out of %q was matched through a "+
			"file-narrowed lookup that could not previously return that entity; the "+
			"bucket was widened", len(rep.ForbiddenHits), real.ID)
	}
}
