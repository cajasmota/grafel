package ingest

import (
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/types"
)

func codeEntitiesFixture(repoTag string) []graph.Entity {
	mk := func(kind, name, qn, src string) graph.Entity {
		return graph.Entity{
			ID:            graph.EntityID(repoTag, kind, name, src),
			Name:          name,
			QualifiedName: qn,
			Kind:          kind,
			SourceFile:    src,
		}
	}
	return []graph.Entity{
		mk(string(types.EntityKindClass), "OrderService", "orders/order.OrderService", "orders/order.go"),
		mk(string(types.EntityKindFunction), "placeOrder", "orders/order.OrderService.placeOrder", "orders/order.go"),
		mk(string(types.EntityKindFunction), "validateOrder", "orders/order.validateOrder", "orders/order.go"),
	}
}

func TestDiscoverMarkdown_ExcludesVendored(t *testing.T) {
	in := []string{
		"docs/orders.md",
		"README.markdown",
		"node_modules/dep.md",
		"vendor/x/y.md",
		"src/main.go",
		"dist/bundle.md",
	}
	got := DiscoverMarkdown(in)
	want := map[string]bool{"docs/orders.md": true, "README.markdown": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want keys %v", got, want)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected discovered path %q", g)
		}
	}
}

func TestIngest_EmitsDocSectionAndMentionEdges(t *testing.T) {
	repoRoot, err := filepath.Abs("testdata/repo")
	if err != nil {
		t.Fatal(err)
	}
	repoTag := "repo"
	code := codeEntitiesFixture(repoTag)

	res := Ingest(repoRoot, repoTag, []string{"docs/orders.md"}, code)

	if res.Documents != 1 {
		t.Fatalf("documents = %d, want 1", res.Documents)
	}
	// # Orders, ## Placing an order, ### Validation, ## Refunds = 4 sections.
	if res.Sections != 4 {
		t.Fatalf("sections = %d, want 4", res.Sections)
	}

	// Exactly one Document node + 4 Section nodes.
	var docNodes, secNodes int
	for _, e := range res.Entities {
		switch e.Kind {
		case string(types.EntityKindMarkdownDocument):
			docNodes++
			if e.SourceFile != "docs/orders.md" {
				t.Errorf("doc node SourceFile = %q", e.SourceFile)
			}
		case string(types.EntityKindSection):
			secNodes++
			if e.StartLine <= 0 || e.EndLine < e.StartLine {
				t.Errorf("section %q has bad span %d-%d", e.Name, e.StartLine, e.EndLine)
			}
		}
	}
	if docNodes != 1 || secNodes != 4 {
		t.Fatalf("nodes: doc=%d sec=%d, want 1 and 4", docNodes, secNodes)
	}

	// CONTAINS edges: 4 (doc->H1, H1->H2 placing, H2->H3 validation, doc/H1->H2 refunds).
	var contains, mentions int
	mentionTargets := map[string]bool{}
	for _, r := range res.Relationships {
		switch r.Kind {
		case string(types.RelationshipKindContains):
			contains++
		case string(types.RelationshipKindMentions):
			mentions++
			mentionTargets[r.ToID] = true
		}
	}
	if contains != 4 {
		t.Fatalf("CONTAINS edges = %d, want 4", contains)
	}

	// MENTIONS must point at the known code entities by exact name.
	wantTargets := map[string]bool{
		graph.EntityID(repoTag, string(types.EntityKindClass), "OrderService", "orders/order.go"):     true,
		graph.EntityID(repoTag, string(types.EntityKindFunction), "placeOrder", "orders/order.go"):    true,
		graph.EntityID(repoTag, string(types.EntityKindFunction), "validateOrder", "orders/order.go"): true,
	}
	for id := range wantTargets {
		if !mentionTargets[id] {
			t.Errorf("expected a MENTIONS edge to %q, none found", id)
		}
	}
	// Every MENTIONS edge must resolve to one of the 3 known code entities —
	// common words ("type", "data", "file", "test") must NOT produce edges and
	// no edge may point anywhere unexpected.
	if len(mentionTargets) != len(wantTargets) {
		t.Fatalf("distinct mention targets = %d, want %d (no noisy links)", len(mentionTargets), len(wantTargets))
	}
	for id := range mentionTargets {
		if !wantTargets[id] {
			t.Errorf("unexpected (noisy) MENTIONS target %q", id)
		}
	}
	// validateOrder is legitimately referenced in two distinct sections, so the
	// edge COUNT (4) exceeds the distinct-target count (3). Both are correct.
	if mentions < len(wantTargets) {
		t.Fatalf("mentions = %d, want >= %d", mentions, len(wantTargets))
	}
}

func TestIngest_Deterministic(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/repo")
	code := codeEntitiesFixture("repo")
	a := Ingest(repoRoot, "repo", []string{"docs/orders.md"}, code)
	b := Ingest(repoRoot, "repo", []string{"docs/orders.md"}, code)
	if len(a.Entities) != len(b.Entities) || len(a.Relationships) != len(b.Relationships) {
		t.Fatal("non-deterministic counts")
	}
	for i := range a.Entities {
		if a.Entities[i].ID != b.Entities[i].ID {
			t.Fatalf("entity order/ID differs at %d", i)
		}
	}
	for i := range a.Relationships {
		if a.Relationships[i].ID != b.Relationships[i].ID {
			t.Fatalf("rel order/ID differs at %d", i)
		}
	}
}
