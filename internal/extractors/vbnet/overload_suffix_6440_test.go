package vbnet

// Unit gates for the CONTENT of #6440's overload discriminator.
//
// WHY THESE EXIST, AND WHY THE GOLDEN FIXTURE IS NOT ENOUGH. vbnet-mini pins
// that a discriminator is PRESENT: `StructPtr.Dispose()` becomes its own entity
// instead of merging into `Dispose(disposing As Boolean)`. But that member's
// parameter list is EMPTY, so its discriminator renders as a bare `()` and the
// whole of paramTypeList is exercised by a string that contains no type at all.
// Measured, not assumed: mutating paramTypeList to append parameter NAMES —
// the one thing its doc comment says it must never do — left
// ./internal/extractors/vbnet/, ./cmd/grafel/ (Quality|Fixture|Golden|VBNet)
// and the #6440 fixture test ALL GREEN.
//
// So this file asserts the properties the design rests on, at the level they
// are decided, without growing the fixture: what a non-empty list renders as,
// that parameter names are excluded, that array rank is not, that ByRef/Optional
// fold, and that the ordinal fallback keeps the per-file name set injective when
// two rendered lists agree anyway.
//
// The package is `vbnet`, not `vbnet_test`, deliberately: overloadSuffix and
// paramTypeList are unexported, and the point is to observe them DIRECTLY
// rather than through three passes of the indexer.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/vbnet"
)

// memberNames extracts one file and returns the SCOPE.Operation names in
// emission order — the names that become graph.EntityID inputs.
func memberNames(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, r := range extractVBNet(src, "T.vb", "") {
		if r.Kind == "SCOPE.Operation" {
			out = append(out, r.Name)
		}
	}
	return out
}

func eqNames(t *testing.T, got, want []string, why string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s\ngot  %q\nwant %q", why, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s\ngot  %q\nwant %q", why, got, want)
		}
	}
}

// TestOverloadSuffixRendersParameterTypes is the assertion the golden fixture
// cannot make: a NON-EMPTY parameter list. Without it every character
// paramTypeList emits between the parentheses is unobserved.
func TestOverloadSuffixRendersParameterTypes_6440(t *testing.T) {
	got := memberNames(t, `Public Class T
	Public Sub Send()
	End Sub
	Public Sub Send(a As String)
	End Sub
	Public Sub Send(a As String, b As Integer)
	End Sub
	Public Sub Send(Of TItem)(items As System.Collections.Generic.List(Of TItem))
	End Sub
End Class`)
	eqNames(t, got, []string{
		// First in source order keeps the bare name — byLocation
		// (internal/resolve/refs.go:404) is keyed (file, name) and a same-file
		// CALLS ref asks for the undecorated name.
		"T.Send",
		"T.Send(String)",
		"T.Send(String, Integer)",
		// Type parameters lead, so a generic overload cannot render the same
		// list as a non-generic sibling with one argument of that type. The
		// argument type is kept AS WRITTEN, dots included: normalising it would
		// need a type resolver this extractor does not have, and a half-applied
		// normalisation is worse than none.
		"T.Send(Of TItem)(System.Collections.Generic.List(Of TItem))",
	}, "the discriminator must be the rendered parameter-TYPE list")
}

// TestOverloadSuffixExcludesParameterNames is the property the surviving mutant
// violated. Two types declare the same two overloads and differ ONLY in what
// they call the parameters; the discriminators must be identical, because a
// parameter rename is not a change of identity and must not move an entity id.
func TestOverloadSuffixExcludesParameterNames_6440(t *testing.T) {
	got := memberNames(t, `Public Class Alpha
	Public Sub Log()
	End Sub
	Public Sub Log(message As String, retries As Integer)
	End Sub
End Class

Public Class Beta
	Public Sub Log()
	End Sub
	Public Sub Log(text As String, attempts As Integer)
	End Sub
End Class`)
	eqNames(t, got, []string{
		"Alpha.Log",
		"Alpha.Log(String, Integer)",
		"Beta.Log",
		"Beta.Log(String, Integer)",
	}, "parameter NAMES must not reach the discriminator: renaming `message` to "+
		"`text` would otherwise move the entity id of a member nobody touched")
}

// TestOverloadSuffixKeepsArrayRank pins the one parameter property that IS
// discriminating and is easy to drop. The parser lifts the rank marker off the
// type (`Byte()` gives TypeName "Byte" + IsArray), so reading TypeName alone
// folds the array overload into its scalar sibling and pushes it onto the
// ordinal fallback — which is stable, but wrong: these are two different
// signatures and VB overloads on exactly that difference.
func TestOverloadSuffixKeepsArrayRank_6440(t *testing.T) {
	got := memberNames(t, `Public Class T
	Public Sub Send(payload As Byte)
	End Sub
	Public Sub Send(payload As Byte())
	End Sub
End Class`)
	eqNames(t, got, []string{
		"T.Send",
		"T.Send(Byte())",
	}, "the array rank marker must survive into the discriminator")
}

// TestOverloadSuffixOrdinalFallback covers the residual case the design admits:
// two siblings whose rendered lists agree anyway. VB cannot overload on
// ByRef/ByVal or on Optional alone, so a source that looks like this is a
// declaration the parser recovered twice rather than two members — but the
// per-file name set must stay INJECTIVE regardless, because byLocation deletes
// a bucket it sees twice. The ordinal is confined to the group that needs it,
// which is the whole reason the primary discriminator is not an ordinal.
func TestOverloadSuffixOrdinalFallback_6440(t *testing.T) {
	got := memberNames(t, `Public Class T
	Public Sub Ping(a As String)
	End Sub
	Public Sub Ping(ByRef a As String)
	End Sub
	Public Sub Ping(Optional a As String = "")
	End Sub
End Class`)
	eqNames(t, got, []string{
		"T.Ping",
		"T.Ping(String)",
		"T.Ping(String)#2",
	}, "when two rendered type lists agree, the later member takes a source-order "+
		"ordinal so no two members in one file share a name")

	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Fatalf("names are not injective within the file: %q", got)
		}
		seen[n] = true
	}
}

// TestOverloadSuffixEmptyWhenNotColliding is option B stated at the unit level:
// a member with no same-name sibling gets NO suffix, so its name — and
// therefore its graph.EntityID — is byte-identical to what it was before #6440.
// cmd/grafel/quality_vbnet_overload_6440_test.go pins the same property against
// a literal id; this pins the mechanism that produces it.
func TestOverloadSuffixEmptyWhenNotColliding_6440(t *testing.T) {
	got := memberNames(t, `Public Class T
	Public Sub Alone(a As String)
	End Sub
	Public Function Other(b As Integer) As Boolean
	End Function
	Public Property Value As String
End Class`)
	eqNames(t, got, []string{"T.Alone", "T.Other", "T.Value"},
		"a non-colliding member must keep its exact pre-#6440 name")
}

// TestOverloadSuffixGroupsByKind pins the group predicate: a Field and a Sub of
// one name land on different grafel kinds (SCOPE.Schema vs SCOPE.Operation), so
// they never collided by graph.EntityID and must not be discriminated as if
// they had. Widening the group to ignore Kind would move a field's id for no
// reason — an identity change with no defect behind it.
func TestOverloadSuffixGroupsByKind_6440(t *testing.T) {
	var names []string
	for _, r := range extractVBNet(`Public Class T
	Public Handler As String
	Public Sub Handler(a As Integer)
	End Sub
End Class`, "T.vb", "") {
		if r.Kind == "SCOPE.Schema" || r.Kind == "SCOPE.Operation" {
			names = append(names, r.Kind+" "+r.Name)
		}
	}
	eqNames(t, names, []string{"SCOPE.Schema T.Handler", "SCOPE.Operation T.Handler"},
		"a Field and a Sub of one name hash to different ids already, so neither "+
			"may pick up a discriminator")
}

// TestParamTypeListDirect exercises paramTypeList on its own, so a failure
// points at the renderer rather than at three passes of the extractor.
func TestParamTypeListDirect_6440(t *testing.T) {
	cases := []struct {
		name string
		decl string
		want string
	}{
		{"parameterless", "Sub F()", "()"},
		{"one typed param", "Sub F(a As String)", "(String)"},
		{"names excluded", "Sub F(somethingElseEntirely As String)", "(String)"},
		{"two params", "Sub F(a As String, b As Integer)", "(String, Integer)"},
		{"array rank kept", "Sub F(a As Byte())", "(Byte())"},
		{"byref folds away", "Sub F(ByRef a As String)", "(String)"},
		{"optional and default fold away", `Sub F(Optional a As String = "x")`, "(String)"},
		{"untyped param is Object", "Sub F(a)", "(Object)"},
		{"qualified type kept as written", "Sub F(a As System.Text.StringBuilder)", "(System.Text.StringBuilder)"},
		{"type params lead", "Sub F(Of TItem)(a As TItem)", "(Of TItem)(TItem)"},
		{"function return type is not part of it", "Function F(a As String) As Boolean", "(String)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			end := "Sub"
			if strings.HasPrefix(tc.decl, "Function") {
				end = "Function"
			}
			src := "Public Class T\n\t" + tc.decl + "\n\tEnd " + end + "\nEnd Class"
			res := vbnet.Parse(src)
			var member *vbnet.Node
			var walk func(n *vbnet.Node)
			walk = func(n *vbnet.Node) {
				for _, c := range n.Children {
					if c.Kind == vbnet.NodeMethod && member == nil {
						member = c
					}
					walk(c)
				}
			}
			walk(res.File)
			if member == nil {
				t.Fatalf("parser found no method in:\n%s", src)
			}
			if got := paramTypeList(member); got != tc.want {
				t.Errorf("paramTypeList(%s) = %q, want %q", tc.decl, got, tc.want)
			}
		})
	}
}
