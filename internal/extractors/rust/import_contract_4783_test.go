package rust_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// #4783 — Rust IMPORTS edges must stamp imported_name/local_name (and wildcard)
// so the per-symbol external-node synthesis (#4515) mints ext:<crate>:<Symbol>.

// importRels collects every IMPORTS edge from the extracted entities.
func importRels(ents []types.EntityRecord) []types.RelationshipRecord {
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

func findImport(rels []types.RelationshipRecord, toID string) *types.RelationshipRecord {
	for i := range rels {
		if rels[i].ToID == toID {
			return &rels[i]
		}
	}
	return nil
}

// RED→GREEN: `use tokio::sync::Mutex;` stamps imported_name/local_name=Mutex,
// and the ToID keeps the crate root so the external synth resolves ext:tokio:Mutex.
func TestRust_ImportContract_SingleNamed(t *testing.T) {
	src := `use tokio::sync::Mutex;
fn main() {}
`
	rels := importRels(runRust(t, src))
	r := findImport(rels, "tokio::sync::Mutex")
	if r == nil {
		t.Fatalf("no IMPORTS edge for tokio::sync::Mutex; got %+v", rels)
	}
	if r.Properties["imported_name"] != "Mutex" || r.Properties["local_name"] != "Mutex" {
		t.Fatalf("props=%v, want imported_name=local_name=Mutex", r.Properties)
	}
}

// Brace group fans out to one edge per symbol, each leaf-stamped; `as` aliases
// keep imported=leaf, local=alias.
func TestRust_ImportContract_BraceGroupAndAlias(t *testing.T) {
	src := `use serde::{Serialize, Deserialize as De};
fn main() {}
`
	rels := importRels(runRust(t, src))
	ser := findImport(rels, "serde::Serialize")
	if ser == nil || ser.Properties["imported_name"] != "Serialize" || ser.Properties["local_name"] != "Serialize" {
		t.Fatalf("Serialize edge wrong: %+v", ser)
	}
	de := findImport(rels, "serde::Deserialize")
	if de == nil || de.Properties["imported_name"] != "Deserialize" || de.Properties["local_name"] != "De" {
		t.Fatalf("aliased Deserialize edge wrong: %+v", de)
	}
}

// Glob import stamps wildcard=1 with the namespace local.
func TestRust_ImportContract_Wildcard(t *testing.T) {
	src := `use std::collections::*;
fn main() {}
`
	rels := importRels(runRust(t, src))
	r := findImport(rels, "std::collections")
	if r == nil {
		t.Fatalf("no IMPORTS edge for std::collections glob; got %+v", rels)
	}
	if r.Properties["wildcard"] != "1" || r.Properties["local_name"] != "collections" {
		t.Fatalf("glob props=%v, want wildcard=1 local_name=collections", r.Properties)
	}
}
