package vbnet_test

// S7b of #6327 — `Handles` clauses and `AddressOf` operands become REFERENCES.
//
// Both constructs name a member as a VALUE rather than invoking it, which is
// exactly what REFERENCES means everywhere else in grafel (golang/references.go,
// java/references.go, python/references.go). CALLS would be a lie: the body of
// `Button1_Click` does not run at the AddressOf site, and a `Handles` clause
// invokes nothing at all.
//
// Direction is handler -> handled, and FromID is EMPTY on every edge so
// assembly stamps the OWNING member. A path-valued FromID here is the
// #6295/#6298/#6365/#6367 defect class and
// internal/extractors/file_anchored_rels_guard_test.go is its source-level
// half.

import (
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

const handlesSrc = `Public Class Form1
    Inherits System.Windows.Forms.Form

    Private WithEvents Button1 As Button

    Private Sub Button1_Click(sender As Object, e As EventArgs) Handles Button1.Click
        Compute(1)
    End Sub

    Private Sub Any(sender As Object, e As EventArgs) _
        Handles Button1.Click, Button2.Click, MyBase.Load, Me.Shown
    End Sub

    Private Sub Wire()
        AddHandler Button1.Click, AddressOf Button1_Click
        Dim t As New System.Threading.Thread(AddressOf Worker)
        RemoveHandler Button1.Click, AddressOf Me.Button1_Click
    End Sub

    Private Sub Worker()
    End Sub

    Private Sub Compute(n As Integer)
    End Sub
End Class
`

func TestHandles_ReferencesOnTheHandler(t *testing.T) {
	ents := run(t, "Form1.vb", handlesSrc)

	// The target is the WithEvents FIELD, not the event: the field is in-tree
	// (S7a merged the designer half into this Component) while `Click` belongs
	// to System.Windows.Forms.Button and can never resolve. See references.go
	// for the corpus measurement that decided it; the event is not lost, it is
	// stamped as the `event` property.
	got := edges(find(t, ents, "SCOPE.Operation", "Form1.Button1_Click"), "REFERENCES")
	want := []string{"REFERENCES->Form1.Button1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Button1_Click REFERENCES\n got %v\nwant %v", got, want)
	}

	// Multiple comma-separated targets, and the Me/MyBase prefixes folded away
	// exactly as callTarget folds them: they denote the current instance and
	// are not nameable entities.
	// Multiple comma-separated targets. Two distinct control receivers give two
	// distinct edges; Me/MyBase name no entity, so those two target the EVENT,
	// rendered under the enclosing type the way declName renders every member.
	got = edges(find(t, ents, "SCOPE.Operation", "Form1.Any"), "REFERENCES")
	want = []string{
		"REFERENCES->Form1.Button1",
		"REFERENCES->Form1.Button2",
		"REFERENCES->Form1.Load",
		"REFERENCES->Form1.Shown",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Any REFERENCES\n got %v\nwant %v", got, want)
	}
}

func TestAddressOf_ReferencesOnTheEnclosingMember(t *testing.T) {
	ents := run(t, "Form1.vb", handlesSrc)

	got := edges(find(t, ents, "SCOPE.Operation", "Form1.Wire"), "REFERENCES")
	// `AddressOf Me.Button1_Click` folds to the bare member, and the duplicate
	// of `AddressOf Button1_Click` collapses onto it: one edge per target.
	want := []string{"REFERENCES->Form1.Button1_Click", "REFERENCES->Form1.Worker"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Wire REFERENCES\n got %v\nwant %v", got, want)
	}

	// AddressOf is NOT a call: the S5 CALLS set must be untouched by it.
	calls := edges(find(t, ents, "SCOPE.Operation", "Form1.Wire"), "CALLS")
	for _, c := range calls {
		if strings.HasSuffix(c, "Button1_Click") || strings.HasSuffix(c, "Worker") {
			t.Fatalf("AddressOf operand leaked into CALLS: %v", calls)
		}
	}
}

// TestHandlesAddressOf_NoFileAnchoredFromID is the behavioural half of the
// guard in internal/extractors/file_anchored_rels_guard_test.go.
func TestHandlesAddressOf_NoFileAnchoredFromID(t *testing.T) {
	ents := run(t, "Form1.vb", handlesSrc)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "REFERENCES" {
				continue
			}
			if r.FromID != "" {
				t.Fatalf("REFERENCES->%s carries FromID %q; it must be empty so "+
					"assembly stamps the owning member", r.ToID, r.FromID)
			}
		}
	}
}

// TestHandlesAddressOf_LineProperty pins the `line` property, which is what
// lets a reader jump to the wiring site rather than to the type.
func TestHandlesAddressOf_LineProperty(t *testing.T) {
	ents := run(t, "Form1.vb", handlesSrc)
	lineOf := func(name, to string) string {
		rec := find(t, ents, "SCOPE.Operation", name)
		for _, r := range rec.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == to {
				v := r.Properties.Get("line")
				return v
			}
		}
		t.Fatalf("no REFERENCES->%s on %s", to, name)
		return ""
	}
	// The Handles clause is stamped with the method's declaration line: VB
	// records the clause targets as a []string with no positions, the same
	// limit the hierarchy edges already document.
	if got := lineOf("Form1.Button1_Click", "Form1.Button1"); got != "6" {
		t.Fatalf("Handles line: got %q want %q", got, "6")
	}
	// An AddressOf operand carries its own physical line.
	if got := lineOf("Form1.Wire", "Form1.Worker"); got != "16" {
		t.Fatalf("AddressOf line: got %q want %q", got, "16")
	}
}

// TestAddressOf_FieldInitialiserAnchorsOnTheType pins the ONE ownership case
// that is not the obvious one, so it is not rediscovered as a bug.
//
// A field initialiser's operand lands on the enclosing TYPE, not on the field:
// walkFields opens the field node without pushing it, so the innermost open
// node at scan time is the type. That is inherited verbatim from the existing
// Refs pass — `Private cb As Action = Foo()` already puts its CALLS on the
// type — and moving it would move CALLS with it, which is not S7b's to do.
func TestAddressOf_FieldInitialiserAnchorsOnTheType(t *testing.T) {
	ents := run(t, "M.vb", `Module M
    Private cb As Action = AddressOf Target

    Sub Target()
    End Sub
End Module
`)
	got := edges(find(t, ents, "SCOPE.Component", "M"), "REFERENCES")
	if strings.Join(got, ",") != "REFERENCES->M.Target" {
		t.Fatalf("field-initialiser REFERENCES on the type: got %v", got)
	}
	if got := edges(find(t, ents, "SCOPE.Schema", "M.cb"), "REFERENCES"); len(got) != 0 {
		t.Fatalf("the field itself must own no REFERENCES, got %v", got)
	}
}

// TestHandlesAddressOf_LanguageTagged — relLanguage reads
// Properties["language"] off each RELATIONSHIP to select the vbnet
// disposition arm; an untagged edge lands in bug-extractor.
func TestHandlesAddressOf_LanguageTagged(t *testing.T) {
	ents := run(t, "Form1.vb", handlesSrc)
	var refs []types.RelationshipRecord
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				refs = append(refs, r)
			}
		}
	}
	if len(refs) == 0 {
		t.Fatal("no REFERENCES edges emitted at all")
	}
	for _, r := range refs {
		if v := r.Properties.Get("language"); v != "vbnet" {
			t.Fatalf("REFERENCES->%s language=%q, want vbnet", r.ToID, v)
		}
	}
}

// TestHandles_NotAnIdentifier — `handles` and `addressof` are reserved words,
// but a bracket-escaped declaration may still use them, and a trailing word
// that merely looks like the clause must not be cut off a signature.
func TestHandles_NotAnIdentifier(t *testing.T) {
	ents := run(t, "C.vb", `Public Class C
    Private [Handles] As Integer

    Public Sub NoClause(handlesCount As Integer)
    End Sub
End Class
`)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "REFERENCES" {
				t.Fatalf("unexpected REFERENCES->%s on %s", r.ToID, e.Name)
			}
		}
	}
	names := map[string]bool{}
	for _, e := range ents {
		names[e.Name] = true
	}
	for _, want := range []string{"C.Handles", "C.NoClause"} {
		if !names[want] {
			var got []string
			for n := range names {
				got = append(got, n)
			}
			sort.Strings(got)
			t.Fatalf("missing entity %q; got %v", want, got)
		}
	}
}

// TestHandles_EventAndViaProperties pins what the receiver-targeting decision
// must NOT throw away. Retargeting `Handles Button1.Click` at the field is
// only defensible because the event survives as a property; a reader (or the
// dashboard) can still answer "which event".
func TestHandles_EventAndViaProperties(t *testing.T) {
	ents := run(t, "Form1.vb", handlesSrc)
	props := func(name, to string) (event, via string) {
		rec := find(t, ents, "SCOPE.Operation", name)
		for _, r := range rec.Relationships {
			if r.Kind == "REFERENCES" && r.ToID == to {
				return r.Properties.Get("event"), r.Properties.Get("via")
			}
		}
		t.Fatalf("no REFERENCES->%s on %s", to, name)
		return "", ""
	}
	if event, via := props("Form1.Button1_Click", "Form1.Button1"); event != "Click" || via != "handles" {
		t.Fatalf("Handles props: event=%q via=%q, want Click/handles", event, via)
	}
	// A Me/MyBase receiver targets the EVENT itself, so there is no separate
	// event to record — recording one would name the target twice.
	if event, via := props("Form1.Any", "Form1.Load"); event != "" || via != "handles" {
		t.Fatalf("MyBase Handles props: event=%q via=%q, want \"\"/handles", event, via)
	}
	if event, via := props("Form1.Wire", "Form1.Worker"); event != "" || via != "addressof" {
		t.Fatalf("AddressOf props: event=%q via=%q, want \"\"/addressof", event, via)
	}
}

// TestReferences_QualifiedTargetsAreNotReQualified is the refusal half of the
// naming rule: a target that already carries a qualifier names something
// outside this type, and prefixing it would invent a nesting that does not
// exist.
func TestReferences_QualifiedTargetsAreNotReQualified(t *testing.T) {
	ents := run(t, "C.vb", `Public Class C
    Sub Wire()
        AddHandler X, AddressOf Helpers.Run
    End Sub

    Sub Other() Handles My.Forms.Explorer.StatusChange
    End Sub
End Class
`)
	got := edges(find(t, ents, "SCOPE.Operation", "C.Wire"), "REFERENCES")
	if strings.Join(got, ",") != "REFERENCES->Helpers.Run" {
		t.Fatalf("qualified AddressOf: got %v, want REFERENCES->Helpers.Run", got)
	}
	got = edges(find(t, ents, "SCOPE.Operation", "C.Other"), "REFERENCES")
	if strings.Join(got, ",") != "REFERENCES->My.Forms.Explorer" {
		t.Fatalf("dotted Handles receiver: got %v, want REFERENCES->My.Forms.Explorer", got)
	}
}

// TestAddressOf_AutoPropertyInitialiser — walkProperty scans no refs and
// pushes no frame, so an auto-property initialiser is invisible to the
// reference pass. MEASURED: 142 of the corpus's 585 AddressOf tokens live in
// exactly this shape.
func TestAddressOf_AutoPropertyInitialiser(t *testing.T) {
	ents := run(t, "P.vb", `Public Class EncoderParams
    Property QPInit As New NumParam With {.Switch = "--qp", .ArgsFunc = AddressOf GetQpArgs, .ImportAction = AddressOf ImportQpArgs}

    Function GetQpArgs() As String
        Return ""
    End Function

    Sub ImportQpArgs()
    End Sub
End Class
`)
	got := edges(find(t, ents, "SCOPE.Operation", "EncoderParams.QPInit"), "REFERENCES")
	want := []string{
		"REFERENCES->EncoderParams.GetQpArgs",
		"REFERENCES->EncoderParams.ImportQpArgs",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("auto-property REFERENCES\n got %v\nwant %v", got, want)
	}
	// The operands belong to the PROPERTY, not to the enclosing class.
	if got := edges(find(t, ents, "SCOPE.Component", "EncoderParams"), "REFERENCES"); len(got) != 0 {
		t.Fatalf("class must own no REFERENCES from a property initialiser, got %v", got)
	}
}
