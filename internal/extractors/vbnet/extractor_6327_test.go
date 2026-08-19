package vbnet_test

// S5 of #6327 — the edge half of VB.NET support.
//
// These tests pin the four edge kinds the epic names and, more importantly,
// the two contracts that cost the previous five languages a bug each:
//
//   - EXTENDS/IMPLEMENTS are anchored on the TYPE, with FromID left EMPTY so
//     graph assembly stamps the owning record's id (#6295, #6298, #6365,
//     #6367). internal/extractors/file_anchored_rels_guard_test.go is the
//     source-level half of the same rule; these are the behavioural half.
//   - IMPORTS hang on the per-file SCOPE.Component carrier and emit NO
//     per-import placeholder entity (#742 / #6368 / #6369).

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// run drives the registered vbnet extractor over one file.
func run(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("vbnet")
	if !ok {
		t.Fatal("vbnet extractor not registered — registry_gen.go blank import missing")
	}
	ents, err := ext.Extract(t.Context(), extractor.FileInput{
		Path: path, Content: []byte(src), Language: "vbnet",
	})
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	return ents
}

// find returns the single record with the given kind and name.
func find(t *testing.T, ents []types.EntityRecord, kind, name string) *types.EntityRecord {
	t.Helper()
	var hits []*types.EntityRecord
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			hits = append(hits, &ents[i])
		}
	}
	if len(hits) != 1 {
		var got []string
		for i := range ents {
			got = append(got, ents[i].Kind+"/"+ents[i].Subtype+"/"+ents[i].Name)
		}
		sort.Strings(got)
		t.Fatalf("want exactly 1 %s named %q, got %d; entities: %v", kind, name, len(hits), got)
	}
	return hits[0]
}

// edges returns "KIND->ToID" for every relationship of the given kinds.
func edges(rec *types.EntityRecord, kinds ...string) []string {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var out []string
	for _, r := range rec.Relationships {
		if want[r.Kind] {
			out = append(out, r.Kind+"->"+r.ToID)
		}
	}
	sort.Strings(out)
	return out
}

const twoTypes = `Imports System.Windows.Forms

Namespace App
    Public Class MainForm
        Inherits System.Windows.Forms.Form
        Implements IDisposable, IComparable

        Public Sub Load()
            Helper.Run()
            Compute(1)
        End Sub
    End Class

    Public Class Helper
        Inherits Base

        Public Shared Sub Run()
        End Sub
    End Class
End Namespace
`

// TestHierarchyEdgesAnchorOnTheTypeNotTheFile is the #6295/#6298/#6365/#6367
// contract. Two types in one file each carry their own bases; if the edges
// were file-anchored they would merge onto the file carrier and MainForm's
// bases would be indistinguishable from Helper's.
func TestHierarchyEdgesAnchorOnTheTypeNotTheFile(t *testing.T) {
	ents := run(t, "src/MainForm.vb", twoTypes)

	main := find(t, ents, "SCOPE.Component", "MainForm")
	if got, want := edges(main, "EXTENDS", "IMPLEMENTS"), []string{
		"EXTENDS->System.Windows.Forms.Form",
		"IMPLEMENTS->IComparable",
		"IMPLEMENTS->IDisposable",
	}; !equal(got, want) {
		t.Errorf("MainForm hierarchy edges = %v, want %v", got, want)
	}

	helper := find(t, ents, "SCOPE.Component", "Helper")
	if got, want := edges(helper, "EXTENDS", "IMPLEMENTS"), []string{"EXTENDS->Base"}; !equal(got, want) {
		t.Errorf("Helper hierarchy edges = %v, want %v", got, want)
	}

	// FromID must be EMPTY: both graph-assembly paths (cmd/grafel/index.go and
	// internal/extractors/incremental.go) substitute the owning record's id
	// only when FromID == "". A path-valued FromID would be rewritten onto the
	// file carrier — the defect this test exists to prevent.
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind != "EXTENDS" && r.Kind != "IMPLEMENTS" {
				continue
			}
			if r.FromID != "" {
				t.Errorf("%s %s edge on %q carries FromID=%q; it must be EMPTY so "+
					"assembly stamps the owning TYPE's id (#6295/#6298)",
					r.Kind, r.ToID, ents[i].Name, r.FromID)
			}
		}
	}

	// And no hierarchy edge may sit on the file carrier.
	fileEnt := find(t, ents, "SCOPE.Component", "src/MainForm.vb")
	if got := edges(fileEnt, "EXTENDS", "IMPLEMENTS"); len(got) != 0 {
		t.Errorf("file carrier hosts hierarchy edges %v; they belong on the type", got)
	}
}

// TestImportsHangOnTheFileCarrierWithNoPlaceholder is the #742 / #6368 / #6369
// contract: no per-import entity, because such an entity collides by
// graph.EntityID with a same-named declaration in the same file.
func TestImportsHangOnTheFileCarrierWithNoPlaceholder(t *testing.T) {
	src := `Imports System.Text
Imports IO = System.IO

Public Class Text
End Class
`
	ents := run(t, "src/Thing.vb", src)

	fileEnt := find(t, ents, "SCOPE.Component", "src/Thing.vb")
	var hosted int
	for _, r := range fileEnt.Relationships {
		if r.Kind == "IMPORTS" {
			hosted++
			if r.FromID != "src/Thing.vb" {
				t.Errorf("IMPORTS FromID = %q, want the importing file's path — "+
					"BuildImportTable keys the per-file binding on it", r.FromID)
			}
		}
	}
	if hosted != 2 {
		t.Errorf("file carrier hosts %d IMPORTS edges, want 2", hosted)
	}
	var total int
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == "IMPORTS" {
				total++
			}
		}
	}
	if total != hosted {
		t.Errorf("%d IMPORTS edges exist but only %d sit on the file carrier (#6368)", total, hosted)
	}

	// `Public Class Text` and `Imports System.Text` share the leaf name "Text".
	// A placeholder named after the leaf would hash to the same EntityID as the
	// class (graph.EntityID excludes Subtype and the span) — variant 5 of #6368.
	texts := 0
	for i := range ents {
		if ents[i].Name == "Text" {
			texts++
		}
	}
	if texts != 1 {
		t.Errorf("%d entities named \"Text\"; want exactly the class declaration — "+
			"a per-import placeholder collides by graph.EntityID (#6368)", texts)
	}

	// The alias form keeps the alias as the local name and the RHS as the module.
	var aliased bool
	for _, r := range fileEnt.Relationships {
		if r.Kind == "IMPORTS" && r.Properties.Get("local_name") == "IO" {
			aliased = true
			if got := r.Properties.Get("source_module"); got != "System.IO" {
				t.Errorf("aliased import source_module = %q, want %q", got, "System.IO")
			}
		}
	}
	if !aliased {
		t.Error("no IMPORTS edge with local_name=IO for `Imports IO = System.IO`")
	}
}

// TestCallsComeFromTheDisambiguatorNotFromShape pins the three properties of
// Ref that S4 spent three review rounds establishing.
func TestCallsComeFromTheDisambiguatorNotFromShape(t *testing.T) {
	src := `Public Class C
    Private buf(10) As Integer
    Private items As New List(Of String)

    Public Sub Go()
        Dim n As Integer = buf(3)
        Dim s As String = CType(items, Object).ToString()
        Compute(n)
        Me.Helper(n)
        With items
            .Add("x")
        End With
    End Sub

    Private Function Compute(v As Integer) As Integer
        Return v
    End Function

    Private Sub Helper(v As Integer)
    End Sub
End Class
`
	ents := run(t, "src/C.vb", src)
	goM := find(t, ents, "SCOPE.Operation", "C.Go")
	got := edges(goM, "CALLS")

	// buf(3) is an ARRAY INDEX — the declaration table says so.
	for _, e := range got {
		if strings.HasSuffix(e, "->buf") {
			t.Errorf("CALLS->buf emitted: `Private buf(10) As Integer` makes buf(3) "+
				"an index, not a call; edges=%v", got)
		}
	}
	// CType is an intrinsic: call-SHAPED, resolves to no declaration. One
	// designer file in the corpus contains hundreds.
	for _, e := range got {
		if strings.HasSuffix(e, "->CType") {
			t.Errorf("CALLS->CType emitted: intrinsics must not produce CALLS "+
				"(Ref.Intrinsic); edges=%v", got)
		}
	}
	// `.Add("x")` inside a `With` block is Qualified with an unnameable
	// receiver. Ref.IsCall() is false for it, and S5 must NOT route around
	// that by resolving Add against this file's declarations.
	for _, e := range got {
		if strings.HasSuffix(e, "->Add") {
			t.Errorf("CALLS->Add emitted from a With-block member access: the "+
				"receiver is real but unnameable by a per-file pass "+
				"(Ref.Qualified with Qualifier==\"\"); edges=%v", got)
		}
	}
	// The two real calls survive. `Me.Helper` reduces to the bare member:
	// Me is not a nameable entity, and vbnet's own classifier already resolves
	// Me.Foo in the enclosing TYPE's scope rather than the local one.
	want := map[string]bool{"CALLS->Compute": true, "CALLS->Helper": true}
	seen := map[string]bool{}
	for _, e := range got {
		seen[e] = true
	}
	for w := range want {
		if !seen[w] {
			t.Errorf("missing %s; edges=%v", w, got)
		}
	}
	// CALLS anchor on the METHOD, not the file.
	for _, r := range goM.Relationships {
		if r.Kind == "CALLS" && r.FromID != "" {
			t.Errorf("CALLS FromID = %q, want empty so assembly stamps C.Go", r.FromID)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
