package external

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

func excType(id, typeName string) graph.Entity {
	return graph.Entity{
		ID:         id,
		Name:       "exception:" + typeName,
		Kind:       string(types.EntityKindExceptionType),
		SourceFile: "<exception>",
	}.WithProperties(map[string]string{"exception_type": typeName})
}

func realClass(id, name string) graph.Entity {
	return graph.Entity{ID: id, Name: name, Kind: string(types.EntityKindClass), SourceFile: "x.ts"}
}

func throwsRel(from, to string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(from, to, string(types.RelationshipKindThrows)),
		FromID: from, ToID: to, Kind: string(types.RelationshipKindThrows),
	}
}

func hasEntity(doc *graph.Document, id string) bool {
	for i := range doc.Entities {
		if doc.Entities[i].ID == id {
			return true
		}
	}
	return false
}

func throwsTo(doc *graph.Document) string {
	for _, r := range doc.Relationships {
		if r.Kind == string(types.RelationshipKindThrows) {
			return r.ToID
		}
	}
	return ""
}

// Retargets to the unique real class and drops the synthetic node.
func TestResolveExceptionTypes_RetargetAndDrop(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn", Name: "f", Kind: string(types.EntityKindFunction)},
			realClass("cls", "MyError"),
			excType("exc", "MyError"),
		},
		Relationships: []graph.Relationship{throwsRel("fn", "exc")},
	}
	st := ResolveExceptionTypes(doc)
	if st.Retargeted != 1 || st.SyntheticDropped != 1 || st.SyntheticKept != 0 {
		t.Fatalf("stats: %+v", st)
	}
	if hasEntity(doc, "exc") {
		t.Fatal("synthetic node should be dropped")
	}
	if throwsTo(doc) != "cls" {
		t.Fatalf("THROWS should target real class, got %s", throwsTo(doc))
	}
}

// No real entity → keep the single synthetic node.
func TestResolveExceptionTypes_KeepWhenUnresolvable(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn", Name: "f", Kind: string(types.EntityKindFunction)},
			excType("exc", "ThirdPartyError"),
		},
		Relationships: []graph.Relationship{throwsRel("fn", "exc")},
	}
	st := ResolveExceptionTypes(doc)
	if st.Retargeted != 0 || st.SyntheticDropped != 0 || st.SyntheticKept != 1 {
		t.Fatalf("stats: %+v", st)
	}
	if !hasEntity(doc, "exc") {
		t.Fatal("synthetic node should be kept")
	}
	if throwsTo(doc) != "exc" {
		t.Fatalf("THROWS should stay on synthetic, got %s", throwsTo(doc))
	}
}

// Ambiguous leaf name (two real classes) → keep synthetic (precision).
func TestResolveExceptionTypes_AmbiguousKept(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "fn", Name: "f", Kind: string(types.EntityKindFunction)},
			realClass("cls1", "Dup"),
			realClass("cls2", "Dup"),
			excType("exc", "Dup"),
		},
		Relationships: []graph.Relationship{throwsRel("fn", "exc")},
	}
	st := ResolveExceptionTypes(doc)
	if st.SyntheticKept != 1 || st.SyntheticDropped != 0 {
		t.Fatalf("ambiguous should keep synthetic: %+v", st)
	}
	if throwsTo(doc) != "exc" {
		t.Fatalf("THROWS should stay on synthetic, got %s", throwsTo(doc))
	}
}

// CATCHES edges are retargeted too, and the pass is idempotent.
func TestResolveExceptionTypes_CatchesAndIdempotent(t *testing.T) {
	catch := graph.Relationship{
		ID:     graph.RelationshipID("h", "exc", string(types.RelationshipKindCatches)),
		FromID: "h", ToID: "exc", Kind: string(types.RelationshipKindCatches),
	}
	doc := &graph.Document{
		Entities: []graph.Entity{
			realClass("cls", "E"),
			excType("exc", "E"),
		},
		Relationships: []graph.Relationship{throwsRel("t", "exc"), catch},
	}
	st := ResolveExceptionTypes(doc)
	if st.Retargeted != 2 || st.SyntheticDropped != 1 {
		t.Fatalf("want 2 retargeted (throws+catches): %+v", st)
	}
	for _, r := range doc.Relationships {
		if r.ToID != "cls" {
			t.Fatalf("edge %s not retargeted: to=%s", r.Kind, r.ToID)
		}
	}
	// Second run is a no-op (synthetic already gone).
	st2 := ResolveExceptionTypes(doc)
	if st2.Retargeted != 0 || st2.SyntheticDropped != 0 {
		t.Fatalf("idempotency: second run mutated: %+v", st2)
	}
}

// SCOPE.Component (declared-type fallback some extractors use) is a candidate.
func TestResolveExceptionTypes_ComponentCandidate(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "comp", Name: "CompError", Kind: string(types.EntityKindComponent), SourceFile: "a.ts"},
			excType("exc", "CompError"),
		},
		Relationships: []graph.Relationship{throwsRel("t", "exc")},
	}
	st := ResolveExceptionTypes(doc)
	if st.Retargeted != 1 || st.SyntheticDropped != 1 {
		t.Fatalf("component candidate: %+v", st)
	}
	if throwsTo(doc) != "comp" {
		t.Fatalf("THROWS should target component, got %s", throwsTo(doc))
	}
}

func callsRel(from, to string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(from, to, string(types.RelationshipKindCalls)),
		FromID: from, ToID: to, Kind: string(types.RelationshipKindCalls),
	}
}

func callsTo(doc *graph.Document, from string) []string {
	var out []string
	for _, r := range doc.Relationships {
		if r.Kind == string(types.RelationshipKindCalls) && r.FromID == from {
			out = append(out, r.ToID)
		}
	}
	return out
}

// #4555 — the live NotFoundException shape: a synthetic ExceptionType node with
// THROWS on it, plus a DANGLING `new NotFoundException()` constructor CALLS edge
// (bare-name target, no entity) from the SAME throwing method. No real class
// entity exists, so the exception node is KEPT — and the constructor CALLS must
// be folded onto it so ONE node carries both throws + calls.
func TestResolveExceptionTypes_4555_UnifiesDanglingConstructorCall(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "method", Name: "remove", Kind: string(types.EntityKindFunction), SourceFile: "repo.ts"},
			excType("exc", "NotFoundException"),
		},
		Relationships: []graph.Relationship{
			throwsRel("method", "exc"),
			callsRel("method", "NotFoundException"), // dangling bare-name ctor call
		},
	}
	// RED before: two nodes — exception node + phantom dangling CALLS target.
	if got := callsTo(doc, "method"); len(got) != 1 || got[0] != "NotFoundException" {
		t.Fatalf("setup: want dangling CALLS to bare name, got %v", got)
	}

	st := ResolveExceptionTypes(doc)

	if st.ConstructorCallsUnified != 1 {
		t.Fatalf("want 1 constructor call unified, got %+v", st)
	}
	// Exception node KEPT (no real class to resolve to).
	if !hasEntity(doc, "exc") {
		t.Fatal("exception node must be kept (no real class)")
	}
	// GREEN: both THROWS and the construction CALLS now target the ONE node.
	if throwsTo(doc) != "exc" {
		t.Fatalf("THROWS must target exception node, got %s", throwsTo(doc))
	}
	got := callsTo(doc, "method")
	if len(got) != 1 || got[0] != "exc" {
		t.Fatalf("construction CALLS must be folded onto exception node, got %v", got)
	}
}

// #4555 — when the exception DOES resolve to a real class, both the THROWS and
// the dangling constructor CALLS fold onto that real class (single node).
func TestResolveExceptionTypes_4555_UnifiesOntoRealClass(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "method", Name: "findById", Kind: string(types.EntityKindFunction), SourceFile: "svc.ts"},
			realClass("cls", "AppNotFoundError"),
			excType("exc", "AppNotFoundError"),
		},
		Relationships: []graph.Relationship{
			throwsRel("method", "exc"),
			callsRel("method", "AppNotFoundError"),
		},
	}
	st := ResolveExceptionTypes(doc)
	if st.SyntheticDropped != 1 || st.Retargeted != 1 || st.ConstructorCallsUnified != 1 {
		t.Fatalf("stats: %+v", st)
	}
	if hasEntity(doc, "exc") {
		t.Fatal("synthetic node must be dropped")
	}
	if throwsTo(doc) != "cls" {
		t.Fatalf("THROWS must target real class, got %s", throwsTo(doc))
	}
	got := callsTo(doc, "method")
	if len(got) != 1 || got[0] != "cls" {
		t.Fatalf("CALLS must fold onto real class, got %v", got)
	}
}

// #4555 precision — a same-named construction from a method that does NOT throw
// the exception is left untouched (no spurious fold).
func TestResolveExceptionTypes_4555_NonThrowingCallerNotFolded(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "thrower", Name: "a", Kind: string(types.EntityKindFunction), SourceFile: "a.ts"},
			{ID: "other", Name: "b", Kind: string(types.EntityKindFunction), SourceFile: "b.ts"},
			excType("exc", "NotFoundException"),
		},
		Relationships: []graph.Relationship{
			throwsRel("thrower", "exc"),
			callsRel("other", "NotFoundException"), // different method, not a thrower
		},
	}
	st := ResolveExceptionTypes(doc)
	if st.ConstructorCallsUnified != 0 {
		t.Fatalf("non-throwing caller must not be folded, got %+v", st)
	}
	if got := callsTo(doc, "other"); len(got) != 1 || got[0] != "NotFoundException" {
		t.Fatalf("non-thrower CALLS must be untouched, got %v", got)
	}
}

func TestResolveExceptionTypes_NilSafe(t *testing.T) {
	if st := ResolveExceptionTypes(nil); st.Retargeted != 0 {
		t.Fatal("nil doc must be safe")
	}
	if st := ResolveExceptionTypes(&graph.Document{}); st.Retargeted != 0 {
		t.Fatal("empty doc must be safe")
	}
}
