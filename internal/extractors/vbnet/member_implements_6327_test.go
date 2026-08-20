package vbnet_test

// S7c of #6327 — method/property/event-level `Implements IFoo.Bar` -> IMPLEMENTS.
//
// The load-bearing gate. `.github/workflows/quality.yml` is dispatch-only, so
// the golden delta on internal/quality/golden/vbnet-mini does NOT run in CI and
// cannot be what protects this behaviour.
//
// What is asserted, and why each assertion is separate from the others:
//
//   - the member edge EXISTS, on the MEMBER record, not the type's;
//   - it does NOT displace the type-level edge — the two coexist with
//     different endpoints, which is the whole answer to the "competes with the
//     type-level edge" objection extractor.go used to record;
//   - it carries `via=implements-member`, so a consumer can separate the two
//     subsets WITHOUT resolving either endpoint;
//   - FromID stays EMPTY (#6295/#6298/#6365/#6367 defect class);
//   - the target keeps its dotted prefix and loses its `(Of ...)` arguments,
//     exactly as baseTypeName already does for the type-level clause.

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// memberImplSrc carries all three member kinds that parse an Implements
// clause (Sub/Function, Property, Event), a multi-target clause, a generic
// interface, and a `Global.` escape.
const memberImplSrc = `Namespace App
    Public Interface IWorker
        Sub Run()
        Event Done()
    End Interface

    Public Class Worker
        Implements IWorker, IDisposable

        Public Event Finished() Implements IWorker.Done

        Public Property Name As String Implements IThing.Name, IOther.Name

        Public Sub Go() Implements IWorker.Run
        End Sub

        Public Sub Dispose() Implements Global.System.IDisposable.Dispose
        End Sub

        Public Function Cmp(o As Object) As Integer Implements IComparable(Of Command).CompareTo
        End Function

        Public Sub Plain()
        End Sub
    End Class
End Namespace
`

// implEdges renders every IMPLEMENTS edge of a record as
// "ToID|via|line", sorted, so an assertion reads as one string per edge.
func implEdges(rec *types.EntityRecord) []string {
	var out []string
	for _, r := range rec.Relationships {
		if r.Kind != "IMPLEMENTS" {
			continue
		}
		out = append(out, r.ToID+"|"+r.Properties.Get("via")+"|"+r.Properties.Get("line"))
	}
	sort.Strings(out)
	return out
}

func TestMemberImplementsEmitsEdgeOnTheMember_6327(t *testing.T) {
	ents := run(t, "app/Worker.vb", memberImplSrc)

	cases := []struct {
		member string
		want   []string
	}{
		{"Worker.Go", []string{"IWorker.Run|implements-member|14"}},
		{"Worker.Finished", []string{"IWorker.Done|implements-member|10"}},
		// Multi-target: splitNames already handles it, so it is supported for
		// free. The measured corpus contains none, so this buys no edges
		// there — it is pinned because the code path exists, not as a claim.
		{"Worker.Name", []string{
			"IOther.Name|implements-member|12",
			"IThing.Name|implements-member|12",
		}},
		// `Global.` is a root-namespace escape, stripped case-folded; the
		// remaining dotted prefix is KEPT, as for the type-level clause.
		{"Worker.Dispose", []string{"System.IDisposable.Dispose|implements-member|17"}},
		// `(Of ...)` is a type-ARGUMENT list on the interface, not part of the
		// name, and the member tail survives the trim.
		{"Worker.Cmp", []string{"IComparable.CompareTo|implements-member|20"}},
		// A member with no clause must gain nothing.
		{"Worker.Plain", nil},
	}
	for _, tc := range cases {
		rec := find(t, ents, "SCOPE.Operation", tc.member)
		got := implEdges(rec)
		if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
			t.Errorf("%s IMPLEMENTS edges:\n got %v\nwant %v", tc.member, got, tc.want)
		}
		for _, r := range rec.Relationships {
			if r.Kind == "IMPLEMENTS" && r.FromID != "" {
				t.Errorf("%s IMPLEMENTS->%s has FromID %q; it must stay EMPTY so "+
					"assembly stamps the owning member (#6367)", tc.member, r.ToID, r.FromID)
			}
		}
	}
}

// TestMemberImplementsDoesNotDisplaceTheTypeEdge_6327 is the answer to the
// objection, as a test rather than as a comment: the type keeps exactly the
// edges it had, and none of them acquires the member marker.
func TestMemberImplementsDoesNotDisplaceTheTypeEdge_6327(t *testing.T) {
	rec := find(t, run(t, "app/Worker.vb", memberImplSrc), "SCOPE.Component", "Worker")
	got := implEdges(rec)
	want := []string{"IDisposable||7", "IWorker||7"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("type-level IMPLEMENTS edges changed:\n got %v\nwant %v", got, want)
	}
}

// TestMemberImplementsIsNotEmittedOnPropertyKind_6327 — a Property is a
// distinct entity kind, and its clause must land on IT, not on the type and
// not on a sibling. Kept separate from the table above so a kind-routing
// regression names itself.
func TestPropertyLevelImplementsLandsOnTheProperty_6327(t *testing.T) {
	ents := run(t, "app/Worker.vb", memberImplSrc)
	for i := range ents {
		if ents[i].Name != "Worker.Name" {
			continue
		}
		if len(implEdges(&ents[i])) != 2 {
			t.Errorf("property Worker.Name (kind %s) has %v", ents[i].Kind, implEdges(&ents[i]))
		}
		return
	}
	t.Fatal("no record named Worker.Name")
}
