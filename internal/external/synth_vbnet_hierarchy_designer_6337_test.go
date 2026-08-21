package external

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	vbextractor "github.com/cajasmota/grafel/internal/extractors/vbnet"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// #6473 round 5 — the two pins in this file both cross the package boundary on
// purpose. Everything else in the 6337 suite hand-builds a graph.Document, so
// every assumption it makes about what the VB.NET EXTRACTOR actually emits is
// unobserved. Two of those assumptions were load-bearing and neither was
// pinned; both are pinned here by driving the real extractor.

// vbExtractToEntities runs the REAL vbnet extractor over a small on-disk repo
// and projects its records the way the indexer does.
//
// The projection is deliberately narrow: entityRecordToGraphEntity
// (internal/extractors/incremental.go:1779) copies Name, Kind, SourceFile and
// Language straight through from the record, and those four are exactly what
// buildInTreeNameSet reads. Nothing else in graph.Entity is observable from
// gate 3, so nothing else is carried. That package cannot be imported here —
// internal/extractors imports internal/external — but the four assignments it
// makes are one-line pass-throughs, not logic.
func vbExtractToEntities(t *testing.T, files map[string]string) []graph.Entity {
	t.Helper()
	root := t.TempDir()
	for p, src := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", p, err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	var ents []graph.Entity
	e := &vbextractor.Extractor{}
	for p, src := range files {
		recs, err := e.Extract(context.Background(), extractor.FileInput{
			Path: p, Content: []byte(src), RepoRoot: root,
		})
		if err != nil {
			t.Fatalf("extract %s: %v", p, err)
		}
		for _, r := range recs {
			ents = append(ents, graph.Entity{
				ID:         "id:" + r.Kind + ":" + r.Name + ":" + r.SourceFile,
				Name:       r.Name,
				Kind:       r.Kind,
				SourceFile: r.SourceFile,
				Language:   r.Language,
			})
		}
	}
	return ents
}

// designerPanel is the WinForms designer half: the `DesignerGenerated` attribute
// and a `Partial Class` declaring the type, which is the shape
// internal/extractors/vbnet/partial.go keys its merge on.
const designerPanel = `<Global.Microsoft.VisualBasic.CompilerServices.DesignerGenerated()> _
Partial Class Panel
    Private Sub InitializeComponent()
    End Sub
End Class
`

// mainPanel is the hand-written half of the same partial type.
const mainPanel = `Partial Public Class Panel
    Public Sub Refresh()
    End Sub
End Class
`

// TestVBNetHierarchyMaskGuardSeesDesignerAnchoredEntities_6337 kills review
// mutant MW5 (#6473), which made buildInTreeNameSet skip every entity whose
// SourceFile ends `.Designer.vb`. That mutant survived the whole suite, which
// was pointed, because gate 3's stated purpose names the designer split
// directly: "a partial class split across `Foo.vb` and `Foo.Designer.vb`"
// (resolve/vbnet_hierarchy_target_6337.go:21).
//
// # The stated premise is FALSE, and this test pins that too
//
// Driving the real extractor over a genuine designer PAIR shows why no test
// could have been written to that premise. #6327's S7a merge re-anchors the
// designer half's Component onto the sibling's path, so the pair yields ONE
// `Panel` entity whose SourceFile is `Forms/Panel.vb` — the `.Designer.vb`
// spelling does not survive on any entity that carries the TYPE's name. A
// suffix test on SourceFile is therefore an EQUIVALENT mutation for the merged
// split, and a test built on that premise would have gone green under MW5.
//
// # What MW5 actually blinds
//
// The merge requires a sibling that declares the same type, and MEASURED on the
// 302-file corpus only 33 of 88 `*.Designer.vb` files have one
// (internal/extractors/vbnet/partial.go). The other 55 —
// `My Project/Settings.Designer.vb`, `Resources.Designer.vb`,
// `Application.Designer.vb`, the localized `Strings.<culture>.Designer.vb`
// family — are generated STANDALONE, are correctly left un-re-anchored, and
// keep a `.Designer.vb` SourceFile on the type they declare. Those are the
// entities MW5 deletes from gate 3's view, and they are the majority of the
// designer population, not an edge case.
//
// So gate 3's real subject is not the split pair the comment named. It is any
// in-tree declaration of an allowlisted framework name, and generated code is
// where such a name is most likely to appear without a human having chosen it.
func TestVBNetHierarchyMaskGuardSeesDesignerAnchoredEntities_6337(t *testing.T) {
	cases := []struct {
		name       string
		files      map[string]string
		wantAnchor string
		why        string
	}{
		{
			name:       "standalone designer file keeps its .Designer.vb anchor",
			files:      map[string]string{"Forms/Panel.Designer.vb": designerPanel},
			wantAnchor: "Forms/Panel.Designer.vb",
			why: "no `.vb` sibling declares `Panel`, so partial.go's sibling guard " +
				"declines to re-anchor — the 55/88 standalone-generated shape",
		},
		{
			name: "designer PAIR re-anchors onto the hand-written half",
			files: map[string]string{
				"Forms/Panel.vb":          mainPanel,
				"Forms/Panel.Designer.vb": designerPanel,
			},
			wantAnchor: "Forms/Panel.vb",
			why: "#6327 S7a merges the split, so the `.Designer.vb` spelling never " +
				"reaches gate 3 on a type-named entity — this is why the guard's " +
				"stated purpose could not be tested as stated",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents := vbExtractToEntities(t, tc.files)

			// The producibility half. Without this the Synthesize assertion
			// below could pass on a hand-built entity the indexer can never
			// emit, which is the failure mode AGENTS.md calls decoration.
			//
			// Keyed on the DISTINCT anchor set, not the record count: the pair
			// extracts per-file and so emits the merged Component twice, once
			// from each half. That duplication is exactly what S7a's identity
			// rewrite exists to make collapsible — graph.EntityID hashes
			// (repo, kind, name, source_file), so two records agreeing on the
			// anchor mint ONE id and buildDocument's dedup folds them. What is
			// under test here is the anchor those records agree on.
			anchors := map[string]bool{}
			for _, e := range ents {
				if e.Name == "Panel" && e.Kind == "SCOPE.Component" {
					anchors[e.SourceFile] = true
				}
			}
			if len(anchors) != 1 || !anchors[tc.wantAnchor] {
				t.Fatalf("extractor emitted `Panel` Components anchored at %v, want exactly {%q}\n  %s",
					anchors, tc.wantAnchor, tc.why)
			}

			// The guard half. `Panel` is in vbExternalBaseTypes, so gate 2
			// passes and gate 3 is the only thing that can stop the synthesis.
			doc := vbDoc("EXTENDS", "Panel", ents...)
			Synthesize(doc)
			if got := doc.Relationships[0].ToID; got != "Panel" {
				t.Errorf("`Inherits Panel` against an in-tree `Panel` declared in %s: ToID = %q, want it left unresolved as %q\n"+
					"  gate 3 must see this declaration: the edge is the partial-class ambiguity, not an external base",
					tc.wantAnchor, got, "Panel")
			}
		})
	}
}

// TestVBNetExtractorLanguageStampMatchesConst_6337 is the pin item 2 of #6473
// round 5 asked for, and it is the pin the comment on vbnetLanguage USED to
// claim already existed.
//
// vbnetLanguage is read in two places that both matter: the lang gate at the
// top of vbnetHierarchyExternal, and the `e.Language == vbnetLanguage` test
// that populates inTreeNameSet.vbFold. Both compare against a string the VB.NET
// EXTRACTOR stamps, in a different package (internal/extractors/vbnet's own
// unexported `lang`), via extractor.TagEntitiesLanguage.
//
// Every other test in this suite hardcodes the literal `"vbnet"` on hand-built
// entities, so all of them would stay green if the extractor's stamp drifted:
// vbFold would be silently empty, the whole round-3 case fix would become a
// no-op, and the arm's lang gate would reject every real VB.NET edge. Naming
// the constant once does NOT keep the two in step — only an assertion across
// the boundary does, and this is it.
//
// It asserts the STAMP rather than only comparing the exported accessor,
// because the stamp is what reaches graph.Entity.Language. Mutation picked the
// shape: drifting the extractor's `lang` const dies in internal/resolve anyway
// (TestVBExternalBaseTypesAreLoadBearing and two siblings), but drifting ONLY
// the entity stamp at extractor.go's emit site left internal/external AND
// internal/resolve both green — the exact "vbFold is silently empty" failure,
// invisible to every constant-level check.
func TestVBNetExtractorLanguageStampMatchesConst_6337(t *testing.T) {
	if got := (&vbextractor.Extractor{}).Language(); got != vbnetLanguage {
		t.Errorf("vbnet Extractor.Language() = %q, want %q (internal/external's vbnetLanguage)", got, vbnetLanguage)
	}

	ents := vbExtractToEntities(t, map[string]string{"Forms/Panel.vb": mainPanel})
	if len(ents) == 0 {
		t.Fatal("extractor emitted no entities; the stamp assertion below would be vacuous")
	}
	var typed int
	for _, e := range ents {
		if e.Language != vbnetLanguage {
			t.Errorf("entity %q (%s) stamped Language %q, want %q — the extractor's slug and "+
				"internal/external's vbnetLanguage have drifted, which silently empties inTreeNameSet.vbFold",
				e.Name, e.Kind, e.Language, vbnetLanguage)
		}
		if e.Kind == "SCOPE.Component" && e.Name == "Panel" {
			typed++
		}
	}
	if typed != 1 {
		t.Fatalf("expected exactly one `Panel` Component to carry the stamp, got %d", typed)
	}

	// The relationship half of the same stamp. relLanguage
	// (internal/resolve/refs.go) reads Properties["language"] off each
	// RELATIONSHIP, and that is the value that reaches this arm's `lang`
	// parameter — a different TagRelationshipsLanguage call, so a drift in one
	// is not a drift in the other.
	root := t.TempDir()
	const rel = "Public Class Widget\n    Inherits System.Windows.Forms.Form\nEnd Class\n"
	if err := os.WriteFile(filepath.Join(root, "Widget.vb"), []byte(rel), 0o644); err != nil {
		t.Fatal(err)
	}
	recs, err := (&vbextractor.Extractor{}).Extract(context.Background(), extractor.FileInput{
		Path: "Widget.vb", Content: []byte(rel), RepoRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	var seen int
	for _, r := range recs {
		for _, rl := range r.Relationships {
			if rl.Kind != string(types.RelationshipKindExtends) {
				continue
			}
			seen++
			if got := rl.Properties.Get("language"); got != vbnetLanguage {
				t.Errorf("EXTENDS %q carries language property %q, want %q — this is the value that "+
					"reaches vbnetHierarchyExternal's lang gate, and a drift makes the arm unreachable",
					rl.ToID, got, vbnetLanguage)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no EXTENDS relationship emitted; the language-property assertion was vacuous")
	}
}
