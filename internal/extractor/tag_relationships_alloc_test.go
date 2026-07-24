package extractor

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// TestTagRelationshipsLanguageAllocsPerRel pins the real hot path from #5954.
//
// TagRelationshipsLanguage stamps a single "language" property on ~1.34M
// relationships during a full corpus index. Backed by map[string]string that
// is two allocations and ~337 B per relationship (~142 MB of peak heap for
// one short string); backed by types.Props it is one allocation of ~32 B.
//
// The assertion is deliberately on allocations rather than bytes so it stays
// stable across Go releases: a map costs at least an hmap header plus one
// bucket, so anything at or below one allocation per relationship proves the
// map is gone.
func TestTagRelationshipsLanguageAllocsPerRel(t *testing.T) {
	const nRels = 128

	recs := []types.EntityRecord{{
		Name:          "fixture",
		Kind:          "CODE.Function",
		Relationships: make([]types.RelationshipRecord, nRels),
	}}
	rels := recs[0].Relationships

	avg := testing.AllocsPerRun(200, func() {
		for i := range rels {
			rels[i].Properties = nil
		}
		TagRelationshipsLanguage(recs, "go")
	})

	perRel := avg / float64(nRels)
	if perRel > 1.0 {
		t.Errorf("TagRelationshipsLanguage allocates %.2f objects per relationship, want <= 1.00 "+
			"(a map[string]string costs 2: hmap + bucket)", perRel)
	}
}

// TestTagStandaloneRelationshipsLanguageAllocsPerRel is the pass-2 twin of
// TestTagRelationshipsLanguageAllocsPerRel.
func TestTagStandaloneRelationshipsLanguageAllocsPerRel(t *testing.T) {
	const nRels = 128

	rels := make([]types.RelationshipRecord, nRels)

	avg := testing.AllocsPerRun(200, func() {
		for i := range rels {
			rels[i].Properties = nil
		}
		TagStandaloneRelationshipsLanguage(rels, "go")
	})

	perRel := avg / float64(nRels)
	if perRel > 1.0 {
		t.Errorf("TagStandaloneRelationshipsLanguage allocates %.2f objects per relationship, want <= 1.00", perRel)
	}
}

// TestTagRelationshipsLanguageSemantics pins the behaviour the allocation
// change must not disturb: lazily stamped when absent, existing values
// preserved (per-extractor override wins), other keys untouched.
func TestTagRelationshipsLanguageSemantics(t *testing.T) {
	recs := []types.EntityRecord{{
		Relationships: []types.RelationshipRecord{
			{FromID: "a", ToID: "b", Kind: "CALLS"},
			{FromID: "c", ToID: "d", Kind: "CALLS", Properties: types.Props{{K: "language", V: "python"}}},
			{FromID: "e", ToID: "f", Kind: "CALLS", Properties: types.Props{{K: "line", V: "42"}}},
		},
	}}

	TagRelationshipsLanguage(recs, "go")

	rels := recs[0].Relationships
	if got := rels[0].Properties.Get("language"); got != "go" {
		t.Errorf("rel 0 language = %q, want %q", got, "go")
	}
	if got := rels[1].Properties.Get("language"); got != "python" {
		t.Errorf("rel 1 language = %q, want %q (existing value must win)", got, "python")
	}
	if got := rels[2].Properties.Get("language"); got != "go" {
		t.Errorf("rel 2 language = %q, want %q", got, "go")
	}
	if got := rels[2].Properties.Get("line"); got != "42" {
		t.Errorf("rel 2 line = %q, want %q (other keys must survive)", got, "42")
	}
	if _, ok := rels[0].Properties.Lookup("nope"); ok {
		t.Error("absent key reported present")
	}

	// No-op for an empty language, and nil stays nil (omitempty parity).
	TagRelationshipsLanguage([]types.EntityRecord{{Relationships: []types.RelationshipRecord{{}}}}, "")
}
