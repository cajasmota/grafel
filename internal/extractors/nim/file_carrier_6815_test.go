package nim_test

// #6815 — nim's buildImportEntities anchors every IMPORTS edge on the .nim
// path, and nothing carried that path as its Name, so the FROM end of every
// import edge resolved to nothing. Graded in both directions: the carrier
// exists when there is an import to carry, and NOT otherwise. The second
// direction is the one a recall-shaped assertion cannot see — an unconditional
// carrier would mint one orphan node per .nim file in a repo and still satisfy
// every "the carrier now exists" check.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/nim"
	"github.com/cajasmota/grafel/internal/types"
)

const nimPath6815 = "src/domain/animals.nim"

func extractNim6815(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("nim")
	if !ok {
		t.Fatal("nim extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     nimPath6815,
		Content:  []byte(src),
		Language: "nim",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return out
}

func nimCarriers6815(recs []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == nimPath6815 {
			out = append(out, r)
		}
	}
	return out
}

func nimFileAnchoredImports6815(recs []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" && rel.FromID == nimPath6815 {
				out = append(out, rel)
			}
		}
	}
	return out
}

// Axis VARIED: the import statement (present). HELD CONSTANT: one proc, no
// include, no from-import.
func TestNim_ImportGetsAFileCarrier_6815(t *testing.T) {
	src := `import strutils

proc greet(name: string): string =
  result = name.toUpperAscii()
`
	recs := extractNim6815(t, src)
	if n := len(nimFileAnchoredImports6815(recs)); n != 1 {
		t.Fatalf("premise: want exactly 1 file-anchored IMPORTS edge, got %d", n)
	}
	if n := len(nimCarriers6815(recs)); n != 1 {
		t.Fatalf("an import edge must have exactly 1 file carrier, got %d", n)
	}
}

// Axis VARIED: the import FORM — `include` rather than `import`. HELD CONSTANT:
// one proc, one module named, no plain `import` line. Nim's collectImports
// feeds three syntaxes into the same edge producer; a carrier keyed on the
// `import` regex alone would pass the case above and leave this one dangling.
func TestNim_IncludeGetsAFileCarrier_6815(t *testing.T) {
	src := `include prelude

proc greet(name: string): string =
  result = name
`
	recs := extractNim6815(t, src)
	if n := len(nimFileAnchoredImports6815(recs)); n != 1 {
		t.Fatalf("premise: want exactly 1 file-anchored IMPORTS edge, got %d", n)
	}
	if n := len(nimCarriers6815(recs)); n != 1 {
		t.Fatalf("an include edge must have exactly 1 file carrier, got %d", n)
	}
}

// OVER-FIRING control. Axis VARIED: imports absent. HELD CONSTANT: the same
// proc, byte for byte, as the first case.
func TestNim_NoCarrierWithoutAnythingToCarry_6815(t *testing.T) {
	src := `proc greet(name: string): string =
  result = name
`
	recs := extractNim6815(t, src)
	if n := len(nimFileAnchoredImports6815(recs)); n != 0 {
		t.Fatalf("premise: want 0 file-anchored IMPORTS edges, got %d", n)
	}
	if n := len(nimCarriers6815(recs)); n != 0 {
		t.Fatalf("a module with nothing to carry must emit no file carrier, got %d", n)
	}
	for _, r := range recs {
		if r.Name == nimPath6815 {
			t.Fatalf("no record may be named %q here, got kind=%q subtype=%q",
				nimPath6815, r.Kind, r.Subtype)
		}
	}
}

// OVER-FIRING control on COUNT. Axis VARIED: the NUMBER of imports (three
// modules on one `import` line plus one `from … import`). HELD CONSTANT: one
// file. The carrier is per-FILE, not per-EDGE.
func TestNim_OneCarrierPerFileNotPerImport_6815(t *testing.T) {
	src := `import strutils, sequtils, tables
from os import getEnv

proc greet(name: string): string =
  result = name
`
	recs := extractNim6815(t, src)
	if n := len(nimFileAnchoredImports6815(recs)); n != 4 {
		t.Fatalf("premise: want 4 file-anchored IMPORTS edges, got %d", n)
	}
	if n := len(nimCarriers6815(recs)); n != 1 {
		t.Fatalf("4 import edges must still yield exactly 1 file carrier, got %d", n)
	}
}

// The carrier owns no relationships: the per-import stubs still carry the
// IMPORTS edges, so re-homing them onto the carrier would double every edge.
func TestNim_CarrierCarriesNoRelationshipsOfItsOwn_6815(t *testing.T) {
	src := `import strutils
`
	recs := extractNim6815(t, src)
	cs := nimCarriers6815(recs)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Fatalf("the nim file carrier must own no relationships, got %d", n)
	}
	if n := len(nimFileAnchoredImports6815(recs)); n != 1 {
		t.Fatalf("the import edge must still be emitted exactly once, got %d", n)
	}
}

// The carrier must not be mistaken for an import placeholder: #6481 keys the
// placeholder marker on kind=="SCOPE.Component" && subtype=="import", and a
// carrier stamped that way would be excluded from the by-name index it exists
// to populate.
func TestNim_CarrierIsSubtypeFileNotImport_6815(t *testing.T) {
	src := `import strutils
`
	recs := extractNim6815(t, src)
	cs := nimCarriers6815(recs)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Subtype != "file" {
		t.Fatalf("carrier Subtype = %q, want %q", cs[0].Subtype, "file")
	}
	if cs[0].Language != "nim" {
		t.Fatalf("carrier Language = %q, want %q", cs[0].Language, "nim")
	}
	if cs[0].SourceFile != nimPath6815 {
		t.Fatalf("carrier SourceFile = %q, want %q", cs[0].SourceFile, nimPath6815)
	}
}
