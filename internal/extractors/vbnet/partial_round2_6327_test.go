package vbnet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// Round 2 of #6327 S7a. Four properties the round-1 code has but does not
// pin, plus one it does not have.

// TestPickSiblingRefusesAnAmbiguousCaseFoldedSet drives the ambiguity arm
// directly, with a SYNTHETIC listing.
//
// TestSiblingPathRefusesAnAmbiguousCaseFoldedSet builds the situation on a real
// filesystem and therefore skips on macOS and Windows, where APFS/NTFS collapse
// `Widget.VB` and `Widget.Vb` into one entry. The refusal arm was consequently
// unreachable outside Linux CI: mutating `n != 1` to `n == 0` survived a full
// local run at exit 0. Splitting the pure decision out of the directory read
// makes the arm testable on every platform.
func TestPickSiblingRefusesAnAmbiguousCaseFoldedSet(t *testing.T) {
	for _, tc := range []struct {
		name  string
		names []string
		want  string
		ok    bool
	}{
		{"exact match wins outright", []string{"Widget.VB", "Widget.vb", "Widget.Vb"}, "Widget.vb", true},
		{"a single fold is taken", []string{"other.txt", "Widget.VB"}, "Widget.VB", true},
		{"two folds are refused", []string{"Widget.VB", "Widget.Vb"}, "", false},
		{"three folds are refused", []string{"Widget.VB", "Widget.Vb", "WIDGET.VB"}, "", false},
		{"no candidate at all", []string{"Gadget.vb"}, "", false},
		{"empty listing", nil, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := pickSibling(tc.names, "Widget.vb")
			if ok != tc.ok || got != tc.want {
				t.Errorf("pickSibling(%v) = %q,%v; want %q,%v", tc.names, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestNestedNamespaceSiblingMerges pins the namespace CONCATENATION in
// typeDecls. emit keys a type nested in `Namespace A / Namespace B` under the
// joined path `A.B`, so typeDecls must join too. Dropping the join keys the
// sibling's declaration under the innermost segment `B` alone, nothing ever
// matches, and every merge in a nested-namespace file is silently lost — which
// the round-1 tests did not notice, because they all use a single namespace
// level or none.
func TestNestedNamespaceSiblingMerges(t *testing.T) {
	const body = `Namespace A
    Namespace B
        Partial Public Class Widget
            Public Sub Run()
            End Sub
        End Class
    End Namespace
End Namespace
`
	const designer = `Namespace A
    Namespace B
        Partial Class Widget
            Private Sub InitializeComponent()
            End Sub
        End Class
    End Namespace
End Namespace
`
	files := map[string]string{"Widget.vb": body, "Widget.Designer.vb": designer}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)
	if ids := componentIDs(recs, "Widget"); len(ids) != 1 {
		t.Errorf("a nested-namespace partial pair must derive ONE id, got %d (%v)", len(ids), sourceFilesOf(recs, "Widget"))
	}
}

// TestPartialTypeNestedInsideAnotherTypeMerges pins typeDecls' recursion INTO a
// type. emit walks through a Class or Module into its members, so a partial type
// declared inside one is emitted with the enclosing namespace and is a merge
// candidate like any other. typeDecls must see it too. Deleting its recursive
// call leaves the sibling's set holding only top-level types, so the inner half
// never merges. Every round-1 fixture declares its partial type at the top
// level, so the call was unpinned.
func TestPartialTypeNestedInsideAnotherTypeMerges(t *testing.T) {
	const body = `Public Class Outer
    Partial Public Class Widget
        Public Sub Run()
        End Sub
    End Class
End Class
`
	const designer = `Public Class Outer
    Partial Class Widget
        Private Sub InitializeComponent()
        End Sub
    End Class
End Class
`
	files := map[string]string{"Widget.vb": body, "Widget.Designer.vb": designer}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)
	if ids := componentIDs(recs, "Widget"); len(ids) != 1 {
		t.Errorf("a nested partial pair must derive ONE id, got %d (%v)", len(ids), sourceFilesOf(recs, "Widget"))
	}
}

// TestNamespaceCaseDifferenceStillMerges: VB.NET is case-insensitive, so
// `Namespace ALPHA` and `Namespace Alpha` ARE the same namespace and a partial
// type split across the two is ONE type. Comparing the namespace exactly
// dropped that merge.
//
// The name is still compared exactly, and deliberately: the name IS part of the
// identity triple graph.EntityID hashes, so two case-differing names derive two
// ids no matter which file they claim, and re-anchoring would only move a
// declaration onto a file whose declaration keeps its own id.
func TestNamespaceCaseDifferenceStillMerges(t *testing.T) {
	const body = `Namespace ALPHA
    Partial Public Class Widget
        Public Sub Run()
        End Sub
    End Class
End Namespace
`
	const designer = `Namespace Alpha
    Partial Class Widget
        Private Sub InitializeComponent()
        End Sub
    End Class
End Namespace
`
	files := map[string]string{"Widget.vb": body, "Widget.Designer.vb": designer}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)
	if ids := componentIDs(recs, "Widget"); len(ids) != 1 {
		t.Errorf("ALPHA and Alpha are ONE namespace in VB.NET; want ONE id, got %d (%v)", len(ids), sourceFilesOf(recs, "Widget"))
	}
}

// TestNameCaseDifferenceDoesNotMerge is the other half of the decision above.
// `Widget` and `WIDGET` derive different ids, so re-anchoring merges nothing
// and would only cost the designer half its span.
func TestNameCaseDifferenceDoesNotMerge(t *testing.T) {
	files := map[string]string{
		"Widget.vb":          "Partial Public Class WIDGET\n    Public Sub Run()\n    End Sub\nEnd Class\n",
		"Widget.Designer.vb": "Partial Class Widget\n    Private Sub InitializeComponent()\n    End Sub\nEnd Class\n",
	}
	root := writeRepo(t, files)
	recs := extractRepo(t, root, files)
	for _, r := range recs {
		if r.Kind != "SCOPE.Component" || r.Name != "Widget" {
			continue
		}
		if r.SourceFile != "Widget.Designer.vb" {
			t.Errorf("Widget re-anchored to %q, which declares WIDGET — a different id", r.SourceFile)
		}
		if r.StartLine == 0 {
			t.Error("Widget lost its span to a rewrite that merged nothing")
		}
	}
}

// TestSymlinkedSiblingIsUsedAsAnchor documents the SYMLINK decision.
//
// A symlink to a regular file is accepted as an anchor. The rule this module
// enforces is not "is a regular file" but "os.ReadFile returns rather than
// blocks", and the indexer's own walker agrees: filepath.WalkDir reports a
// symlink-to-file as a file and hands it to the extractor (walker.go:136-233
// branches only on d.IsDir()). Rejecting it here would refuse an anchor the
// walker DOES index — a file entity would claim `Widget.vb` while no component
// did. A symlink to a directory or a FIFO is still refused, because those are
// the shapes that fail or block.
func TestSymlinkedSiblingIsUsedAsAnchor(t *testing.T) {
	files := map[string]string{
		"Widget.Designer.vb": "Partial Class Widget\n    Private Sub InitializeComponent()\n    End Sub\nEnd Class\n",
	}
	root := writeRepo(t, files)
	real := filepath.Join(root, "real_widget.txt")
	if err := os.WriteFile(real, []byte("Partial Public Class Widget\n    Public Sub Run()\n    End Sub\nEnd Class\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "Widget.vb")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	recs := extractRepo(t, root, files)
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "Widget" && r.SourceFile != "Widget.vb" {
			t.Errorf("a symlinked sibling that declares Widget must anchor it; got %q", r.SourceFile)
		}
	}
}

// sourceFilesOf reports the source files every same-named component claims,
// which is the only useful thing to print when an id count is wrong.
func sourceFilesOf(recs []types.EntityRecord, name string) []string {
	var out []string
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == name {
			out = append(out, r.SourceFile)
		}
	}
	return out
}
