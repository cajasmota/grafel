package mcp

import "testing"

// Test_buildServiceCycles_RelationFieldAlias verifies that service-level SCC
// detection (#1502) reads the on-disk links-pass "relation" field (not just the
// MCP-candidate "kind" field) so cross-repo links emitted by internal/links
// surface their direction. The canonical orders↔payments cycle uses
// relation=calls one way and relation=publishes_to the other.
func Test_buildServiceCycles_RelationFieldAlias(t *testing.T) {
	t.Parallel()
	lg := &LoadedGroup{
		Name: "g",
		Links: []CrossRepoLink{
			{Source: "orders::a", Target: "payments::b", Relation: "calls"},
			{Source: "payments::c", Target: "orders::d", Relation: "publishes_to"},
		},
	}
	cycles := buildServiceCycles(lg, nil)
	if len(cycles) != 1 || cycles[0].Size != 2 {
		t.Fatalf("want one size-2 SCC, got %+v", cycles)
	}
	if cycles[0].Members[0] != "orders" || cycles[0].Members[1] != "payments" {
		t.Fatalf("members = %v, want [orders payments]", cycles[0].Members)
	}
}

// Test_buildServiceCycles_SharedLabelExcluded confirms undirected shared_label
// links cannot fabricate a service cycle even as a mutual pair.
func Test_buildServiceCycles_SharedLabelExcluded(t *testing.T) {
	t.Parallel()
	lg := &LoadedGroup{
		Links: []CrossRepoLink{
			{Source: "a::1", Target: "b::2", Relation: "shared_label"},
			{Source: "b::3", Target: "a::4", Relation: "shared_label"},
		},
	}
	if got := buildServiceCycles(lg, nil); len(got) != 0 {
		t.Fatalf("shared_label must not form a cycle, got %+v", got)
	}
}

// Test_buildServiceCycles_RepoFilter restricts the service graph to links whose
// both endpoints fall inside the filter.
func Test_buildServiceCycles_RepoFilter(t *testing.T) {
	t.Parallel()
	lg := &LoadedGroup{
		Links: []CrossRepoLink{
			{Source: "orders::a", Target: "payments::b", Relation: "calls"},
			{Source: "payments::c", Target: "orders::d", Relation: "publishes_to"},
			{Source: "payments::e", Target: "ledger::f", Relation: "publishes_to"},
		},
	}
	// Filter excludes ledger; orders↔payments still cycles, ledger never appears.
	cycles := buildServiceCycles(lg, []string{"orders", "payments"})
	if len(cycles) != 1 || cycles[0].Size != 2 {
		t.Fatalf("want one size-2 SCC, got %+v", cycles)
	}
	for _, m := range cycles[0].Members {
		if m == "ledger" {
			t.Fatalf("ledger leaked into filtered SCC: %v", cycles[0].Members)
		}
	}
}

// Test_buildServiceCycles_KindFieldStillWorks verifies the legacy "kind" field
// (MCP-appended candidates) is still honoured.
func Test_buildServiceCycles_KindFieldStillWorks(t *testing.T) {
	t.Parallel()
	lg := &LoadedGroup{
		Links: []CrossRepoLink{
			{Source: "a::1", Target: "b::2", Kind: "calls"},
			{Source: "b::3", Target: "a::4", Kind: "publishes_to"},
		},
	}
	if got := buildServiceCycles(lg, nil); len(got) != 1 {
		t.Fatalf("kind field must still work, got %+v", got)
	}
}
