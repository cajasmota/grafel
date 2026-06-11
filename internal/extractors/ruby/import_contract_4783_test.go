package ruby_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// #4783 — Ruby IMPORTS edges must stamp imported_name/local_name (where
// statically recoverable) + require_kind so #4515's per-symbol external-node
// synthesis can mint ext:<gem>:<Const>.

func rbImportRels(ents []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				out = append(out, r)
			}
		}
	}
	return out
}

func rbFindImport(rels []types.RelationshipRecord, toID string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].ToID == toID {
			return &rels[i]
		}
	}
	return nil
}

// `require 'active_record'` → imported_name=local_name=ActiveRecord (CamelCased
// leaf, the gem-constant convention) + require_kind=require.
func TestRuby_ImportContract_RequireGem(t *testing.T) {
	src := `require 'active_record'
`
	rels := rbImportRels(runRuby(t, src))
	r := rbFindImport(rels, "active_record")
	if r == nil {
		t.Fatalf("no IMPORTS edge for active_record; got %+v", rels)
	}
	if r.Properties["require_kind"] != "require" {
		t.Fatalf("require_kind=%q, want require", r.Properties["require_kind"])
	}
	if r.Properties["imported_name"] != "ActiveRecord" || r.Properties["local_name"] != "ActiveRecord" {
		t.Fatalf("props=%v, want imported_name=local_name=ActiveRecord", r.Properties)
	}
}

// `require 'json'` → Json (single-segment camelize).
func TestRuby_ImportContract_RequireSimple(t *testing.T) {
	src := `require 'json'
`
	rels := rbImportRels(runRuby(t, src))
	r := rbFindImport(rels, "json")
	if r == nil || r.Properties["imported_name"] != "Json" {
		t.Fatalf("json import props wrong: %+v", r)
	}
}

// `require_relative 'helper'` is intra-project: stamps require_kind but no
// imported_name (so the external synth correctly skips it).
func TestRuby_ImportContract_RequireRelative(t *testing.T) {
	src := `require_relative 'helper'
`
	rels := rbImportRels(runRuby(t, src))
	r := rbFindImport(rels, "helper")
	if r == nil {
		t.Fatalf("no IMPORTS edge for require_relative helper; got %+v", rels)
	}
	if r.Properties["require_kind"] != "require_relative" {
		t.Fatalf("require_kind=%q, want require_relative", r.Properties["require_kind"])
	}
	if _, ok := r.Properties["imported_name"]; ok {
		t.Fatalf("require_relative must NOT stamp imported_name, got %v", r.Properties)
	}
}
