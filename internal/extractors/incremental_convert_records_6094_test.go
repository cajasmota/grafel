// Package extractors — incremental_convert_records_6094_test.go
//
// #6094 unit gates for convertExtractedRecords: the record→graph seam on the
// incremental path. Two behaviours live there and each has a destructive
// failure mode, so each is pinned independently:
//
//	OWNER-ID SUBSTITUTION — an omitted FromID means "from my owner". Dropping
//	  it strands the edge with an empty FromID, invisible to the stale-edge
//	  eviction, which is the unbounded accumulation in #6094.
//	THE seenRel GUARD — the same owned edge emitted twice for one file must be
//	  emitted once, as the full path does.
//
// The guard's KEY is the dangerous part. (from, to, kind) is not a unique
// relationship key in general — engine passes salt IDs precisely because edges
// share a triple, and #6085 converged on data loss by collapsing on it. It is
// safe HERE only because types.RelationshipRecord has no ID field and the
// salted producers never reach this loop. TestConvertExtractedRecords_GuardKey
// pins every component of that key, so widening OR narrowing it fails loudly.
package extractors

import (
	"fmt"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// ceRec builds an EntityRecord with a deterministic identity and the given
// embedded relationships.
func ceRec(kind, name, file string, rels ...types.RelationshipRecord) types.EntityRecord {
	return types.EntityRecord{
		Kind:          kind,
		Name:          name,
		QualifiedName: name,
		SourceFile:    file,
		Language:      "go",
		Relationships: rels,
	}
}

// ceRel builds a RelationshipRecord. An empty from means "owned by my record".
func ceRel(from, to, kind string, props map[string]string) types.RelationshipRecord {
	return types.RelationshipRecord{
		FromID:     from,
		ToID:       to,
		Kind:       kind,
		Properties: types.PropsFromMap(props),
	}
}

// ceTriples renders the produced edges as sorted "from|to|kind" strings.
func ceTriples(rels []graph.Relationship) []string {
	out := make([]string, 0, len(rels))
	for _, r := range rels {
		out = append(out, fmt.Sprintf("%s|%s|%s", r.FromID, r.ToID, r.Kind))
	}
	sort.Strings(out)
	return out
}

// TestConvertExtractedRecords_GuardKey pins the identity the dedupe guard uses.
//
// Each sub-case is a mutant detector. The (from,to) case is the one that
// matters most: dropping Kind from the key is the exact shape of the #6085 data
// loss, and it is invisible to any test that only counts rows per file.
func TestConvertExtractedRecords_GuardKey(t *testing.T) {
	const repo = "r"

	t.Run("identical owned edges collapse to one", func(t *testing.T) {
		// Two SCOPE.Component records for the same import, each emitting the
		// same file→package IMPORTS edge — the real shape produced by Go's
		// `import "strings"` + `import s2 "strings"`.
		recs := []types.EntityRecord{
			ceRec("SCOPE.Component", "strings", "a.go", ceRel("a.go", "ext:strings", "IMPORTS", map[string]string{"language": "go"})),
			ceRec("SCOPE.Component", "strings", "a.go", ceRel("a.go", "ext:strings", "IMPORTS", map[string]string{"language": "go"})),
		}
		_, rels := convertExtractedRecords(recs, repo, map[string]bool{})
		if got := ceTriples(rels); len(got) != 1 {
			t.Fatalf("the guard must suppress the byte-identical repeat: got %d edge(s) %v, want 1", len(got), got)
		}
	})

	t.Run("same endpoints, DIFFERENT kinds both survive", func(t *testing.T) {
		// THE KEY ASSERTION. These two edges share (from, to) and differ only in
		// Kind. They are distinct edges and both must reach the graph. A guard
		// keyed on (from, to) silently destroys one of them — that is the #6085
		// data-loss shape, and this is the only thing in the tree that sees it.
		recs := []types.EntityRecord{
			ceRec("SCOPE.Component", "a.go", "a.go",
				ceRel("a.go", "ext:strings", "IMPORTS", map[string]string{"language": "go"}),
				ceRel("a.go", "ext:strings", "REFERENCES", map[string]string{"language": "go"}),
			),
		}
		_, rels := convertExtractedRecords(recs, repo, map[string]bool{})
		got := ceTriples(rels)
		want := []string{"a.go|ext:strings|IMPORTS", "a.go|ext:strings|REFERENCES"}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("edges sharing (from,to) but differing in Kind must BOTH survive — Kind is "+
				"load-bearing in the dedupe key.\ngot:  %v\nwant: %v", got, want)
		}
	})

	t.Run("same kind, DIFFERENT endpoints both survive", func(t *testing.T) {
		// Pins the from- and to- components of the key the same way, so a guard
		// narrowed to (to, kind) or (from, kind) also fails.
		recs := []types.EntityRecord{
			ceRec("SCOPE.Component", "a.go", "a.go",
				ceRel("a.go", "ext:strings", "IMPORTS", nil),
				ceRel("a.go", "ext:bytes", "IMPORTS", nil),
				ceRel("b.go", "ext:strings", "IMPORTS", nil),
			),
		}
		_, rels := convertExtractedRecords(recs, repo, map[string]bool{})
		if got := ceTriples(rels); len(got) != 3 {
			t.Fatalf("three edges differing in an endpoint must all survive: got %d %v", len(got), got)
		}
	})

	t.Run("owned edges from DIFFERENT owners both survive", func(t *testing.T) {
		// Two distinct records emit an omitted-FromID edge to the same target —
		// the shape Go produces when two functions in one file both reference the
		// same import. After owner substitution these are different edges. If
		// owner substitution were dropped, BOTH would key on ("", to, kind) and
		// the guard would collapse them to one — so this case also fails if the
		// substitution regresses, in a way distinct from the empty-FromID check.
		recs := []types.EntityRecord{
			ceRec("SCOPE.Operation", "A", "a.go", ceRel("", "scope:component:ref:go:a.go:strings", "REFERENCES", nil)),
			ceRec("SCOPE.Operation", "B", "a.go", ceRel("", "scope:component:ref:go:a.go:strings", "REFERENCES", nil)),
		}
		ents, rels := convertExtractedRecords(recs, repo, map[string]bool{})
		if len(rels) != 2 {
			t.Fatalf("two owners emitting the same owned edge produce two distinct edges: got %d %v",
				len(rels), ceTriples(rels))
		}
		if rels[0].FromID != ents[0].ID || rels[1].FromID != ents[1].ID {
			t.Fatalf("each owned edge must be attributed to ITS OWN owner: got from=[%s %s], want [%s %s]",
				rels[0].FromID, rels[1].FromID, ents[0].ID, ents[1].ID)
		}
	})

	t.Run("guard is shared across files in one batch", func(t *testing.T) {
		// seenRel is threaded through the whole re-extraction batch, so the same
		// edge emitted from two different files is still emitted once — matching
		// buildDocument, whose seenRel is corpus-wide.
		seen := map[string]bool{}
		_, r1 := convertExtractedRecords([]types.EntityRecord{
			ceRec("SCOPE.Component", "strings", "a.go", ceRel("shared", "ext:strings", "IMPORTS", nil)),
		}, repo, seen)
		_, r2 := convertExtractedRecords([]types.EntityRecord{
			ceRec("SCOPE.Component", "strings", "b.go", ceRel("shared", "ext:strings", "IMPORTS", nil)),
		}, repo, seen)
		if len(r1) != 1 || len(r2) != 0 {
			t.Fatalf("the batch-wide guard must suppress the cross-file repeat: got %d then %d, want 1 then 0",
				len(r1), len(r2))
		}
	})
}

// TestConvertExtractedRecords_OwnerIDSubstitution pins the other half of the
// #6094 fix: no edge may leave this seam with an empty FromID.
func TestConvertExtractedRecords_OwnerIDSubstitution(t *testing.T) {
	recs := []types.EntityRecord{
		ceRec("SCOPE.Class", "T", "a.go",
			ceRel("", "field:T.N", "CONTAINS", map[string]string{"language": "go"}),
			ceRel("", "method:T.Do", "CONTAINS", map[string]string{"language": "go"}),
			// An edge that DOES carry an explicit FromID must be left alone.
			ceRel("explicit", "ext:strings", "IMPORTS", nil),
		),
	}
	ents, rels := convertExtractedRecords(recs, "r", map[string]bool{})
	if len(ents) != 1 {
		t.Fatalf("want 1 entity, got %d", len(ents))
	}
	owner := ents[0].ID
	if owner == "" {
		t.Fatal("owner entity ID is empty — the fixture cannot pin substitution")
	}
	for _, r := range rels {
		if r.FromID == "" {
			t.Errorf("edge →%s:%s left the record→graph seam with an EMPTY FromID; the owning "+
				"record's ID (%s) must be substituted, exactly as buildDocument does", r.ToID, r.Kind, owner)
		}
		// The ID must be derived from the SUBSTITUTED FromID, not the empty one,
		// or downstream re-keying disagrees with the full path.
		if want := graph.RelationshipID(r.FromID, r.ToID, r.Kind); r.ID != want {
			t.Errorf("edge %s→%s:%s has ID %q, want %q (derived from the substituted FromID)",
				r.FromID, r.ToID, r.Kind, r.ID, want)
		}
	}
	for _, r := range rels {
		if r.Kind == "CONTAINS" && r.FromID != owner {
			t.Errorf("owned CONTAINS edge →%s attributed to %q, want the owning record %q", r.ToID, r.FromID, owner)
		}
		if r.Kind == "IMPORTS" && r.FromID != "explicit" {
			t.Errorf("edge with an EXPLICIT FromID was overwritten: got %q, want %q", r.FromID, "explicit")
		}
	}
}
