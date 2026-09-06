package astro

import (
	"maps"
	"slices"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// issue6298_anchoring_test.go — RENDERS and IMPLEMENTS must be anchored on the
// .astro component that owns them, not on the file path.
//
// WHAT WAS MEASURED BEFORE THE FIX. The three .astro files below, extracted,
// id-stamped and pushed through the production resolver pipeline
// (ResolveImports → ReferencesEmbedded), produced:
//
//	owner="Header" kind=RENDERS    FROM=<UNRESOLVED:src/components/Header.astro> TO=Nav @src/components/Nav.astro
//	owner="Header" kind=RENDERS    FROM=<UNRESOLVED:src/components/Header.astro> TO=Logo @src/components/Logo.astro
//	owner="Header" kind=IMPLEMENTS FROM=<UNRESOLVED:src/components/Header.astro> TO=Nav @src/components/Nav.astro
//
// "UNRESOLVED" is literal: no entity in the set carries that ID. This package
// emits no FileEntity, so unlike the Solidity (#6295) and verilog forms of the
// same defect — where the rewrite at least landed on a real file component —
// the path survived rewriting verbatim and reached graph assembly as a DANGLING
// FromID. Both assembly paths (cmd/grafel/index.go and relRecordToGraphRel in
// internal/extractors/incremental.go) substitute the owning record's own entity
// id only when FromID is EMPTY, so a non-empty path is passed straight through.
//
// After the fix the same run gives FROM=<empty> on all three, which assembly
// stamps with Header's id.
//
// REAL-TREE COUNT (AGENTS.md § Evidence). This repository's own Astro site,
// site/ — 11 .astro files, 13 entity records — extracted and resolved the same
// way:
//
//	before: RENDERS+IMPLEMENTS = 10, FromID empty = 0,  DANGLING = 10
//	after:  RENDERS+IMPLEMENTS = 10, FromID empty = 10, DANGLING =  0
//
// Every one of the ten template edges on a real Astro tree was dangling. The
// measurement harness was a throwaway; site/ is checked in, so it is
// reproducible by walking it through astroExtract + BuildIndex/ReferencesEmbedded.
// No equivalent verilog tree exists locally (0 .sv / .v files in this repo and
// in ~/Projects/archigraph-corpora), so verilog's evidence is the two-class
// fixture in the sibling test file and its measured pre-fix id collision.
func TestAstro_TemplateRelsAnchoredOnComponent(t *testing.T) {
	files := map[string]string{
		"src/components/Header.astro": "---\nimport Nav from './Nav.astro';\n---\n<div><Nav client:load /><Logo /></div>\n",
		"src/components/Nav.astro":    "---\n---\n<nav>hi</nav>\n",
		"src/components/Logo.astro":   "---\n---\n<span>logo</span>\n",
	}
	var recs []types.EntityRecord
	for _, p := range slices.Sorted(maps.Keys(files)) {
		recs = append(recs, astroExtract(t, p, files[p])...)
	}
	for i := range recs {
		if recs[i].Name == "" {
			continue
		}
		recs[i].ID = graph.EntityID("issue6298", recs[i].Kind, recs[i].Name, recs[i].SourceFile)
	}

	// REVISITED BY #6852, exactly as the sentence this replaces asked for.
	//
	// It used to read "nothing in this package's output is named for the file, so
	// the file path can never resolve to an entity here", and said that if astro
	// ever started emitting a FileEntity this was the assertion to revisit. astro
	// now does: #6852 added a CONDITIONAL extractor.PrependFileCarrier so the
	// IMPORTS edge's path-valued FromID has something to resolve onto.
	//
	// The premise that mattered survives, narrowed rather than deleted, and it is
	// what stops this test from passing for the wrong reason. The RENDERS and
	// IMPLEMENTS edges below must be anchored on the COMPONENT. If the carrier
	// were to own them -- which is exactly what happens if the PrependFileCarrier
	// call is placed above the template section of Extract, since it inserts at
	// index 0 and the template edges are appended to entities[0] -- this file's
	// subject would have silently moved to a different node. So: the ONLY
	// file-named record is the carrier, and it owns NO relationships.
	fileNamed := 0
	for i := range recs {
		if recs[i].Name != "src/components/Header.astro" {
			continue
		}
		fileNamed++
		if recs[i].Kind != "SCOPE.Component" || recs[i].Subtype != "file" {
			t.Errorf("the file-named entity is %s/%s, want SCOPE.Component/file - "+
				"the only record astro may name after the path is the #6852 carrier",
				recs[i].Kind, recs[i].Subtype)
		}
		if n := len(recs[i].Relationships); n != 0 {
			t.Errorf("the file carrier owns %d relationship(s); RENDERS and IMPLEMENTS "+
				"belong to the component (#6298), and a carrier that owns them is this "+
				"defect reintroduced from the other end", n)
		}
	}
	if fileNamed != 1 {
		t.Errorf("records named after the file = %d, want exactly 1 (the #6852 carrier) - "+
			"0 means the path-anchored IMPORTS edge dangles again, >1 puts two nodes "+
			"under one entity id", fileNamed)
	}

	resolve.ResolveImports(recs, resolve.BuildImportTable(recs))
	resolve.ReferencesEmbedded(recs, resolve.BuildIndex(recs))

	var header *types.EntityRecord
	for i := range recs {
		if recs[i].Name == "Header" && recs[i].Kind == "SCOPE.Component" {
			header = &recs[i]
		}
	}
	if header == nil {
		t.Fatal("no Header component entity")
	}

	byID := make(map[string]*types.EntityRecord, len(recs))
	for i := range recs {
		byID[recs[i].ID] = &recs[i]
	}

	var renders, implements []string
	for _, r := range header.Relationships {
		switch r.Kind {
		case "RENDERS", "IMPLEMENTS":
		default:
			continue // IMPORTS keeps the file path on purpose (#120).
		}
		if r.FromID != "" {
			t.Errorf("%s → %s: FromID = %q, want \"\" so assembly stamps Header's id",
				r.Kind, r.ToID, r.FromID)
		}
		target := "<dangling:" + r.ToID + ">"
		if e := byID[r.ToID]; e != nil {
			target = e.Name + "@" + e.SourceFile
		}
		if r.Kind == "RENDERS" {
			renders = append(renders, target)
		} else {
			implements = append(implements, target)
		}
	}
	slices.Sort(renders)
	if got, want := slices.Compact(renders), []string{"Logo@src/components/Logo.astro", "Nav@src/components/Nav.astro"}; !slices.Equal(got, want) {
		t.Errorf("RENDERS targets = %v, want %v", got, want)
	}
	if got, want := implements, []string{"Nav@src/components/Nav.astro"}; !slices.Equal(got, want) {
		t.Errorf("IMPLEMENTS targets = %v, want %v", got, want)
	}
}
