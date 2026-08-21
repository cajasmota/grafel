package vbnet_test

// #6454 — VB.NET qualified member calls were rendered from the RECEIVER'S
// SPELLING, so `writer.WriteStartElement()` emitted the ToID
// `writer.WriteStartElement` and could never bind to `XmlWriter`.
//
// The per-file symbol table already records the declared type of every
// `As`-typed declaration, so the receiver's TYPE is available without any
// cross-file resolution. These tests pin the substitution and, just as
// importantly, the two places it must NOT happen:
//
//   - the receiver is a TYPE (a Module or Class name), where the "declared
//     type" of the symbol is the word "module"/"class" and substituting it
//     would mint nonsense;
//   - the receiver's only same-named declaration in the file is OUT OF SCOPE.
//     Table.Resolve deliberately falls back to any same-named symbol as a
//     hint (internal/vbnet/symbols.go), and acting on that hint produces a
//     CONFIDENTLY WRONG edge, which is strictly worse than a missing one.
//
// Out of scope by design, and asserted as such below: `Dim x = expr` type
// inference, method-return chaining (`a.B().C()`), With-block receivers, and
// receivers with no declaration in this file.
//
// ONE MEASURED FACT THE ISSUE TEXT DID NOT STATE, and which these tests pin:
// before this change a qualified member call produced NO CALLS EDGE AT ALL,
// not a mis-named one. classify (internal/vbnet/parenrefs.go) answered
// ParenUnknown for every non-Me qualifier, and such a site is only a call to
// Ref.IsCall when it sits at statement head — which `x.Y(...)` never does,
// because the NAME starts after the qualifier. So the receiver's type is what
// unblocks emission, not merely what renames the target: every case below that
// expects an edge is a case that previously produced silence. That is also why
// the negative cases expect NO edge rather than a bare `w.Emit` — leaving such
// a site unclassified is the pre-existing, deliberate behaviour.

import (
	"strings"
	"testing"
)

// callsOf is edges(...,"CALLS") without the "CALLS->" prefix, which only adds
// noise when every row has it.
func callsOf(t *testing.T, src, owner string) []string {
	t.Helper()
	var out []string
	for _, e := range edges(find(t, run(t, "S.vb", src), "SCOPE.Operation", owner), "CALLS") {
		out = append(out, strings.TrimPrefix(e, "CALLS->"))
	}
	return out
}

func hasCall(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

func wantCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !hasCall(got, w) {
			t.Errorf("missing CALLS->%s; got %v", w, got)
		}
	}
}

func denyCalls(t *testing.T, got []string, deny ...string) {
	t.Helper()
	for _, d := range deny {
		if hasCall(got, d) {
			t.Errorf("unwanted CALLS->%s; got %v", d, got)
		}
	}
}

// TestDeclaredTypeRetargetsFieldReceiver is the shape the issue was filed on,
// lifted verbatim from internal/quality/golden/vbnet-mini FrameServer.vb: a
// private field typed with an interface declared in the SAME file.
func TestDeclaredTypeRetargetsFieldReceiver_6454(t *testing.T) {
	const src = `Public Class DirectFrameServer
    Private NativeServer As INativeFrameServer

    Sub CreateAndOpen(path As String)
        NativeServer.OpenFile(path)
    End Sub
End Class

Public Interface INativeFrameServer
    Function OpenFile(file As String) As Integer
End Interface
`
	got := callsOf(t, src, "DirectFrameServer.CreateAndOpen")
	wantCalls(t, got, "INativeFrameServer.OpenFile")
	denyCalls(t, got, "NativeServer.OpenFile")
}

// TestDeclaredTypeRetargetShapes covers the declaration forms readType already
// handles, each of which yields a non-empty Symbol.TypeName.
func TestDeclaredTypeRetargetShapes_6454(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		owner string
		want  string
		deny  string
	}{
		{
			name:  "local Dim As",
			body:  "        Dim w As Writer\n        w.Emit()\n",
			owner: "C.M",
			want:  "Writer.Emit",
			deny:  "w.Emit",
		},
		{
			name:  "As New with constructor args",
			body:  "        Dim w As New Writer(\"p\")\n        w.Emit()\n",
			owner: "C.M",
			want:  "Writer.Emit",
			deny:  "w.Emit",
		},
		{
			name:  "multi declarator shares the type",
			body:  "        Dim a, b As Writer\n        a.Emit()\n        b.Emit()\n",
			owner: "C.M",
			want:  "Writer.Emit",
			deny:  "a.Emit",
		},
		{
			name:  "Using resource",
			body:  "        Using w As New Writer(\"p\")\n            w.Emit()\n        End Using\n",
			owner: "C.M",
			want:  "Writer.Emit",
			deny:  "w.Emit",
		},
		{
			name:  "For Each loop variable",
			body:  "        For Each w As Writer In items\n            w.Emit()\n        Next\n",
			owner: "C.M",
			want:  "Writer.Emit",
			deny:  "w.Emit",
		},
		{
			name:  "Catch variable",
			body:  "        Try\n            Nothing()\n        Catch w As Writer\n            w.Emit()\n        End Try\n",
			owner: "C.M",
			want:  "Writer.Emit",
			deny:  "w.Emit",
		},
	}
	const shell = `Public Class C
    Sub M()
%s    End Sub
End Class

Public Class Writer
    Sub Emit()
    End Sub
End Class
`
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := callsOf(t, strings.Replace(shell, "%s", tc.body, 1), tc.owner)
			wantCalls(t, got, tc.want)
			denyCalls(t, got, tc.deny)
		})
	}
}

// TestDeclaredTypeRetargetsWithEventsField pins the WithEvents form, which is
// pervasive in the WinForms-era VB this extractor targets.
func TestDeclaredTypeRetargetsWithEventsField_6454(t *testing.T) {
	const src = `Public Class Form1
    Public WithEvents Btn As Button

    Sub Wire()
        Btn.PerformClick()
    End Sub
End Class

Public Class Button
    Sub PerformClick()
    End Sub
End Class
`
	got := callsOf(t, src, "Form1.Wire")
	wantCalls(t, got, "Button.PerformClick")
	denyCalls(t, got, "Btn.PerformClick")
}

// TestDeclaredTypeRetargetsParameterReceiver — a parameter is the other half
// of the corpus's explicit-`As` population.
func TestDeclaredTypeRetargetsParameterReceiver_6454(t *testing.T) {
	const src = `Public Class C
    Sub M(w As Writer)
        w.Emit()
    End Sub
End Class

Public Class Writer
    Sub Emit()
    End Sub
End Class
`
	got := callsOf(t, src, "C.M")
	wantCalls(t, got, "Writer.Emit")
	denyCalls(t, got, "w.Emit")
}

// TestDeclaredTypeLocalShadowsField is the reason the substitution must go
// through Table.Resolve rather than a flat name lookup: the innermost
// declaration wins, so the local's type — not the field's — names the target.
func TestDeclaredTypeLocalShadowsField_6454(t *testing.T) {
	const src = `Public Class C
    Private w As Alpha

    Sub Inner()
        Dim w As Beta
        w.Emit()
    End Sub

    Sub Outer()
        w.Emit()
    End Sub
End Class

Public Class Alpha
    Sub Emit()
    End Sub
End Class

Public Class Beta
    Sub Emit()
    End Sub
End Class
`
	inner := callsOf(t, src, "C.Inner")
	wantCalls(t, inner, "Beta.Emit")
	denyCalls(t, inner, "Alpha.Emit", "w.Emit")

	outer := callsOf(t, src, "C.Outer")
	wantCalls(t, outer, "Alpha.Emit")
	denyCalls(t, outer, "Beta.Emit", "w.Emit")
}

// TestDeclaredTypeRefusesOutOfScopeDeclaration is the SCOPE GUARD, and the
// single most important assertion in this file.
//
// Table.Resolve's documented last-ditch fallback returns ANY same-named
// declaration in the file when nothing is lexically visible. Here `w` is a
// field of Alpha's owner ONLY; the call site is in an unrelated class that
// never declares `w`. Substituting the hint would emit a confidently wrong
// `Holder.Emit` edge. The receiver must be left spelled as written so the edge
// dangles honestly instead.
func TestDeclaredTypeRefusesOutOfScopeDeclaration_6454(t *testing.T) {
	const src = `Public Class Holder
    Private w As Alpha
End Class

Public Class Other
    Sub M()
        w.Emit()
    End Sub
End Class

Public Class Alpha
    Sub Emit()
    End Sub
End Class
`
	got := callsOf(t, src, "Other.M")
	denyCalls(t, got, "Alpha.Emit", "w.Emit")
	if len(got) != 0 {
		t.Errorf("an undeclared receiver must produce NO CALLS edge; got %v", got)
	}
}

// TestDeclaredTypeRefusesTypeReceiver — a Module or Class name as receiver is
// a STATIC access, not a value. Symbol.TypeName on a KindType symbol carries
// the declaring keyword ("module", "class"), so substituting it blindly would
// produce `module.Log`.
func TestDeclaredTypeRefusesTypeReceiver_6454(t *testing.T) {
	const src = `Module Helpers
    Sub Log(m As String)
    End Sub
End Module

Public Class Registry
    Shared Sub Add(k As String)
    End Sub
End Class

Public Class C
    Sub M()
        Helpers.Log("x")
        Registry.Add("y")
    End Sub
End Class
`
	got := callsOf(t, src, "C.M")
	denyCalls(t, got, "module.Log", "class.Add", "Module.Log", "Helpers.Log", "Registry.Add")
	if len(got) != 0 {
		t.Errorf("a TYPE receiver is a static access this pass does not model; "+
			"it must produce NO CALLS edge rather than one rendered from the "+
			"declaring keyword; got %v", got)
	}
}

// TestDeclaredTypeRetargetsFrameworkReceiver — a receiver typed with a .NET
// framework type still dangles (#6337 owns external .NET binding), but it
// dangles as a BETTER STRING: `String.Contains` instead of `_macro.Contains`.
// Same disposition, no new risk class, and the edge becomes joinable the day
// #6337 lands.
func TestDeclaredTypeRetargetsFrameworkReceiver_6454(t *testing.T) {
	const src = `Public Class Criteria
    Private _macro As String

    Sub M(ptr As IntPtr, wDay As UInt16)
        If _macro.Contains(":") Then
        End If
        ptr.ToInt64()
        wDay.ToString()
    End Sub
End Class
`
	got := callsOf(t, src, "Criteria.M")
	wantCalls(t, got, "String.Contains", "IntPtr.ToInt64", "UInt16.ToString")
	denyCalls(t, got, "_macro.Contains", "ptr.ToInt64", "wDay.ToString")
}

// TestDeclaredTypeLeavesUntypedReceiversAlone pins the SLICE BOUNDARY. Each of
// these is a real receiver whose type this pass deliberately does not know;
// every one must keep its spelled qualifier rather than acquire a guess.
func TestDeclaredTypeLeavesUntypedReceiversAlone_6454(t *testing.T) {
	const src = `Public Class C
    Sub M()
        Dim inferred = MakeWriter()
        inferred.Emit()
        undeclared.Emit()
    End Sub

    Function MakeWriter() As Writer
    End Function
End Class

Public Class Writer
    Sub Emit()
    End Sub
End Class
`
	got := callsOf(t, src, "C.M")
	wantCalls(t, got, "MakeWriter") // the unqualified call is unaffected
	denyCalls(t, got, "Writer.Emit", "inferred.Emit", "undeclared.Emit")
}

// TestDeclaredTypeRetargetsAddressOfReceiver — REFERENCES edges share
// memberTarget with CALLS (references.go), so the two edge kinds cannot drift
// apart on the receiver's type either.
func TestDeclaredTypeRetargetsAddressOfReceiver_6454(t *testing.T) {
	const src = `Public Class Form1
    Private Btn As Button

    Sub Wire()
        AddHandler Something, AddressOf Btn.OnClick
    End Sub
End Class

Public Class Button
    Sub OnClick()
    End Sub
End Class
`
	var got []string
	for _, e := range edges(find(t, run(t, "S.vb", src), "SCOPE.Operation", "Form1.Wire"), "REFERENCES") {
		got = append(got, strings.TrimPrefix(e, "REFERENCES->"))
	}
	wantCalls(t, got, "Button.OnClick")
	denyCalls(t, got, "Btn.OnClick")
}

// TestDeclaredTypeRefusesArrayReceiver — for `Dim buf() As Byte`, Byte is the
// ELEMENT type, not the receiver's type: `buf.Clone()` is a member of the
// array, and rendering `Byte.Clone` would be confidently wrong rather than
// merely dangling. The table records the distinction as Symbol.IsArray, which
// is the only thing separating `As Integer()` from `As List(Of Integer)`.
func TestDeclaredTypeRefusesArrayReceiver_6454(t *testing.T) {
	const src = `Public Class C
    Private buf() As Byte
    Private names As String()

    Sub M()
        buf.Clone()
        names.Clone()
    End Sub
End Class
`
	got := callsOf(t, src, "C.M")
	denyCalls(t, got, "Byte.Clone", "String.Clone", "buf.Clone", "names.Clone")
	if len(got) != 0 {
		t.Errorf("an array receiver's element type does not name its members; "+
			"the site must produce NO CALLS edge; got %v", got)
	}
}

// TestDeclaredTypeRefusesSameScopeDisagreement is the SECOND half of the scope
// guard, and it closes the door the first half left open.
//
// Symbol.Scope is a DECLARATION path, not a block path: VB block-scopes `Dim`,
// but two sibling blocks of one method both record Scope "C.Go". Both are
// visible, neither is innermost, and the table has no line ordering to break
// the tie — so picking the first binds the Beta site to Alpha with full
// confidence, which is the exact failure class the guard exists to prevent.
// One dangling edge is the correct price.
func TestDeclaredTypeRefusesSameScopeDisagreement_6454(t *testing.T) {
	const src = `Public Class C
    Sub Go()
        If True Then
            Dim w As Alpha
            w.Emit(1)
        Else
            Dim w As Beta
            w.Emit(2)
        End If
    End Sub
End Class

Public Class Alpha
    Sub Emit(n As Integer)
    End Sub
End Class

Public Class Beta
    Sub Emit(n As Integer)
    End Sub
End Class
`
	got := callsOf(t, src, "C.Go")
	denyCalls(t, got, "Alpha.Emit", "Beta.Emit", "w.Emit")
	if len(got) != 0 {
		t.Errorf("two same-scope declarations disagreeing on the type cannot be "+
			"ordered by this table; both sites must dangle rather than half of "+
			"them binding wrong; got %v", got)
	}
}

// TestDeclaredTypeAgreeingSameScopeStillBinds keeps the refusal above from
// being a blanket ban on repeated declarations: when the candidates AGREE, the
// tie needs no breaking and the edge is as sound as any other.
func TestDeclaredTypeAgreeingSameScopeStillBinds_6454(t *testing.T) {
	const src = `Public Class C
    Sub Go()
        If True Then
            Dim w As Alpha
            w.Emit(1)
        Else
            Dim w As Alpha
            w.Emit(2)
        End If
    End Sub
End Class

Public Class Alpha
    Sub Emit(n As Integer)
    End Sub
End Class
`
	wantCalls(t, callsOf(t, src, "C.Go"), "Alpha.Emit")
}

// TestDeclaredTypeRefusesAcrossNestedTypeBoundary — visibleFrom models LEXICAL
// containment, and VB.NET member visibility is not lexical containment: a
// nested class does not see the enclosing type's instance members. Binding
// `log.Emit()` in Inner to Outer's field is the same confidently-wrong class as
// the out-of-scope bind, just with narrower reach.
func TestDeclaredTypeRefusesAcrossNestedTypeBoundary_6454(t *testing.T) {
	const src = `Public Class Outer
    Private log As Alpha

    Public Class Inner
        Sub Go()
            log.Emit()
        End Sub
    End Class

    Sub Direct()
        log.Emit()
    End Sub
End Class

Public Class Alpha
    Sub Emit()
    End Sub
End Class
`
	// The record is named off the NEAREST enclosing type, so Inner.Go.
	inner := callsOf(t, src, "Inner.Go")
	denyCalls(t, inner, "Alpha.Emit", "log.Emit")
	if len(inner) != 0 {
		t.Errorf("a nested type does not see the enclosing type's instance "+
			"members; got %v", inner)
	}
	// The same field read from the SAME type still binds — the guard is a type
	// boundary, not a blanket refusal of anything declared above the use site.
	wantCalls(t, callsOf(t, src, "Outer.Direct"), "Alpha.Emit")
}

// TestDeclaredTypeDropsGenericArgumentList — a generic type is emitted under
// its BARE name, so `As TaskDialog(Of String)` must answer TaskDialog. Keeping
// the argument list renders a target no entity in the graph carries, which
// silently converts in-tree binds back into dangles.
func TestDeclaredTypeDropsGenericArgumentList_6454(t *testing.T) {
	const src = `Public Class C
    Private dlg As TaskDialog(Of String)

    Sub Go()
        dlg.AddCommand("x")
    End Sub
End Class

Public Class TaskDialog(Of T)
    Sub AddCommand(s As String)
    End Sub
End Class
`
	got := callsOf(t, src, "C.Go")
	wantCalls(t, got, "TaskDialog.AddCommand")
	denyCalls(t, got, "TaskDialog(Of String).AddCommand", "dlg.AddCommand")
}

// TestDeclaredTypeRefusesMethodReceiver — a METHOD in qualifier position is
// return-value chaining (`MakeWriter().Emit()` spelled without the parens the
// parser needs to see it as an expression). Symbol.TypeName on a method is its
// RETURN type, so admitting KindMethod would render a target off a value that
// the receiver is not — and chaining is explicitly outside this slice.
func TestDeclaredTypeRefusesMethodReceiver_6454(t *testing.T) {
	const src = `Public Class C
    Function MakeWriter() As Writer
    End Function

    Sub Go()
        MakeWriter.Emit()
    End Sub
End Class

Public Class Writer
    Sub Emit()
    End Sub
End Class
`
	got := callsOf(t, src, "C.Go")
	denyCalls(t, got, "Writer.Emit", "MakeWriter.Emit")
	if len(got) != 0 {
		t.Errorf("a method receiver is return-value chaining, which this pass "+
			"does not model; got %v", got)
	}
}
