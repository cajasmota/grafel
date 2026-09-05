package quality

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6488 arm C. `from_bare_name` existed only in prose: resolveExpectedEdge
// built its "from" candidates from entity lookups alone, and EVERY match path
// — including the bare-name fallback — was nested inside
// `for _, fc := range fromCands`, so an empty candidate list could not match by
// any route. An edge whose FromID is a raw path (the cross-language IMPORTS
// convention of #120) was therefore unassertable BY CONSTRUCTION, not merely
// unasserted: erlang, nim and groovy emit path-anchored IMPORTS and no
// extractor.FileEntity to carry the path, so nothing in those graphs is
// addressable as the "from" side of an include edge.
//
// MEASURED before this arm was written, on erlang-otp-mini: deleting the
// `-include` IMPORTS edge from internal/extractors/erlang/extractor.go left the
// fixture's report byte-identical (entities 20/20, relationships 25/25,
// forbidden 0, nice 0/2) with the graph's only IMPORTS edge gone. The row that
// names that edge could not fail, because it could not match.
//
// The tests below use in-memory Documents modelled on that fixture:
// `cache_server.erl` is the raw carrier string, `cache.hrl` the include target
// entity, and the edge hangs off the path rather than off any entity ID.

// includeDoc mirrors erlang-otp-mini: an IMPORTS edge whose FromID is a file
// path no entity carries as its ID, pointing at a resolved entity.
func includeDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-mod", Name: "cache_server", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"},
			{ID: "sha-hrl", Name: "cache.hrl", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-inc", FromID: "cache_server.erl", ToID: "sha-hrl", Kind: "IMPORTS"},
		},
	}
}

// includeRow is the assertion the fixture could not previously write: the FROM
// side is the raw carrier string, the TO side a normally-resolved entity.
func includeRow() ExpectedRelationship {
	return ExpectedRelationship{
		FromBareName: "cache_server.erl",
		Kind:         "IMPORTS",
		ToName:       "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
		MustExist: true,
	}
}

func oneRow(rows ...ExpectedRelationship) *Fixture {
	return &Fixture{Name: "erlang-ish", ExpectedRelationships: rows}
}

func TestFromBareNameMatchesAnEdgeWhoseFromIDIsARawPath_6488(t *testing.T) {
	rep := Evaluate(oneRow(includeRow()), includeDoc())
	if rep.RelExpected != 1 || rep.RelFound != 1 {
		t.Fatalf("path-anchored edge must be matchable via from_bare_name: found=%d expected=%d",
			rep.RelFound, rep.RelExpected)
	}
	if got := rep.RelResults[0].MatchedRelID; got != "rel-inc" {
		t.Fatalf("matched the wrong edge: %q", got)
	}
}

// The other half of the pair. The row below names the MODULE ENTITY
// (`cache_server`), which is what a fixture author had to reach for before this
// arm — not the carrier string `cache_server.erl`, which no row could name at
// all. The module entity carries no IMPORTS edge, so the row misses: that is
// the state this arm found, and the reason the erlang include edge could be
// deleted with no fixture going red. If this test ever passes, the bare-name
// path is not what made the test above green.
func TestNamingTheModuleEntityStillCannotMatchAPathAnchoredEdge_6488(t *testing.T) {
	row := ExpectedRelationship{
		FromName: "cache_server", FromKind: "SCOPE.Component", FromFile: "cache_server.erl",
		Kind:   "IMPORTS",
		ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
		MustExist: true,
	}
	rep := Evaluate(oneRow(row), includeDoc())
	if rep.RelFound != 0 {
		t.Fatalf("the module entity carries no IMPORTS edge; this row must still miss")
	}
	// ...and its from endpoint IS resolved, which is the true direction of the
	// from_resolved assertion: an entity was found, the edge was not.
	if !rep.RelResults[0].FromResolved {
		t.Fatalf("from_resolved must be true when the row's from endpoint is an extracted entity")
	}
}

// The "either" semantics the field's doc promises: a row may state BOTH a
// from_name and a from_bare_name, and both are candidates. Without this row the
// promise is prose — narrowing the carrier to "only when the entity lookup came
// up empty" passes every other test in this file, because no other row resolves
// a from entity AND needs the carrier to match.
func TestFromNameAndFromBareNameAreBothCandidates_6488(t *testing.T) {
	row := includeRow()
	row.FromName, row.FromKind, row.FromFile = "cache_server", "SCOPE.Component", "cache_server.erl"

	rep := Evaluate(oneRow(row), includeDoc())
	if rep.RelFound != 1 || rep.RelResults[0].MatchedRelID != "rel-inc" {
		t.Fatalf("a resolving from_name must not shadow the carrier: found=%d id=%q",
			rep.RelFound, rep.RelResults[0].MatchedRelID)
	}
	// ...and the row's from endpoint IS resolved here, because it named an
	// entity that exists. The carrier decided the match; the entity decided
	// from_resolved, and the two answers are independent.
	if !rep.RelResults[0].FromResolved {
		t.Fatalf("from_resolved follows the named entity, which was extracted")
	}
}

// from_resolved, false direction. A row that MATCHED via from_bare_name must
// still report from_resolved:false when the carrier is not an entity — that is
// the whole finding the row records: the edge dangles from a string. Reporting
// true here (e.g. `|| er.FromBareName != ""`) is #6487's defect exactly, and
// recall alone cannot see it, because the row matches either way.
func TestMatchedFromBareNameRowStillReportsFromResolvedFalse_6488(t *testing.T) {
	rep := Evaluate(oneRow(includeRow()), includeDoc())
	rr := rep.RelResults[0]
	if !rr.Found {
		t.Fatalf("precondition: the row must match")
	}
	if rr.FromResolved {
		t.Fatalf("carrier %q is no entity's ID; from_resolved must be false even on a match",
			includeRow().FromBareName)
	}
}

// The bare string that IS an entity's ID resolves — this row can match through
// the ordinary literal-ID path, so calling it unresolved would be the tightening
// mutant of the test above. It is the only disjunct of from_resolved that can
// decide this input: the row names no from entity, so the candidate lookup is
// empty.
func TestFromBareNameEqualToAnEntityIDIsResolved_6488(t *testing.T) {
	row := ExpectedRelationship{
		FromBareName: "sha-mod", Kind: "IMPORTS",
		ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
		MustExist: true,
	}
	rep := Evaluate(oneRow(row), includeDoc())
	if !rep.RelResults[0].FromResolved {
		t.Fatalf("a bare name equal to an entity ID resolves")
	}
	if rep.RelResults[0].Found {
		t.Fatalf("no edge leaves sha-mod; resolving the endpoint must not invent a match")
	}
}

// Deliberate asymmetry with the TO side, pinned so a future change is a red
// test rather than a discovery. to_bare_name classifies a string equal to an
// entity's NAME as resolved and flags the row (#6476). The from side does not:
// it carries no such flag, so calling the endpoint resolved would suppress the
// "from-entity not extracted" diagnostic and route the row to "both endpoints
// exist; edge not emitted" — naming the extractor for a row that cannot match.
// Unresolved keeps the reader pointed at an endpoint, which is where the repair
// is.
func TestFromBareNameEqualToAnEntityNameIsNotResolved_6488(t *testing.T) {
	row := ExpectedRelationship{
		FromBareName: "cache_server", Kind: "IMPORTS",
		ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
		MustExist: true,
	}
	rep := Evaluate(oneRow(row), includeDoc())
	if rep.RelResults[0].FromResolved {
		t.Fatalf("an entity NAME is not a carrier ID; from_resolved must be false")
	}
}

// Over-firing control, and the axis recall structurally cannot grade. A
// forbidden row whose from_bare_name matches nothing must report ZERO hits —
// and the positive control in the same test proves the zero is a decision, not
// a path that never fires: the identical row with the real carrier DOES hit.
func TestForbiddenFromBareNameFiresOnlyOnTheRealCarrier_6488(t *testing.T) {
	fires := &Fixture{Name: "erlang-ish", ForbiddenRelationships: []ExpectedRelationship{{
		FromBareName: "cache_server.erl", Kind: "IMPORTS",
		ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
	}}}
	if got := len(Evaluate(fires, includeDoc()).ForbiddenHits); got != 1 {
		t.Fatalf("positive control: the forbidden row over the real carrier must fire, got %d hits", got)
	}
	// A fired row has to say WHICH carrier fired, in both reports — the same
	// naming fallback the missing-row path needs.
	if out := humanReport(t, fires, includeDoc()); !strings.Contains(out, "cache_server.erl --[IMPORTS]-->") {
		t.Fatalf("forbidden-hit report must name the bare carrier:\n%s", out)
	}
	raw, err := json.Marshal(Evaluate(fires, includeDoc()).ToJSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var jr struct {
		Forbidden []struct {
			From string `json:"from"`
		} `json:"forbidden"`
	}
	if err := json.Unmarshal(raw, &jr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(jr.Forbidden) != 1 || jr.Forbidden[0].From != "cache_server.erl" {
		t.Fatalf("machine report must name the bare carrier of a forbidden hit: %s", raw)
	}

	misses := &Fixture{Name: "erlang-ish", ForbiddenRelationships: []ExpectedRelationship{{
		FromBareName: "no_such_module.erl", Kind: "IMPORTS",
		ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
	}}}
	if got := len(Evaluate(misses, includeDoc()).ForbiddenHits); got != 0 {
		t.Fatalf("a from_bare_name matching no carrier must report zero hits, got %d", got)
	}
}

// The kind is still part of the match. A carrier string is not a wildcard.
func TestFromBareNameDoesNotMatchAcrossKinds_6488(t *testing.T) {
	row := includeRow()
	row.Kind = "CALLS"
	rep := Evaluate(oneRow(row), includeDoc())
	if rep.RelFound != 0 {
		t.Fatalf("from_bare_name must not match an edge of another kind")
	}
}

// A bare from reaches the to_bare_name paths too, not only the resolved-target
// one: both endpoints of an include edge can be raw strings before the resolver
// binds either. Second sub-case grades the whitespace/case-insensitive TO
// comparison under a bare FROM, which no earlier test could reach.
func TestFromBareNameCombinesWithToBareName_6488(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "sha-mod", Name: "cache_server", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"},
		},
		Relationships: []graph.Relationship{
			{ID: "rel-literal", FromID: "cache_server.erl", ToID: "cache.hrl", Kind: "IMPORTS"},
			{ID: "rel-mangled", FromID: "cache_sup.erl", ToID: " Cache.HRL ", Kind: "IMPORTS"},
		},
	}
	rep := Evaluate(oneRow(
		ExpectedRelationship{FromBareName: "cache_server.erl", Kind: "IMPORTS",
			ToBareName: "cache.hrl", MustExist: true},
		ExpectedRelationship{FromBareName: "cache_sup.erl", Kind: "IMPORTS",
			ToBareName: "cache.hrl", MustExist: true},
	), doc)
	if rep.RelFound != 2 {
		t.Fatalf("both bare-to paths must be reachable from a bare FROM: found=%d/2 (%q, %q)",
			rep.RelFound, rep.RelResults[0].MatchedRelID, rep.RelResults[1].MatchedRelID)
	}
}

// The fixture's value is trimmed — the TO side has trimmed since #6476 and a
// row should not fail on a stray space — but it is NOT folded. Case-folding the
// FROM side would be a widening no test demanded: the match is a literal
// lookup on the edge's FromID, and a path is not case-insensitive just because
// one filesystem is.
func TestFromBareNameIsTrimmedAndNotFolded_6488(t *testing.T) {
	padded := includeRow()
	padded.FromBareName = "  cache_server.erl  "
	if Evaluate(oneRow(padded), includeDoc()).RelFound != 1 {
		t.Fatalf("a padded from_bare_name must still match")
	}

	folded := includeRow()
	folded.FromBareName = "CACHE_SERVER.ERL"
	if Evaluate(oneRow(folded), includeDoc()).RelFound != 0 {
		t.Fatalf("from_bare_name is compared exactly; a case-folded carrier must not match")
	}
}

// A blank bare name is not a candidate. Without the emptiness guard, "" joins
// the from candidates on EVERY row — including the overwhelming majority that
// set no from_bare_name at all — and any edge the indexer emitted with an empty
// FromID would satisfy an unrelated expectation.
func TestBlankFromBareNameIsNotACandidate_6488(t *testing.T) {
	doc := &graph.Document{
		Entities:      []graph.Entity{{ID: "sha-hrl", Name: "cache.hrl", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"}},
		Relationships: []graph.Relationship{{ID: "rel-orphan", FromID: "", ToID: "sha-hrl", Kind: "IMPORTS"}},
	}
	for _, bare := range []string{"", "   "} {
		row := ExpectedRelationship{
			FromBareName: bare, Kind: "IMPORTS",
			ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
			MustExist: true,
		}
		rep := Evaluate(oneRow(row), doc)
		if rep.RelFound != 0 {
			t.Fatalf("from_bare_name %q must match no edge, not the empty-FromID one", bare)
		}
		if rep.RelResults[0].FromResolved {
			t.Fatalf("from_bare_name %q resolves nothing", bare)
		}
	}
}

// A missed bare-from row must not be blamed on a missing from-entity: the row
// names no entity, so "from-entity not extracted" is true of every such row on
// every miss and identifies nothing. The edge is what is absent.
func TestMissedFromBareNameRowIsNotBlamedOnAMissingFromEntity_6488(t *testing.T) {
	out := humanReport(t, oneRow(includeRow()), &graph.Document{Entities: includeDoc().Entities})
	if strings.Contains(out, "from-entity not extracted") || strings.Contains(out, "NEITHER endpoint") {
		t.Fatalf("a raw carrier is not a missing entity:\n%s", out)
	}
	if !strings.Contains(out, "raw carrier \"cache_server.erl\"") {
		t.Fatalf("diagnostic must name the carrier the edge should have left:\n%s", out)
	}
	// The carrier is printed as the matcher read it — trimmed. A padded row
	// already matches (the matcher trims); a diagnostic quoting the padding
	// would show the reader a string the graph is not being searched for.
	padded := includeRow()
	padded.FromBareName = "  cache_server.erl  "
	out = humanReport(t, oneRow(padded), &graph.Document{Entities: includeDoc().Entities})
	if !strings.Contains(out, "raw carrier \"cache_server.erl\"") {
		t.Fatalf("diagnostic must quote the trimmed carrier:\n%s", out)
	}
}

// ...and the guard on that arm is not decorative. A from_bare_name that IS an
// entity's ID resolves, so the ordinary "both endpoints exist" diagnosis is the
// correct one and must survive.
func TestFromBareNameEqualToAnEntityIDKeepsTheOrdinaryDiagnostic_6488(t *testing.T) {
	row := includeRow()
	row.FromBareName = "sha-mod"
	out := humanReport(t, oneRow(row), includeDoc())
	if !strings.Contains(out, "both endpoints exist") {
		t.Fatalf("a resolved carrier keeps the extractor diagnosis:\n%s", out)
	}
}

// A row that ALSO mistyped its from_file keeps the wrong-path diagnosis: that
// is a concrete authoring error with a concrete repair, and it outranks every
// other arm (#6464).
func TestWrongFromFileOutranksTheBareCarrierArm_6488(t *testing.T) {
	row := includeRow()
	row.FromName, row.FromKind, row.FromFile = "cache_server", "SCOPE.Component", "nowhere.erl"
	out := humanReport(t, oneRow(row), &graph.Document{Entities: includeDoc().Entities})
	if !strings.Contains(out, "names a path no such entity is under") {
		t.Fatalf("the mistyped path is the first thing to fix:\n%s", out)
	}
	// A row that states both names the ENTITY it named: from_name is the more
	// specific claim, and a report that renamed such a row to its carrier would
	// stop matching the row the author wrote.
	if !strings.Contains(out, "cache_server --[IMPORTS]-->") {
		t.Fatalf("from_name wins the label when the row states one:\n%s", out)
	}
}

// A carrier that differs from an entity's ID only in case does NOT resolve.
// The classification has to agree with the matcher: the match below is a
// literal lookup, so a folded "yes" here would report an endpoint as resolved
// that no row could ever match through — the over-claim from_resolved exists to
// avoid, one axis over from #6487's.
func TestFromBareNameResolutionIsNotCaseFolded_6488(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "Cache_Server.erl", Name: "cache_server", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"},
			{ID: "sha-hrl", Name: "cache.hrl", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"},
		},
	}
	row := ExpectedRelationship{
		FromBareName: "cache_server.erl", Kind: "IMPORTS",
		ToName: "cache.hrl", ToKind: "SCOPE.Component", ToFile: "cache_server.erl",
		MustExist: true,
	}
	rep := Evaluate(oneRow(row), doc)
	if rep.RelResults[0].FromResolved {
		t.Fatalf("an ID differing in case is not this row's carrier; from_resolved must be false")
	}
	// Positive control: the exactly-cased carrier over the same graph DOES
	// resolve, so the false above is a case decision and not a dead lookup.
	exact := row
	exact.FromBareName = "Cache_Server.erl"
	if !Evaluate(oneRow(exact), doc).RelResults[0].FromResolved {
		t.Fatalf("the exactly-cased carrier must resolve")
	}
}

// The reporter must name the row it is talking about. A bare-from row has no
// from_name, so both outputs printed an empty left-hand side — the row read as
// ` --[IMPORTS]--> cache.hrl`, which identifies nothing in a fixture with three
// modules. The TO side has had this fallback since it was written.
func TestMissedFromBareNameRowIsNamedInBothReports_6488(t *testing.T) {
	empty := &graph.Document{Entities: includeDoc().Entities}
	fix := oneRow(includeRow())

	human := humanReport(t, fix, empty)
	if !strings.Contains(human, "cache_server.erl --[IMPORTS]-->") {
		t.Fatalf("human report must name the bare carrier:\n%s", human)
	}

	raw, err := json.Marshal(Evaluate(fix, empty).ToJSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var jr struct {
		Missing []struct {
			From         string `json:"from"`
			FromResolved bool   `json:"from_resolved"`
		} `json:"missing_relationships"`
	}
	if err := json.Unmarshal(raw, &jr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(jr.Missing) != 1 {
		t.Fatalf("expected one missing row, got %d: %s", len(jr.Missing), raw)
	}
	if jr.Missing[0].From != "cache_server.erl" {
		t.Fatalf("machine report must name the bare carrier, got %q", jr.Missing[0].From)
	}
	if jr.Missing[0].FromResolved {
		t.Fatalf("machine report must serialise from_resolved:false for an absent carrier: %s", raw)
	}
}
