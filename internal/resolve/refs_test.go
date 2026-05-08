package resolve

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func ent(id, kind, name string) types.EntityRecord {
	return types.EntityRecord{ID: id, Kind: kind, Name: name, SourceFile: "x.go"}
}

func TestReferences_Unambiguous(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("unambiguous: ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_Ambiguous(t *testing.T) {
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "Foo"),
		ent("bbbbbbbbbbbbbbbb", "Function", "Foo"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Foo", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Function:Foo" {
		t.Fatalf("ambiguous: ToID was rewritten to %s, expected stub preserved", rels[0].ToID)
	}
	if stats.Ambiguous != 1 {
		t.Fatalf("expected 1 ambiguous, got %d", stats.Ambiguous)
	}
}

func TestReferences_Unmatched(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Function:Missing", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "Function:Missing" {
		t.Fatalf("unmatched: ToID was rewritten: %s", rels[0].ToID)
	}
	if stats.Unmatched != 1 {
		t.Fatalf("expected 1 unmatched, got %d", stats.Unmatched)
	}
}

func TestReferences_KindAware(t *testing.T) {
	entities := []types.EntityRecord{
		ent("aaaaaaaaaaaaaaaa", "Function", "User"),
		ent("bbbbbbbbbbbbbbbb", "View", "User"),
	}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:User", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("kind-aware: ToID resolved to wrong entity: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_StubMissingPrefix(t *testing.T) {
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "Hello", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("bare-name fallback: ToID not rewritten: %s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_ScopePrefixedKind(t *testing.T) {
	// Pass 3 cross-language extractors emit kinds like "SCOPE.View". A stub
	// "View:User" must still resolve to that entity.
	entities := []types.EntityRecord{ent("cccccccccccccccc", "SCOPE.View", "Dashboard")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "View:Dashboard", Kind: "USES"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "cccccccccccccccc" {
		t.Fatalf("scope-prefixed kind: ToID=%s", rels[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}

func TestReferences_SkipsHexIDs(t *testing.T) {
	// Already-resolved IDs (16-char lower hex) must be left untouched.
	entities := []types.EntityRecord{ent("aaaaaaaaaaaaaaaa", "Function", "Hello")}
	rels := []types.RelationshipRecord{{FromID: "0000000000000000", ToID: "ffffffffffffffff", Kind: "CALLS"}}
	idx := BuildIndex(entities)
	stats := References(rels, idx)
	if rels[0].ToID != "ffffffffffffffff" {
		t.Fatalf("hex-ID was modified: %s", rels[0].ToID)
	}
	if stats.Rewritten != 0 || stats.Ambiguous != 0 || stats.Unmatched != 0 {
		t.Fatalf("hex-ID counted in stats: %+v", stats)
	}
}

func TestReferencesEmbedded(t *testing.T) {
	records := []types.EntityRecord{
		{ID: "aaaaaaaaaaaaaaaa", Kind: "Function", Name: "Hello", SourceFile: "a.go"},
		{
			ID:         "bbbbbbbbbbbbbbbb",
			Kind:       "Function",
			Name:       "Greet",
			SourceFile: "b.go",
			Relationships: []types.RelationshipRecord{
				{ToID: "Hello", Kind: "CALLS"},
			},
		},
	}
	idx := BuildIndex(records)
	stats := ReferencesEmbedded(records, idx)
	if records[1].Relationships[0].ToID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("embedded: ToID not rewritten: %s", records[1].Relationships[0].ToID)
	}
	if stats.Rewritten != 1 {
		t.Fatalf("expected 1 rewrite, got %d", stats.Rewritten)
	}
}
