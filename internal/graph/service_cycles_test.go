package graph

import (
	"reflect"
	"testing"
)

// Test_FindServiceCycles_TwoNodeCycle covers the canonical orders↔payments
// shape: a directed REST `calls` edge one way and a Kafka `publishes_to` edge
// back, mediated by no shared entity. The two services must form exactly one
// SCC of size 2.
func Test_FindServiceCycles_TwoNodeCycle(t *testing.T) {
	links := []ServiceLink{
		{FromService: "orders", ToService: "payments", Relation: "calls"},
		{FromService: "payments", ToService: "orders", Relation: "publishes_to"},
	}
	got := FindServiceCycles(links)
	if len(got) != 1 {
		t.Fatalf("want 1 SCC, got %d: %+v", len(got), got)
	}
	if got[0].Size != 2 {
		t.Fatalf("want size 2, got %d", got[0].Size)
	}
	want := []string{"orders", "payments"}
	if !reflect.DeepEqual(got[0].Members, want) {
		t.Fatalf("members = %v, want %v", got[0].Members, want)
	}
	if len(got[0].Edges) != 2 {
		t.Fatalf("want 2 internal edges, got %d: %+v", len(got[0].Edges), got[0].Edges)
	}
}

// Test_FindServiceCycles_NoCycle ensures a one-way fan-out produces no SCC.
func Test_FindServiceCycles_NoCycle(t *testing.T) {
	links := []ServiceLink{
		{FromService: "a", ToService: "b", Relation: "calls"},
		{FromService: "a", ToService: "c", Relation: "calls"},
		{FromService: "b", ToService: "c", Relation: "publishes_to"},
	}
	if got := FindServiceCycles(links); len(got) != 0 {
		t.Fatalf("want 0 SCC, got %d: %+v", len(got), got)
	}
}

// Test_FindServiceCycles_OneWaySubscriberNotPulledIn mirrors MANIFEST §11.10:
// `ledger` only consumes payments.settled (payments → ledger) and never calls
// back. It must NOT join the orders↔payments SCC.
func Test_FindServiceCycles_OneWaySubscriberNotPulledIn(t *testing.T) {
	links := []ServiceLink{
		{FromService: "orders", ToService: "payments", Relation: "calls"},
		{FromService: "payments", ToService: "orders", Relation: "publishes_to"},
		{FromService: "payments", ToService: "ledger", Relation: "publishes_to"},
	}
	got := FindServiceCycles(links)
	if len(got) != 1 {
		t.Fatalf("want 1 SCC, got %d: %+v", len(got), got)
	}
	for _, m := range got[0].Members {
		if m == "ledger" {
			t.Fatalf("ledger must not be in the SCC: %v", got[0].Members)
		}
	}
	if !reflect.DeepEqual(got[0].Members, []string{"orders", "payments"}) {
		t.Fatalf("members = %v, want [orders payments]", got[0].Members)
	}
}

// Test_FindServiceCycles_UndirectedRelationsExcluded ensures shared_label /
// string_match co-occurrence links cannot fabricate a cycle even when they form
// a mutual pair.
func Test_FindServiceCycles_UndirectedRelationsExcluded(t *testing.T) {
	links := []ServiceLink{
		{FromService: "x", ToService: "y", Relation: "shared_label"},
		{FromService: "y", ToService: "x", Relation: "shared_label"},
		{FromService: "x", ToService: "y", Relation: "string_match"},
		{FromService: "y", ToService: "x", Relation: "string_match"},
	}
	if got := FindServiceCycles(links); len(got) != 0 {
		t.Fatalf("undirected relations must not form a cycle, got %d: %+v", len(got), got)
	}
}

// Test_FindServiceCycles_ImportsExcluded ensures cross-repo module imports
// (build-time coupling, e.g. two services importing each other's shared lib)
// do NOT form a runtime service cycle.
func Test_FindServiceCycles_ImportsExcluded(t *testing.T) {
	links := []ServiceLink{
		{FromService: "p", ToService: "q", Relation: "imports"},
		{FromService: "q", ToService: "p", Relation: "imports"},
	}
	if got := FindServiceCycles(links); len(got) != 0 {
		t.Fatalf("imports must not form a service cycle, got %d: %+v", len(got), got)
	}
}

// Test_FindServiceCycles_SelfEdgeIgnored confirms a self-loop is not a cycle.
func Test_FindServiceCycles_SelfEdgeIgnored(t *testing.T) {
	links := []ServiceLink{
		{FromService: "solo", ToService: "solo", Relation: "calls"},
	}
	if got := FindServiceCycles(links); len(got) != 0 {
		t.Fatalf("self-edge must not form a cycle, got %d", len(got))
	}
}

// Test_FindServiceCycles_ThreeNodeCycle covers a larger SCC and edge ordering.
func Test_FindServiceCycles_ThreeNodeCycle(t *testing.T) {
	links := []ServiceLink{
		{FromService: "a", ToService: "b", Relation: "calls"},
		{FromService: "b", ToService: "c", Relation: "calls"},
		{FromService: "c", ToService: "a", Relation: "publishes_to"},
	}
	got := FindServiceCycles(links)
	if len(got) != 1 || got[0].Size != 3 {
		t.Fatalf("want one size-3 SCC, got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Members, []string{"a", "b", "c"}) {
		t.Fatalf("members = %v", got[0].Members)
	}
}
