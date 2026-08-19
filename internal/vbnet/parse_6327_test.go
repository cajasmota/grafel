package vbnet

import (
	"strings"
	"testing"
)

// findNode returns the first node in document order whose dotted path matches
// path ("Ns.C.F"), or nil.
func findNode(root *Node, path string) *Node {
	var hit *Node
	root.Walk(func(n *Node) {
		if hit == nil && n.Path() == path {
			hit = n
		}
	})
	return hit
}

func kindsOf(nodes []*Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Kind.String()+":"+n.Name)
	}
	return out
}

// TestParse_TypeNesting pins the containment tree: a namespace holds a class,
// a class holds a nested enum and its members, and every node knows the scope
// it was declared in.
func TestParse_TypeNesting(t *testing.T) {
	src := strings.Join([]string{
		"Namespace Acme.Tools",         // 1
		"    Public Class Widget",      // 2
		"        Public Enum Mode",     // 3
		"            Fast",             // 4
		"            Slow = 2",         // 5
		"        End Enum",             // 6
		"        Public Sub Run()",     // 7
		"            Dim n As Integer", // 8
		"        End Sub",              // 9
		"    End Class",                // 10
		"End Namespace",                // 11
	}, "\n")

	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}

	for _, tc := range []struct {
		path  string
		kind  NodeKind
		scope string
		start int
		end   int
	}{
		{"Acme.Tools", NodeNamespace, "", 1, 11},
		{"Acme.Tools.Widget", NodeClass, "Acme.Tools", 2, 10},
		{"Acme.Tools.Widget.Mode", NodeEnum, "Acme.Tools.Widget", 3, 6},
		{"Acme.Tools.Widget.Mode.Fast", NodeEnumMember, "Acme.Tools.Widget.Mode", 4, 4},
		{"Acme.Tools.Widget.Mode.Slow", NodeEnumMember, "Acme.Tools.Widget.Mode", 5, 5},
		{"Acme.Tools.Widget.Run", NodeMethod, "Acme.Tools.Widget", 7, 9},
	} {
		n := findNode(res.File, tc.path)
		if n == nil {
			t.Errorf("no node at path %q; tree is\n%s", tc.path, res.File.Dump())
			continue
		}
		if n.Kind != tc.kind {
			t.Errorf("%s: kind = %v, want %v", tc.path, n.Kind, tc.kind)
		}
		if n.Scope != tc.scope {
			t.Errorf("%s: scope = %q, want %q", tc.path, n.Scope, tc.scope)
		}
		if n.Span.StartLine != tc.start || n.Span.EndLine != tc.end {
			t.Errorf("%s: span lines = %d..%d, want %d..%d",
				tc.path, n.Span.StartLine, n.Span.EndLine, tc.start, tc.end)
		}
	}

	// A method-body local is NOT a tree node: S5 emits no entity for it, and
	// the declaration table already carries it for paren classification.
	if n := findNode(res.File, "Acme.Tools.Widget.Run.n"); n != nil {
		t.Errorf("method local leaked into the tree as %v", n.Kind)
	}
	if got := res.Table.Resolve("n", "Acme.Tools.Widget.Run"); got == nil || got.Kind != KindLocal {
		t.Errorf("table lost the local: %+v", got)
	}
}

// TestParse_SpanBytesIndexSource pins that byte spans are offsets into the
// ORIGINAL source, so a caller can slice the declaration back out.
func TestParse_SpanBytesIndexSource(t *testing.T) {
	src := "Option Strict On\r\n\r\nPublic Class C\r\n    Public Sub F()\r\n    End Sub\r\nEnd Class\r\n"
	res := Parse(src)
	c := findNode(res.File, "C")
	if c == nil {
		t.Fatalf("no class; tree is\n%s", res.File.Dump())
	}
	got := src[c.Span.StartByte:c.Span.EndByte]
	if !strings.HasPrefix(got, "Public Class C") || !strings.HasSuffix(got, "End Class") {
		t.Errorf("class span slices %q", got)
	}
	f := findNode(res.File, "C.F")
	if f == nil {
		t.Fatalf("no method")
	}
	if got := src[f.Span.StartByte:f.Span.EndByte]; got != "Public Sub F()\r\n    End Sub" {
		t.Errorf("method span slices %q", got)
	}
}

// TestParse_EnumMemberNamedLikeAModifier is a corpus finding: three real files
// declare an enum member called `Custom`, which is also the modifier that
// opens a Custom Event. Peeling modifiers before recognising an enum member
// consumed the name and left a declaration with no declarator.
func TestParse_EnumMemberNamedLikeAModifier(t *testing.T) {
	src := strings.Join([]string{
		"Public Enum ShutdownMethods As Integer", // 1
		"    WMI = 0",                            // 2
		"    Custom = 1",                         // 3
		"    [Default] = 2",                      // 4
		"    Shared = 3",                         // 5
		"End Enum",                               // 6
	}, "\n")
	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %v", res.Diagnostics)
	}
	e := findNode(res.File, "ShutdownMethods")
	if e == nil {
		t.Fatalf("no enum; tree:\n%s", res.File.Dump())
	}
	want := "enum_member:WMI,enum_member:Custom,enum_member:Default,enum_member:Shared"
	if got := strings.Join(kindsOf(e.Children), ","); got != want {
		t.Fatalf("members = %s, want %s", got, want)
	}
	// The declaration table must agree: a member classified as anything else
	// changes what ClassifyParen answers for a use site spelled the same way.
	if s := res.Table.Resolve("Custom", "ShutdownMethods"); s == nil || s.Kind != KindEnumMember {
		t.Errorf("table has Custom as %+v", s)
	}
}

// TestParse_InterpolatedStrings pins the shape S3 recorded as a known
// limitation and the corpus proved is not an edge case: 106 of the 302 real
// files use $"..." interpolation, and a hole may contain a nested literal.
//
// Scanning the hole as ordinary literal content mis-pairs every later quote on
// the line, which unbalances the bracket depth the continuation joiner runs
// on, which joins or splits the wrong lines — the container stack then
// desynchronises for the rest of the file.
func TestParse_InterpolatedStrings(t *testing.T) {
	t.Run("holes stay code, text is masked", func(t *testing.T) {
		src := `sb.Append($" {mit("Format").Replace("UTF-8", "SRT")} tail")`
		masked := MaskStringLiterals(src)
		if len(masked) != len(src) {
			t.Fatalf("masking changed length: %d vs %d", len(masked), len(src))
		}
		if !strings.Contains(masked, "mit(") || !strings.Contains(masked, ".Replace(") {
			t.Errorf("hole was masked away: %q", masked)
		}
		if strings.Contains(masked, "tail") || strings.Contains(masked, "UTF-8") {
			t.Errorf("literal text survived masking: %q", masked)
		}
		if d := bracketDelta(masked); d != 0 {
			t.Errorf("bracket delta = %d, want 0, on %q", d, masked)
		}
	})

	t.Run("a call inside a hole is a use site", func(t *testing.T) {
		src := "Public Class C\n    Public Function Name() As String\n        Return \"\"\n    End Function\n" +
			"    Public Sub F()\n        Log($\"x {Name()} y\")\n    End Sub\nEnd Class\n"
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Fatalf("diagnostics: %v", res.Diagnostics)
		}
		if _, ok := findRef(res.File, "Name"); !ok {
			t.Errorf("call inside an interpolation hole was not seen: %v", refStrings(res.File))
		}
	})

	t.Run("nested quotes do not desynchronise the joiner", func(t *testing.T) {
		src := strings.Join([]string{
			"Public Class C",     // 1
			"    Public Sub F()", // 2
			"        Run(Sub()",  // 3
			"                sb.Append($\" {mit(\"Format\").Replace(\"A\", \"B\")}\")", // 4
			"            End Sub",    // 5
			"        )",              // 6
			"    End Sub",            // 7
			"    Public Sub After()", // 8
			"    End Sub",            // 9
			"End Class",              // 10
		}, "\n")
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Fatalf("diagnostics: %v\ntree:\n%s", res.Diagnostics, res.File.Dump())
		}
		if n := findNode(res.File, "C.After"); n == nil || n.Scope != "C" {
			t.Fatalf("After was reparented; tree:\n%s", res.File.Dump())
		}
	})
}

// TestParse_Members covers the member shapes an entity pass needs, including
// the ones S3 flagged: an auto-property (no End Property), a MustOverride
// member (no End Sub) and a Declare (no body).
func TestParse_Members(t *testing.T) {
	src := strings.Join([]string{
		"Public MustInherit Class Base",                          // 1
		"    Private ReadOnly _log As String",                    // 2
		"    Public Const Limit As Integer = 10",                 // 3
		"    Public Property Title As String",                    // 4
		"    Public Property Item(ByVal i As Integer) As String", // 5
		"        Get",                                      // 6
		"            Return _log",                          // 7
		"        End Get",                                  // 8
		"        Set(ByVal value As String)",               // 9
		"        End Set",                                  // 10
		"    End Property",                                 // 11
		"    Public MustOverride Sub Render()",             // 12
		"    Public Event Changed(ByVal sender As Object)", // 13
		"    Declare Function GetTick Lib \"kernel32\" () As Integer", // 14
		"    Public Function Sum(ByVal a As Integer) As Integer",      // 15
		"        Return a", // 16
		"    End Function", // 17
		"End Class",        // 18
	}, "\n")

	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	base := findNode(res.File, "Base")
	if base == nil {
		t.Fatalf("no class; tree is\n%s", res.File.Dump())
	}
	want := []string{
		"field:_log", "const:Limit", "property:Title", "property:Item",
		"method:Render", "event:Changed", "method:GetTick", "method:Sum",
	}
	got := kindsOf(base.Children)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("members:\n got %v\nwant %v\ntree:\n%s", got, want, res.File.Dump())
	}

	// The auto-property closes on its own header; the full property closes on
	// End Property and owns its accessors.
	title := findNode(res.File, "Base.Title")
	if title.Span.EndLine != 4 {
		t.Errorf("auto-property span ends at %d, want 4", title.Span.EndLine)
	}
	item := findNode(res.File, "Base.Item")
	if item.Span.StartLine != 5 || item.Span.EndLine != 11 {
		t.Errorf("indexed property span = %d..%d, want 5..11", item.Span.StartLine, item.Span.EndLine)
	}
	if acc := kindsOf(item.Children); strings.Join(acc, ",") != "accessor:Get,accessor:Set" {
		t.Errorf("property accessors = %v", acc)
	}

	// A MustOverride Sub and a Declare open no block; the members after them
	// must still be siblings, not children.
	if r := findNode(res.File, "Base.Render"); r.Span.EndLine != 12 || len(r.Children) != 0 {
		t.Errorf("MustOverride Sub span = %d..%d, %d children",
			r.Span.StartLine, r.Span.EndLine, len(r.Children))
	}

	sum := findNode(res.File, "Base.Sum")
	if sum.TypeName != "Integer" {
		t.Errorf("Sum return type = %q, want Integer", sum.TypeName)
	}
	if len(sum.Params) != 1 || sum.Params[0].Name != "a" || sum.Params[0].TypeName != "Integer" {
		t.Errorf("Sum params = %+v", sum.Params)
	}
	if got := findNode(res.File, "Base._log"); got.TypeName != "String" ||
		strings.Join(got.Modifiers, " ") != "private readonly" {
		t.Errorf("field = %+v", got)
	}
}
