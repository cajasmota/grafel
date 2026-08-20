package vbnet

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// S7a of #6327. Two assertions that must BOTH hold, because either one alone
// is satisfiable by a broken implementation:
//
//   - a partial class split across a WinForms designer pair collapses to ONE
//     SCOPE.Component carrying the members of both halves;
//   - two genuinely distinct types that merely share a name still produce TWO.
//
// The second is not padding. A merge keyed on the NAME would pass the first
// test and silently fuse the seven `My.MySettings` classes the measured corpus
// contains — one per project — into a single node. That is a worse defect than
// the duplication being fixed, so it is pinned here.

const testRepo = "testrepo"

// writeRepo materialises files under a temp dir and returns the root. The
// merge is gated on the anchor sibling EXISTING, so these tests cannot be run
// on in-memory sources alone.
func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// extractRepo runs the extractor over every named file, in the SORTED order
// the indexer uses, and returns the flattened records.
func extractRepo(t *testing.T, root string, files map[string]string) []types.EntityRecord {
	t.Helper()
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	var out []types.EntityRecord
	for _, rel := range rels {
		out = append(out, extractVBNet(files[rel], rel, root)...)
	}
	return out
}

// componentIDs returns the distinct derived graph.EntityIDs of every
// SCOPE.Component with the given name. Derived, not read off the record:
// entityRecordToGraphEntity ignores EntityRecord.ID entirely (#6150), so the
// derived id is the only identity that reaches the graph.
func componentIDs(recs []types.EntityRecord, name string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, r := range recs {
		if r.Kind != "SCOPE.Component" || r.Name != name {
			continue
		}
		id := graph.EntityID(testRepo, r.Kind, r.Name, r.SourceFile)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func containsTargets(recs []types.EntityRecord, ownerName string) []string {
	var out []string
	for _, r := range recs {
		if r.Kind != "SCOPE.Component" || r.Name != ownerName {
			continue
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "CONTAINS" {
				out = append(out, rel.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

const form1Code = `Public Class Form1
    Private Sub Button1_Click()
        Close()
    End Sub
End Class
`

const form1Designer = `<Global.Microsoft.VisualBasic.CompilerServices.DesignerGenerated()> _
Partial Class Form1
    Inherits System.Windows.Forms.Form

    Friend WithEvents Button1 As System.Windows.Forms.Button

    Private Sub InitializeComponent()
        Me.Button1 = New System.Windows.Forms.Button()
    End Sub
End Class
`

// TestPartialDesignerPairMergesToOneComponent is direction one.
func TestPartialDesignerPairMergesToOneComponent(t *testing.T) {
	files := map[string]string{
		"Forms/Form1.vb":          form1Code,
		"Forms/Form1.Designer.vb": form1Designer,
	}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)

	ids := componentIDs(recs, "Form1")
	if len(ids) != 1 {
		var got []string
		for _, r := range recs {
			if r.Kind == "SCOPE.Component" && r.Name == "Form1" {
				got = append(got, r.SourceFile)
			}
		}
		t.Fatalf("split partial class must derive ONE graph.EntityID, got %d (source files %v)", len(ids), got)
	}

	// Both halves must claim the NON-designer sibling, which is what makes the
	// ids agree in the first place.
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Form1" && r.SourceFile != "Forms/Form1.vb" {
			t.Errorf("Form1 component anchored at %q, want %q", r.SourceFile, "Forms/Form1.vb")
		}
	}

	// Members from BOTH files must hang off it. The designer half's member
	// keeps its own real source file; only the CONTAINS owner moved.
	targets := containsTargets(recs, "Form1")
	for _, want := range []string{
		extractor.BuildOperationStructuralRef(lang, "Forms/Form1.vb", "Form1.Button1_Click"),
		extractor.BuildOperationStructuralRef(lang, "Forms/Form1.Designer.vb", "Form1.InitializeComponent"),
	} {
		found := false
		for _, got := range targets {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("merged Form1 is missing CONTAINS -> %q; got %v", want, targets)
		}
	}

	// The rewritten half drops its span so the fold — which fills a span only
	// when it is ZERO — cannot stamp designer line numbers onto Form1.vb
	// regardless of which record wins the survivor slot.
	var designerSpanned, anchorSpanned bool
	for _, r := range recs {
		if r.Kind != "SCOPE.Component" || r.Name != "Form1" {
			continue
		}
		if r.StartLine != 0 {
			anchorSpanned = true
		}
	}
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Form1" && r.StartLine != 0 && r.EndLine > 6 {
			designerSpanned = true
		}
	}
	if !anchorSpanned {
		t.Error("merged Form1 has no non-zero span at all; the anchor half must keep its own")
	}
	if designerSpanned {
		t.Error("the designer half kept a span; it would overwrite Form1.vb's lines depending on fold order")
	}
}

// TestSameNamedPartialsInDifferentProjectsStayDistinct is direction two: the
// shape a name-keyed merge would destroy. Seven of these exist in the measured
// corpus, all `Partial Friend NotInheritable Class MySettings` inside
// `Namespace My`, one per project.
func TestSameNamedPartialsInDifferentProjectsStayDistinct(t *testing.T) {
	const settings = `Namespace My
    Partial Friend NotInheritable Class MySettings
        Inherits Global.System.Configuration.ApplicationSettingsBase
        Private Shared defaultInstance As MySettings
    End Class
End Namespace
`
	files := map[string]string{
		"WOL/My Project/Settings.Designer.vb":            settings,
		"EventLogViewer/My Project/Settings.Designer.vb": settings,
	}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)

	ids := componentIDs(recs, "MySettings")
	if len(ids) != 2 {
		t.Fatalf("two same-named classes in two projects must stay TWO components, got %d", len(ids))
	}
	// Neither may have been re-anchored: there is no `Settings.vb` sibling for
	// either, and a rewrite would point both at a file that does not exist.
	for _, r := range recs {
		if r.Kind != "SCOPE.Component" || r.Name != "MySettings" {
			continue
		}
		if !strings.HasSuffix(r.SourceFile, "Settings.Designer.vb") {
			t.Errorf("orphan designer re-anchored to %q; no sibling exists", r.SourceFile)
		}
		if r.StartLine == 0 {
			t.Errorf("orphan designer at %q lost its span", r.SourceFile)
		}
	}
}

// TestSameNamedClassesInDifferentNamespacesStayDistinct pins the non-designer
// half of direction two.
func TestSameNamedClassesInDifferentNamespacesStayDistinct(t *testing.T) {
	files := map[string]string{
		"Alpha/Widget.vb": "Namespace Alpha\n    Public Class Widget\n        Public Sub Run()\n        End Sub\n    End Class\nEnd Namespace\n",
		"Beta/Widget.vb":  "Namespace Beta\n    Public Class Widget\n        Public Sub Run()\n        End Sub\n    End Class\nEnd Namespace\n",
	}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)
	if ids := componentIDs(recs, "Widget"); len(ids) != 2 {
		t.Fatalf("Alpha.Widget and Beta.Widget must stay TWO components, got %d", len(ids))
	}
}

// TestNonPartialDesignerFileIsNotReanchored pins the modifier gate. A designer
// file may declare a type of its own; only a `Partial` one has another half.
func TestNonPartialDesignerFileIsNotReanchored(t *testing.T) {
	files := map[string]string{
		"Forms/Panel1.vb":          "Public Class Panel1\nEnd Class\n",
		"Forms/Panel1.Designer.vb": "Friend Class Panel1Helper\n    Public Sub Init()\n    End Sub\nEnd Class\n",
	}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Panel1Helper" {
			if r.SourceFile != "Forms/Panel1.Designer.vb" {
				t.Errorf("non-partial designer type re-anchored to %q", r.SourceFile)
			}
			if r.StartLine == 0 {
				t.Error("non-partial designer type lost its span")
			}
		}
	}
}

// TestPartialAnchorWithoutRepoRootDoesNotRewrite pins the guard that keeps the
// unverifiable case honest: with no repo root the sibling cannot be stat'ed,
// and an unverified rewrite is exactly what the existence guard prevents.
func TestPartialAnchorWithoutRepoRootDoesNotRewrite(t *testing.T) {
	recs := extractVBNet(form1Designer, "Forms/Form1.Designer.vb", "")
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Form1" && r.SourceFile != "Forms/Form1.Designer.vb" {
			t.Errorf("rewrote to %q with no repo root to verify the sibling", r.SourceFile)
		}
	}
}

func TestDesignerAnchorPath(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"Forms/Form1.Designer.vb", "Forms/Form1.vb", true},
		{"Forms/Form1.designer.vb", "Forms/Form1.vb", true},
		{"Forms/Form1.vb", "", false},
		{"Forms/.Designer.vb", "", false},
		{".Designer.vb", "", false},
		{"Forms/Strings.nl.Designer.vb", "Forms/Strings.nl.vb", true},
	}
	for _, c := range cases {
		got, ok := designerAnchorPath(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("designerAnchorPath(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}
