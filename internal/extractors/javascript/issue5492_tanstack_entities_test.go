// issue5492_tanstack_entities_test.go — issue #5492 proving tests.
//
// TanStack/React Query react-adapter hooks (useQuery / useSuspenseQuery /
// useInfiniteQuery / useMutation, plus the older positional useQuery(key, fn)
// form) are extracted as decorated SCOPE.Operation entities (subtype
// tanstack_query | tanstack_mutation) carrying the queryKey/queryFn/mutationFn
// as attributes, with a CONTAINS edge from the enclosing component/hook. The
// pass is import-gated: a non-TanStack call named useQuery (no @tanstack import)
// must NOT fire. The queryKey->endpoint USES edge is the follow-up #5494.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

func TestIssue5492_TanstackEntities(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/TanstackEntities.tsx")

	queries := bySubtype(ents, "SCOPE.Operation", "tanstack_query")
	muts := bySubtype(ents, "SCOPE.Operation", "tanstack_mutation")

	// 4 query-family calls (useQuery x2 [object + positional], useSuspenseQuery,
	// useInfiniteQuery) and 1 mutation.
	if len(queries) != 4 {
		t.Fatalf("expected 4 tanstack_query operations; got %d: %s", len(queries), dumpKinds(ents))
	}
	if len(muts) != 1 {
		t.Fatalf("expected 1 tanstack_mutation operation; got %d: %s", len(muts), dumpKinds(ents))
	}

	// Every emitted op carries the provenance + framework stamps.
	for _, e := range append(append([]types.EntityRecord{}, queries...), muts...) {
		if e.Properties["via"] != "tanstack_query" {
			t.Errorf("%s via = %q, want tanstack_query", e.Name, e.Properties["via"])
		}
		if e.Properties["framework"] != "react" {
			t.Errorf("%s framework = %q, want react", e.Name, e.Properties["framework"])
		}
	}

	// query_kind coverage across the query family.
	kinds := map[string]bool{}
	for _, e := range queries {
		kinds[e.Properties["query_kind"]] = true
	}
	for _, want := range []string{"query", "infinite_query"} {
		if !kinds[want] {
			t.Errorf("missing tanstack_query of kind %q; got %v", want, kinds)
		}
	}

	// Object-arg form: query_key joined + query_fn ref recovered.
	assertProp := func(op *types.EntityRecord, key, want string) {
		if op == nil {
			t.Fatalf("missing operation for %s=%s", key, want)
		}
		if got := op.Properties[key]; got != want {
			t.Errorf("%s %s = %q, want %q", op.Name, key, got, want)
		}
	}

	// Find ops by attribute (line-suffixed names vary), assert attributes.
	var simpleQuery, composite, infinite, positional *types.EntityRecord
	for i := range queries {
		q := &queries[i]
		switch q.Properties["query_key"] {
		case "users":
			simpleQuery = q
		case "user,id":
			composite = q
		case "feed":
			infinite = q
		case "todos":
			positional = q
		}
	}
	assertProp(simpleQuery, "query_fn", "getUsers")
	assertProp(composite, "query_key", "user,id")
	assertProp(composite, "query_fn", "getUser")
	assertProp(infinite, "query_kind", "infinite_query")
	assertProp(infinite, "query_fn", "getFeed")

	// Older positional form: useQuery(['todos'], getTodos).
	assertProp(positional, "query_key", "todos")
	assertProp(positional, "query_fn", "getTodos")
	assertProp(positional, "query_call", "useQuery")

	// Mutation attribute.
	if got := muts[0].Properties["mutation_fn"]; got != "createUser" {
		t.Errorf("mutation_fn = %q, want createUser", got)
	}

	// CONTAINS edge from useUsers hook to its tanstack op.
	var owner *types.EntityRecord
	for i := range ents {
		if ents[i].Name == "useUsers" && (ents[i].Kind == "SCOPE.Operation" || ents[i].Kind == "SCOPE.Component") {
			owner = &ents[i]
			break
		}
	}
	if owner == nil {
		t.Fatalf("missing useUsers owner entity")
	}
	contains := 0
	for _, r := range owner.Relationships {
		if r.Kind == "CONTAINS" && r.Properties["via"] == "tanstack_query" {
			contains++
		}
	}
	if contains == 0 {
		t.Errorf("useUsers has no CONTAINS->tanstack edge")
	}
}

// A locally-defined function named useQuery with no @tanstack import must NOT be
// extracted as a TanStack entity (import gate).
func TestIssue5492_TanstackNegative_NoImport(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/TanstackNegative.tsx")
	if got := bySubtype(ents, "SCOPE.Operation", "tanstack_query"); len(got) != 0 {
		t.Errorf("expected 0 tanstack_query ops without @tanstack import; got %d", len(got))
	}
	if got := bySubtype(ents, "SCOPE.Operation", "tanstack_mutation"); len(got) != 0 {
		t.Errorf("expected 0 tanstack_mutation ops without @tanstack import; got %d", len(got))
	}
}
