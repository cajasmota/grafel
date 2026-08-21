package quality

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// #6464 — the grader's file narrowing fell back to an UNNARROWED lookup.
//
// Evaluate narrowed entity/edge resolution by (kind, name, source_file) and
// then, when that lookup produced nothing, retried without the file. The
// retry was not gated on whether the fixture row had specified a file at
// all. A row that NAMES a file and misses it was therefore answered by a
// same-named entity in a DIFFERENT file, silently.
//
// The consequence is not academic: 47 rows across erlang-otp-mini and
// haskell-warp-mini named `src/`-prefixed paths that resolve to nothing
// under the indexed root (the fixture source root is <fixture>/src, so
// paths are relative to THAT), and every one of them was rescued by the
// fallback on every run. Four were actively wrong-bound — erlang asserts
// `init` for both cache_server.erl and cache_sup.erl, and both landed in
// the same unnarrowed bucket, so deleting either module's `init` left the
// survivor satisfying both rows.
//
// The fix is narrow: a row that OMITS source_file must keep reaching the
// unnarrowed lookup (5 of 423 entity rows do), but a row that SPECIFIES a
// file and misses must resolve to nothing.

// twoFileDoc has the same (kind, name) pair in two different files — the
// shape that makes the fallback observable.
func twoFileDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "srv", Name: "cache_server", Kind: "SCOPE.Component", SourceFile: "cache_server.erl"},
			{ID: "srv_init", Name: "init", Kind: "SCOPE.Operation", SourceFile: "cache_server.erl"},
			{ID: "sup", Name: "cache_sup", Kind: "SCOPE.Component", SourceFile: "cache_sup.erl"},
			{ID: "sup_init", Name: "init", Kind: "SCOPE.Operation", SourceFile: "cache_sup.erl"},
			{ID: "lonely", Name: "flush", Kind: "SCOPE.Operation", SourceFile: "cache_server.erl"},
		},
		Relationships: []graph.Relationship{
			{ID: "r1", FromID: "sup", ToID: "sup_init", Kind: "CONTAINS"},
			{ID: "r2", FromID: "srv", ToID: "srv_init", Kind: "CONTAINS"},
		},
	}
}

// A row that names a file it does not match must resolve to NOTHING, even
// though a same-named entity exists in another file.
func TestSpecifiedSourceFileMissDoesNotFallBackToAnotherFile(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedEntities: []ExpectedEntity{
			// cache_sup.erl really does define init — but this row names a
			// path that resolves to nothing under the indexed root.
			{Name: "init", Kind: "SCOPE.Operation", SourceFile: "src/cache_sup.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.EntityFound != 0 {
		t.Fatalf("EntityFound=%d want 0: a row naming src/cache_sup.erl must NOT be "+
			"answered by init in cache_server.erl (matched %q)",
			rep.EntityFound, rep.EntityResults[0].MatchedID)
	}
}

// The same, with only ONE candidate anywhere in the graph: the fallback
// used to hand back the sole same-named entity regardless of its file.
func TestSpecifiedSourceFileMissDoesNotFallBackToTheSoleCandidate(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedEntities: []ExpectedEntity{
			{Name: "flush", Kind: "SCOPE.Operation", SourceFile: "src/cache_server.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.EntityFound != 0 {
		t.Fatalf("EntityFound=%d want 0: sole-candidate fallback still ignored the "+
			"file the row named (matched %q)", rep.EntityFound, rep.EntityResults[0].MatchedID)
	}
}

// The narrowing must still HIT when the row names the right file — the fix
// is a gate on the fallback, not a removal of the narrowed lookup.
func TestSpecifiedSourceFileStillResolvesWhenCorrect(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedEntities: []ExpectedEntity{
			{Name: "init", Kind: "SCOPE.Operation", SourceFile: "cache_sup.erl", MustExist: true},
			{Name: "init", Kind: "SCOPE.Operation", SourceFile: "cache_server.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.EntityFound != 2 {
		t.Fatalf("EntityFound=%d want 2 (both correct rows must still hit)", rep.EntityFound)
	}
	if got := rep.EntityResults[0].MatchedID; got != "sup_init" {
		t.Errorf("cache_sup.erl row matched %q want sup_init", got)
	}
	if got := rep.EntityResults[1].MatchedID; got != "srv_init" {
		t.Errorf("cache_server.erl row matched %q want srv_init", got)
	}
}

// A row that OMITS source_file must keep reaching the unnarrowed lookup.
// Only 5 of 423 entity rows do this, but the path is not dead and the fix
// must not delete it.
func TestOmittedSourceFileStillReachesTheUnnarrowedLookup(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedEntities: []ExpectedEntity{
			{Name: "cache_sup", Kind: "SCOPE.Component", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.EntityFound != 1 {
		t.Fatalf("EntityFound=%d want 1: a row with no source_file must still resolve "+
			"by (kind, name)", rep.EntityFound)
	}
}

// The regression this exists to make observable: re-injecting a `src/`
// prefix on every row of an erlang-shaped fixture must collapse recall to
// zero. Under the shipped grader it reported full recall.
func TestReInjectedSrcPrefixCollapsesRecallInsteadOfPassing(t *testing.T) {
	rows := []string{"cache_server", "cache_sup"}
	var good, bad []ExpectedEntity
	for _, n := range rows {
		good = append(good, ExpectedEntity{Name: n, Kind: "SCOPE.Component", SourceFile: n + ".erl", MustExist: true})
		bad = append(bad, ExpectedEntity{Name: n, Kind: "SCOPE.Component", SourceFile: "src/" + n + ".erl", MustExist: true})
	}
	doc := twoFileDoc()
	if got := Evaluate(&Fixture{Name: "ok", ExpectedEntities: good}, doc).EntityFound; got != len(rows) {
		t.Fatalf("corrected paths: EntityFound=%d want %d", got, len(rows))
	}
	if got := Evaluate(&Fixture{Name: "wrong", ExpectedEntities: bad}, doc).EntityFound; got != 0 {
		t.Fatalf("src/-prefixed paths: EntityFound=%d want 0 — a wrong file must FAIL, "+
			"not silently pass", got)
	}
}

// The match_by: qualified_name path is deliberately NOT file-gated. MatchBy
// explicitly selects the identity key, and byQName is a globally unique index
// — a row saying "match this by qualified_name" has already said which field
// decides. No fixture row uses match_by today (0 of 423), so this is pinned
// as intent rather than measured on a live population: the fix in #6464
// applies to the (kind, name) narrowing, not to the qualified-name lookup.
func TestQualifiedNamePathIsNotFileGated(t *testing.T) {
	doc := twoFileDoc()
	doc.Entities = append(doc.Entities, graph.Entity{
		ID: "qn", Name: "handle_call", Kind: "SCOPE.Operation",
		QualifiedName: "cache_server:handle_call/3", SourceFile: "cache_server.erl",
	})
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedEntities: []ExpectedEntity{
			{Name: "handle_call", Kind: "SCOPE.Operation", MatchBy: "qualified_name",
				QualifiedName: "cache_server:handle_call/3",
				SourceFile:    "src/cache_server.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, doc)
	if rep.EntityFound != 1 || rep.EntityResults[0].MatchedID != "qn" {
		t.Fatalf("EntityFound=%d matched=%q — match_by=qualified_name must resolve by "+
			"qualified_name regardless of source_file",
			rep.EntityFound, rep.EntityResults[0].MatchedID)
	}
}

// ---- edge endpoints: from_file (diff.go resolveExpectedEdge) ----

func TestEdgeFromFileMissDoesNotFallBackToAnotherFile(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedRelationships: []ExpectedRelationship{
			// CONTAINS cache_sup -> init exists; but no edge exists from any
			// entity in a file called src/cache_sup.erl, because no such file
			// is in the graph.
			{FromName: "init", FromKind: "SCOPE.Operation", FromFile: "src/cache_sup.erl",
				Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation",
				ToFile: "cache_sup.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelResults[0].FromResolved {
		t.Fatalf("from_file named src/cache_sup.erl and missed, yet FromResolved=true "+
			"— the unnarrowed fallback answered with another file's entity: %+v",
			rep.RelResults[0])
	}
	if rep.RelFound != 0 {
		t.Fatalf("RelFound=%d want 0", rep.RelFound)
	}
}

func TestEdgeToFileMissDoesNotFallBackToAnotherFile(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedRelationships: []ExpectedRelationship{
			// The CONTAINS cache_sup -> sup_init edge is real. Naming a
			// to_file that resolves to nothing must not be rescued by
			// srv_init in the other file.
			{FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
				Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation",
				ToFile: "src/cache_sup.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelResults[0].ToResolved {
		t.Fatalf("to_file named src/cache_sup.erl and missed, yet ToResolved=true: %+v",
			rep.RelResults[0])
	}
	if rep.RelFound != 0 {
		t.Fatalf("RelFound=%d want 0 — the edge must MISS when to_file names a file "+
			"that resolves to nothing", rep.RelFound)
	}
}

// Correct files on both endpoints must still match.
func TestEdgeWithCorrectFilesStillResolves(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
				Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation",
				ToFile: "cache_sup.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelFound != 1 {
		t.Fatalf("RelFound=%d want 1, results=%+v", rep.RelFound, rep.RelResults)
	}
}

// Edge rows that OMIT the file must keep reaching the unnarrowed lookup.
func TestEdgeWithoutFilesStillReachesTheUnnarrowedLookup(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "cache_sup", FromKind: "SCOPE.Component",
				Kind: "CONTAINS", ToName: "init", ToKind: "SCOPE.Operation", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelFound != 1 {
		t.Fatalf("RelFound=%d want 1 — a row with no from_file/to_file must still "+
			"resolve by (kind, name)", rep.RelFound)
	}
}

// ---- the kind-blank whole-graph scan (diff.go :290-297, :306-311) ----
//
// KEPT deliberately. It is a distinct authoring affordance ("kind left
// blank means any kind"), reachable today by 3 to_kind-blank rows, and
// removing it is a separate behavioural change with its own blast radius —
// not something #6464 should absorb silently. What it must NOT do is
// re-widen the file gate, so it now sits behind the same gate.

func TestKindBlankScanStillWorksWhenNoFileIsNamed(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "cache_sup", FromKind: "SCOPE.Component",
				Kind: "CONTAINS", ToName: "init", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelFound != 1 {
		t.Fatalf("RelFound=%d want 1 — the kind-blank scan is retained for rows that "+
			"name no file", rep.RelFound)
	}
}

func TestKindBlankScanDoesNotRescueAWrongFile(t *testing.T) {
	fix := &Fixture{
		Name: "erlang-shaped",
		ExpectedRelationships: []ExpectedRelationship{
			{FromName: "cache_sup", FromKind: "SCOPE.Component", FromFile: "cache_sup.erl",
				Kind: "CONTAINS", ToName: "init", ToFile: "src/cache_sup.erl", MustExist: true},
		},
	}
	rep := Evaluate(fix, twoFileDoc())
	if rep.RelResults[0].ToResolved {
		t.Fatalf("the kind-blank whole-graph scan re-widened past a specified-and-missed "+
			"to_file: %+v", rep.RelResults[0])
	}
	if rep.RelFound != 0 {
		t.Fatalf("RelFound=%d want 0", rep.RelFound)
	}
}
