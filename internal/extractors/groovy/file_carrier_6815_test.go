package groovy_test

// #6815 — groovy's buildImportRecords anchors every IMPORTS edge on the
// .groovy path, and nothing carried that path as its Name, so the FROM end of
// every import edge resolved to nothing. Graded in both directions: the carrier
// exists when there is an import to carry, and NOT otherwise.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/groovy"
	"github.com/cajasmota/grafel/internal/types"
)

const groovyPath6815 = "grails-app/controllers/UserController.groovy"

func extractGroovy6815(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	tree := parseForTest(t, src)
	ext, ok := extractor.Get("groovy")
	if !ok {
		t.Fatal("groovy extractor not registered")
	}
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     groovyPath6815,
		Content:  []byte(src),
		Language: "groovy",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return out
}

func groovyCarriers6815(recs []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Subtype == "file" && r.Name == groovyPath6815 {
			out = append(out, r)
		}
	}
	return out
}

func groovyFileAnchoredImports6815(recs []types.EntityRecord) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, r := range recs {
		for _, rel := range r.Relationships {
			if rel.Kind == "IMPORTS" && rel.FromID == groovyPath6815 {
				out = append(out, rel)
			}
		}
	}
	return out
}

// Axis VARIED: the import declaration (present). HELD CONSTANT: one class with
// one method, plain non-static non-wildcard import.
func TestGroovy_ImportGetsAFileCarrier_6815(t *testing.T) {
	src := `import grails.gorm.transactions.Transactional

class UserController {
    def index() {
        return [users: []]
    }
}
`
	recs := extractGroovy6815(t, src)
	if n := len(groovyFileAnchoredImports6815(recs)); n != 1 {
		t.Fatalf("premise: want exactly 1 file-anchored IMPORTS edge, got %d", n)
	}
	if n := len(groovyCarriers6815(recs)); n != 1 {
		t.Fatalf("an import edge must have exactly 1 file carrier, got %d", n)
	}
}

// Axis VARIED: the import FORM — `import static … .*`, which buildImportRecord
// routes down its wildcard branch and gives a different Properties set and a
// different Name (no local_name). HELD CONSTANT: one class with one method, one
// import declaration. A carrier keyed on the plain-import branch alone would
// pass the case above and leave every static/wildcard edge dangling.
func TestGroovy_StaticWildcardImportGetsAFileCarrier_6815(t *testing.T) {
	src := `import static java.lang.Math.*

class UserController {
    def index() {
        return [users: []]
    }
}
`
	recs := extractGroovy6815(t, src)
	if n := len(groovyFileAnchoredImports6815(recs)); n != 1 {
		t.Fatalf("premise: want exactly 1 file-anchored IMPORTS edge, got %d", n)
	}
	if n := len(groovyCarriers6815(recs)); n != 1 {
		t.Fatalf("a static wildcard import must have exactly 1 file carrier, got %d", n)
	}
}

// OVER-FIRING control. Axis VARIED: imports absent. HELD CONSTANT: the same
// class and method, byte for byte, as the first case.
func TestGroovy_NoCarrierWithoutAnythingToCarry_6815(t *testing.T) {
	src := `class UserController {
    def index() {
        return [users: []]
    }
}
`
	recs := extractGroovy6815(t, src)
	if n := len(groovyFileAnchoredImports6815(recs)); n != 0 {
		t.Fatalf("premise: want 0 file-anchored IMPORTS edges, got %d", n)
	}
	if n := len(groovyCarriers6815(recs)); n != 0 {
		t.Fatalf("a class with nothing to carry must emit no file carrier, got %d", n)
	}
	for _, r := range recs {
		if r.Name == groovyPath6815 {
			t.Fatalf("no record may be named %q here, got kind=%q subtype=%q",
				groovyPath6815, r.Kind, r.Subtype)
		}
	}
}

// OVER-FIRING control on COUNT. Axis VARIED: the NUMBER of import declarations
// (three). HELD CONSTANT: one file, one class. The carrier is per-FILE, not
// per-EDGE.
func TestGroovy_OneCarrierPerFileNotPerImport_6815(t *testing.T) {
	src := `import grails.gorm.transactions.Transactional
import java.util.List
import static java.lang.Math.max

class UserController {
    def index() {
        return [users: []]
    }
}
`
	recs := extractGroovy6815(t, src)
	if n := len(groovyFileAnchoredImports6815(recs)); n != 3 {
		t.Fatalf("premise: want 3 file-anchored IMPORTS edges, got %d", n)
	}
	if n := len(groovyCarriers6815(recs)); n != 1 {
		t.Fatalf("3 import edges must still yield exactly 1 file carrier, got %d", n)
	}
}

// The carrier owns no relationships: the per-import stubs still carry the
// IMPORTS edges, so re-homing them onto the carrier would double every edge.
func TestGroovy_CarrierCarriesNoRelationshipsOfItsOwn_6815(t *testing.T) {
	src := `import java.util.List

class UserController {
    def index() {
        return [users: []]
    }
}
`
	recs := extractGroovy6815(t, src)
	cs := groovyCarriers6815(recs)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if n := len(cs[0].Relationships); n != 0 {
		t.Fatalf("the groovy file carrier must own no relationships, got %d", n)
	}
	if n := len(groovyFileAnchoredImports6815(recs)); n != 1 {
		t.Fatalf("the import edge must still be emitted exactly once, got %d", n)
	}
}

// The carrier must be stamped groovy and anchored on the file it names.
func TestGroovy_CarrierIsLanguageTagged_6815(t *testing.T) {
	src := `import java.util.List

class UserController {
    def index() {
        return [users: []]
    }
}
`
	recs := extractGroovy6815(t, src)
	cs := groovyCarriers6815(recs)
	if len(cs) != 1 {
		t.Fatalf("premise: want 1 carrier, got %d", len(cs))
	}
	if cs[0].Language != "groovy" {
		t.Fatalf("carrier Language = %q, want %q", cs[0].Language, "groovy")
	}
	if cs[0].SourceFile != groovyPath6815 {
		t.Fatalf("carrier SourceFile = %q, want %q", cs[0].SourceFile, groovyPath6815)
	}
}
